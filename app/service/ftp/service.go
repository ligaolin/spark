// Package ftp implements FTP(S) file operations exposed to the frontend
// as a Wails service. Upload/Download support directories (recursive) and
// best-effort resume via the REST command (falls back to full transfer when
// the server does not support it).
package ftp

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"changeme/app/service/fileutil"
	"changeme/app/service/sshlib"
	"changeme/app/service/settings"
	"changeme/app/service/types"

	"github.com/jlaffaye/ftp"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// FTPFileService manages FTP sessions for remote file operations.
type FTPFileService struct {
	mu       sync.Mutex
	sessions map[string]*ftpSession
}

type ftpSession struct {
	id   string
	conn *ftp.ServerConn

	// mu 串行化对 FTP 控制连接的所有操作：jlaffaye/ftp 的 ServerConn 不是并发
	// 安全的，保活 NOOP 与列表/传输并发读写同一个 bufio 会崩溃（slice bounds
	// out of range panic）。
	mu      sync.Mutex
	stopKA  chan struct{}
	kaOnce  sync.Once
}

// ServiceName implements application.ServiceName.
func (s *FTPFileService) ServiceName() string { return "FTPFileService" }

// Connect establishes an FTP (or explicit FTPS) session and logs in.
// It returns the new session id.
func (s *FTPFileService) Connect(opts types.ConnectOptions) (string, error) {
	if strings.TrimSpace(opts.Host) == "" {
		return "", errors.New("主机地址不能为空")
	}
	if opts.Port <= 0 || opts.Port > 65535 {
		opts.Port = 21
	}
	addr := net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port))

	var conn *ftp.ServerConn
	var err error
	if opts.TLS {
		tlsConf := &tls.Config{
			InsecureSkipVerify: opts.Insecure,
			ServerName:         opts.Host,
		}
		conn, err = ftp.Dial(addr,
			ftp.DialWithTimeout(15*time.Second),
			ftp.DialWithExplicitTLS(tlsConf),
		)
	} else {
		conn, err = ftp.Dial(addr, ftp.DialWithTimeout(15*time.Second))
	}
	if err != nil {
		return "", fmt.Errorf("FTP 连接失败: %w", err)
	}

	if err := conn.Login(opts.Username, opts.Password); err != nil {
		_ = conn.Quit()
		return "", fmt.Errorf("FTP 登录失败: %w", err)
	}

	id := opts.SessionID
	if id == "" {
		id = types.NewID()
	}

	sess := &ftpSession{id: id, conn: conn, stopKA: make(chan struct{})}

	s.ensure()
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()

	// 保活：定期发送 NOOP，防止服务器空闲超时断开；连接死亡时通知前端并清理（间隔可在设置中调整）
	// 注意：控制连接忙（列表/传输进行中）时跳过本轮 NOOP，避免与传输并发读写同一连接
	if ka := settings.GetInt("keepalive.interval", 20); ka > 0 {
		go sshlib.KeepAliveLoop(sess.stopKA, func() error {
			if !sess.mu.TryLock() {
				return nil // 连接正忙，本轮跳过；操作本身也在保活
			}
			defer sess.mu.Unlock()
			return conn.NoOp()
		}, time.Duration(ka)*time.Second, 10*time.Second, 3, func() {
			application.Get().Event.Emit("session:closed", types.SessionClosed{
				SessionID: id,
				Type:      "ftp",
				Reason:    "连接已断开",
			})
			_ = s.Disconnect(id)
		})
	}

	if opts.DefaultDir != "" {
		_ = conn.ChangeDir(opts.DefaultDir) // best effort
	}
	return id, nil
}

// Disconnect closes an FTP session.
func (s *FTPFileService) Disconnect(id string) error {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	if !ok {
		return nil
	}
	sess.kaOnce.Do(func() { close(sess.stopKA) })
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.conn.Quit()
}

// Home returns the remote current directory.
func (s *FTPFileService) Home(id string) (string, error) {
	sess, err := s.get(id)
	if err != nil {
		return "", err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.conn.CurrentDir()
}

// List returns the entries of a remote directory.
func (s *FTPFileService) List(id, remotePath string) ([]types.FileEntry, error) {
	sess, err := s.get(id)
	if err != nil {
		return nil, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if remotePath == "" {
		remotePath, err = sess.conn.CurrentDir()
		if err != nil {
			return nil, err
		}
	}
	entries, err := sess.conn.List(remotePath)
	if err != nil {
		return nil, err
	}
	out := make([]types.FileEntry, 0, len(entries))
	for _, e := range entries {
		// 部分 FTP 服务器的 LIST 会返回 . 和 ..（当前目录/上级目录），
		// 展示层过滤掉，避免出现在文件列表中。
		if e.Name == "." || e.Name == ".." {
			continue
		}
		out = append(out, types.FileEntry{
			Name:       e.Name,
			Path:       joinRemote(remotePath, e.Name),
			Size:       int64(e.Size),
			Mode:       entryTypeName(e.Type),
			ModTime:    e.Time,
			IsDir:      e.Type == ftp.EntryTypeFolder,
			Symlink:    e.Type == ftp.EntryTypeLink,
			LinkTarget: e.Target,
		})
	}
	return out, nil
}

// Mkdir creates a remote directory (including missing parents).
func (s *FTPFileService) Mkdir(id, remotePath string) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return ensureRemoteDir(sess.conn, remotePath)
}

// Rename renames or moves a remote path.
func (s *FTPFileService) Rename(id, oldPath, newPath string) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.conn.Rename(oldPath, newPath)
}

// Remove deletes a remote file or directory (recursively for directories).
func (s *FTPFileService) Remove(id, remotePath string, isDir bool) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if isDir {
		return sess.conn.RemoveDirRecur(remotePath)
	}
	return sess.conn.Delete(remotePath)
}

// Search walks a remote directory recursively and returns filename matches
// (mode "name") or content matches (mode "content").
func (s *FTPFileService) Search(id, dir, pattern, mode string) ([]types.SearchResult, error) {
	sess, err := s.get(id)
	if err != nil {
		return nil, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, errors.New("请输入搜索关键字")
	}
	if dir == "" {
		if dir, err = sess.conn.CurrentDir(); err != nil {
			return nil, err
		}
	}
	contentMode := strings.EqualFold(mode, "content")
	results := make([]types.SearchResult, 0, 64)
	w := sess.conn.Walk(dir)
	for w.Next() {
		if w.Err() != nil {
			continue
		}
		p := w.Path()
		if p == dir {
			continue // 跳过根目录本身
		}
		if len(results) >= fileutil.MaxSearchResults {
			break
		}
		entry := w.Stat()
		name := path.Base(p)
		if entry.Type == ftp.EntryTypeFolder {
			if !contentMode && fileutil.MatchName(name, pattern) {
				results = append(results, types.SearchResult{
					Path: p, Name: name, Size: int64(entry.Size), ModTime: entry.Time, IsDir: true,
				})
			}
			continue
		}
		if entry.Type != ftp.EntryTypeFile {
			continue // 跳过链接等非常规条目
		}
		if contentMode {
			if entry.Size == 0 || entry.Size > fileutil.MaxContentSearchSize {
				continue
			}
			rc, rerr := sess.conn.Retr(p)
			if rerr != nil {
				continue
			}
			data, rerr := io.ReadAll(io.LimitReader(rc, fileutil.MaxContentSearchSize+1))
			_ = rc.Close()
			if rerr != nil || int64(len(data)) > fileutil.MaxContentSearchSize {
				continue
			}
			if fileutil.IsBinary(data) {
				continue
			}
			for _, h := range fileutil.MatchLines(data, pattern, fileutil.MaxMatchesPerFile) {
				results = append(results, types.SearchResult{
					Path: p, Name: name, Size: int64(entry.Size), ModTime: entry.Time,
					LineNo: h.LineNo, Line: h.Line,
				})
				if len(results) >= fileutil.MaxSearchResults {
					break
				}
			}
		} else if fileutil.MatchName(name, pattern) {
			results = append(results, types.SearchResult{
				Path: p, Name: name, Size: int64(entry.Size), ModTime: entry.Time,
			})
		}
	}
	return results, nil
}

// ReadFile reads a remote text file for the built-in editor, enforcing a size
// limit and rejecting binary content.
func (s *FTPFileService) ReadFile(id, remotePath string) (string, error) {
	sess, err := s.get(id)
	if err != nil {
		return "", err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	rc, err := sess.conn.Retr(remotePath)
	if err != nil {
		return "", ftpErrHint(err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, fileutil.MaxEditSize+1))
	if err != nil {
		return "", ftpErrHint(err)
	}
	if int64(len(data)) > fileutil.MaxEditSize {
		return "", fmt.Errorf("文件过大（超过 %d MB），无法在编辑器中打开", fileutil.MaxEditSize>>20)
	}
	if fileutil.IsBinary(data) {
		return "", errors.New("该文件为二进制文件，无法用文本编辑器打开")
	}
	return string(data), nil
}

// WriteFile overwrites a remote text file with the given content (FTP STOR).
func (s *FTPFileService) WriteFile(id, remotePath, content string) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return ftpErrHint(sess.conn.Stor(remotePath, strings.NewReader(content)))
}

// Upload copies a local file or directory (recursively) to the remote path.
// Interrupted file transfers resume from the remote file size (REST).
func (s *FTPFileService) Upload(id, localPath, remotePath string) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	fi, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return ftpErrHint(s.uploadDir(sess.conn, id, localPath, remotePath))
	}
	return ftpErrHint(s.uploadFile(sess.conn, id, localPath, remotePath))
}

func (s *FTPFileService) uploadDir(conn *ftp.ServerConn, id, localDir, remoteDir string) error {
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return err
	}
	if err := ensureRemoteDir(conn, remoteDir); err != nil {
		return err
	}
	for _, e := range entries {
		lp := filepath.Join(localDir, e.Name())
		rp := joinRemote(remoteDir, e.Name())
		if e.IsDir() {
			if err := s.uploadDir(conn, id, lp, rp); err != nil {
				return err
			}
		} else {
			if err := s.uploadFile(conn, id, lp, rp); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *FTPFileService) uploadFile(conn *ftp.ServerConn, id, localPath, remotePath string) error {
	local, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer local.Close()

	if err := ensureRemoteDir(conn, path.Dir(remotePath)); err != nil {
		return err
	}

	total := int64(0)
	if st, err := local.Stat(); err == nil {
		total = st.Size()
	}

	// 断点续传：远端已有部分内容且小于本地大小，则从该偏移继续
	offset := int64(0)
	if sz, err := conn.FileSize(remotePath); err == nil && sz > 0 && sz < total {
		offset = sz
	}

	pr := &progressReader{
		r:     local,
		svc:   s,
		id:    id,
		op:    "upload",
		name:  path.Base(remotePath),
		total: total,
		done:  offset,
	}

	if offset > 0 {
		// StorFrom 需要服务器支持 REST STREAM，失败则退回整文件上传
		if err := conn.StorFrom(remotePath, pr, uint64(offset)); err != nil {
			if _, serr := local.Seek(0, io.SeekStart); serr != nil {
				return err
			}
			pr.done = 0
			return conn.Stor(remotePath, pr)
		}
		return nil
	}
	return conn.Stor(remotePath, pr)
}

// Download copies a remote file or directory (recursively) to the local path.
// isDir tells the service the remote path is a directory (FTP LIST cannot be
// reliably used to stat a single path). Interrupted file transfers resume
// from the local file size (REST).
func (s *FTPFileService) Download(id, remotePath, localPath string, isDir bool) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if isDir {
		return ftpErrHint(s.downloadDir(sess.conn, id, remotePath, localPath))
	}
	return ftpErrHint(s.downloadFile(sess.conn, id, remotePath, localPath))
}

func (s *FTPFileService) downloadDir(conn *ftp.ServerConn, id, remoteDir, localDir string) error {
	entries, err := conn.List(remoteDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		rp := joinRemote(remoteDir, e.Name)
		lp := filepath.Join(localDir, e.Name)
		if e.Type == ftp.EntryTypeFolder {
			if err := s.downloadDir(conn, id, rp, lp); err != nil {
				return err
			}
		} else if e.Type == ftp.EntryTypeFile {
			if err := s.downloadFile(conn, id, rp, lp); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *FTPFileService) downloadFile(conn *ftp.ServerConn, id, remotePath, localPath string) error {
	if err := os.MkdirAll(path.Dir(localPath), 0o755); err != nil {
		return err
	}

	total := int64(0)
	if sz, err := conn.FileSize(remotePath); err == nil && sz >= 0 {
		total = sz
	}

	// 断点续传：本地已有部分内容，则从该偏移继续
	offset := int64(0)
	if li, err := os.Stat(localPath); err == nil && li.Size() > 0 && li.Size() < total {
		offset = li.Size()
	}
	if offset >= total && total > 0 {
		return nil // 已完成
	}

	var rc io.ReadCloser
	var err error
	if offset > 0 {
		rc, err = conn.RetrFrom(remotePath, uint64(offset))
		if err != nil {
			// 服务器不支持 REST：退回整文件下载
			offset = 0
			rc, err = conn.Retr(remotePath)
		}
	} else {
		rc, err = conn.Retr(remotePath)
	}
	if err != nil {
		return err
	}
	defer rc.Close()

	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	local, err := os.OpenFile(localPath, flags, 0o644)
	if err != nil {
		return err
	}
	defer local.Close()
	if offset > 0 {
		if _, err := local.Seek(offset, io.SeekStart); err != nil {
			return err
		}
	}

	_, err = io.Copy(local, &progressReader{
		r:     rc,
		svc:   s,
		id:    id,
		op:    "download",
		name:  path.Base(remotePath),
		total: total,
		done:  offset,
	})
	return err
}

func joinRemote(dir, name string) string {
	if dir == "" || dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

// ftpErrHint 在数据连接相关错误上补充被动端口提示（常见原因：服务器防火墙/
// 安全组未放行 FTP 被动端口范围，或服务器被动端口配置错误）。
func ftpErrHint(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "dial tcp") || strings.Contains(msg, "refused") ||
		strings.Contains(msg, "no route") || strings.Contains(msg, "timed out") {
		return fmt.Errorf("%w（无法连接 FTP 数据端口——通常是服务器防火墙/安全组未放行被动端口范围，或服务器被动模式配置错误，请在服务器 FTP 设置中检查被动端口并放行）", err)
	}
	return err
}

func entryTypeName(t ftp.EntryType) string {
	switch t {
	case ftp.EntryTypeFolder:
		return "dir"
	case ftp.EntryTypeLink:
		return "link"
	default:
		return "file"
	}
}

func (s *FTPFileService) get(id string) (*ftpSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		return sess, nil
	}
	return nil, fmt.Errorf("FTP 会话 %q 不存在", id)
}

func (s *FTPFileService) ensure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[string]*ftpSession)
	}
}

func ensureRemoteDir(conn *ftp.ServerConn, dir string) error {
	if dir == "" || dir == "." || dir == "/" {
		return nil
	}
	if _, err := conn.List(dir); err == nil {
		return nil
	}
	if err := ensureRemoteDir(conn, path.Dir(dir)); err != nil {
		return err
	}
	return conn.MakeDir(dir)
}

// progressReader counts transferred bytes and emits progress events.
type progressReader struct {
	r     io.Reader
	svc   *FTPFileService
	id    string
	op    string
	name  string
	total int64
	done  int64
	last  time.Time
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	if n > 0 {
		p.done += int64(n)
		now := time.Now()
		if now.Sub(p.last) >= 150*time.Millisecond || p.done >= p.total {
			p.last = now
			application.Get().Event.Emit("transfer:progress", types.TransferProgress{
				SessionID: p.id,
				Op:        p.op,
				Name:      p.name,
				Done:      p.done,
				Total:     p.total,
			})
		}
	}
	return n, err
}
