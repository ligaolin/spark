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

	"spark/app/service/fileutil"
	"spark/app/service/settings"
	"spark/app/service/sshlib"
	"spark/app/service/types"

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
	mu     sync.Mutex
	stopKA chan struct{}
	kaOnce sync.Once
	kaWg   sync.WaitGroup
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
		sess.kaWg.Add(1)
		go func() {
			defer sess.kaWg.Done()
			sshlib.KeepAliveLoop(sess.stopKA, func() error {
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
		}()
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
	sess.kaWg.Wait()
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
// (mode "name") or content matches (mode "content"). opts controls case
// sensitivity and regex (content only).
func (s *FTPFileService) Search(id, dir, pattern, mode string, opts types.SearchOptions) ([]types.SearchResult, error) {
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
	if contentMode {
		if err := fileutil.ValidateContentPattern(pattern, opts.CaseSensitive, opts.UseRegex); err != nil {
			return nil, err
		}
	}
	excludes := fileutil.CompileExcludes(opts.Exclude)
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
		if fileutil.MatchesExclude(excludes, p, name) {
			if entry.Type == ftp.EntryTypeFolder {
				w.SkipDir()
			}
			continue
		}
		if entry.Type == ftp.EntryTypeFolder {
			if !contentMode && fileutil.MatchNameOpt(name, pattern, opts.CaseSensitive) {
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
			hits, herr := fileutil.MatchLinesOpt(data, pattern, fileutil.MaxMatchesPerFile, opts.CaseSensitive, opts.UseRegex)
			if herr != nil {
				return nil, herr
			}
			for _, h := range hits {
				results = append(results, types.SearchResult{
					Path: p, Name: name, Size: int64(entry.Size), ModTime: entry.Time,
					LineNo: h.LineNo, Line: h.Line,
				})
				if len(results) >= fileutil.MaxSearchResults {
					break
				}
			}
		} else if fileutil.MatchNameOpt(name, pattern, opts.CaseSensitive) {
			results = append(results, types.SearchResult{
				Path: p, Name: name, Size: int64(entry.Size), ModTime: entry.Time,
			})
		}
	}
	return results, nil
}

// Replace replaces every content match of pattern in files under dir,
// returning how many files and occurrences were changed.
func (s *FTPFileService) Replace(id, dir, pattern, replacement, mode string, opts types.SearchOptions) (types.ReplaceResult, error) {
	sess, err := s.get(id)
	if err != nil {
		return types.ReplaceResult{}, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return types.ReplaceResult{}, errors.New("请输入搜索关键字")
	}
	if !strings.EqualFold(mode, "content") {
		return types.ReplaceResult{}, errors.New("仅内容搜索支持替换")
	}
	if err := fileutil.ValidateContentPattern(pattern, opts.CaseSensitive, opts.UseRegex); err != nil {
		return types.ReplaceResult{}, err
	}
	if dir == "" {
		if dir, err = sess.conn.CurrentDir(); err != nil {
			return types.ReplaceResult{}, err
		}
	}
	excludes := fileutil.CompileExcludes(opts.Exclude)
	res := types.ReplaceResult{}
	w := sess.conn.Walk(dir)
	for w.Next() {
		if w.Err() != nil {
			continue
		}
		p := w.Path()
		if p == dir {
			continue
		}
		entry := w.Stat()
		name := path.Base(p)
		if fileutil.MatchesExclude(excludes, p, name) {
			if entry.Type == ftp.EntryTypeFolder {
				w.SkipDir()
			}
			continue
		}
		if entry.Type != ftp.EntryTypeFile {
			continue
		}
		if entry.Size == 0 || entry.Size > fileutil.MaxContentSearchSize {
			continue
		}
		rc, rerr := sess.conn.Retr(p)
		if rerr != nil {
			continue
		}
		data, rerr := io.ReadAll(io.LimitReader(rc, fileutil.MaxContentSearchSize+1))
		_ = rc.Close()
		if rerr != nil || int64(len(data)) > fileutil.MaxContentSearchSize || fileutil.IsBinary(data) {
			continue
		}
		out, n, cerr := fileutil.ReplaceAllContent(data, pattern, replacement, opts.CaseSensitive, opts.UseRegex)
		if cerr != nil {
			return types.ReplaceResult{}, cerr
		}
		if n == 0 {
			continue
		}
		if werr := sess.conn.Stor(p, strings.NewReader(string(out))); werr != nil {
			return types.ReplaceResult{}, ftpErrHint(werr)
		}
		res.Files++
		res.Occurrences += n
	}
	return res, nil
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
		// 一次遍历同时得到总大小和各级目录列表，上传时不再重复读目录
		plan := scanLocalTree(localPath)
		counter := &transferCounter{
			id:    id,
			op:    "upload",
			name:  path.Base(remotePath),
			total: plan.total,
		}
		return ftpErrHint(s.uploadDir(sess.conn, id, localPath, remotePath, counter, plan))
	}
	return ftpErrHint(s.uploadFile(sess.conn, id, localPath, remotePath, nil))
}

// Copy copies a remote file or directory (recursively) to a new remote path.
func (s *FTPFileService) Copy(id, srcPath, dstPath string, isDir bool) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return ftpErrHint(copyFTP(sess.conn, srcPath, dstPath, isDir))
}

func copyFTP(conn *ftp.ServerConn, src, dst string, isDir bool) error {
	if !isDir {
		return copyFTPFile(conn, src, dst)
	}
	if err := ensureRemoteDir(conn, dst); err != nil {
		return err
	}
	entries, err := conn.List(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		sp := joinRemote(src, e.Name)
		dp := joinRemote(dst, e.Name)
		if e.Type == ftp.EntryTypeFolder {
			if err := copyFTP(conn, sp, dp, true); err != nil {
				return err
			}
			continue
		}
		if err := copyFTPFile(conn, sp, dp); err != nil {
			return err
		}
	}
	return nil
}

// copyFTPFile 复制单个文件。FTP 同一控制连接上不能同时挂着 RETR 的数据流又
// 发 STOR，否则 STOR 会读到 RETR 残留的 226 响应而报错；所以先完整取回到
// 本地临时文件并消费完 226，再发起上传。
func copyFTPFile(conn *ftp.ServerConn, src, dst string) error {
	tmp, err := os.CreateTemp("", "spark-ftp-copy-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(name)
	}()

	rc, err := conn.Retr(src)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(tmp, rc)
	closeErr := rc.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return conn.Stor(dst, tmp)
}

// uploadDir uploads a directory tree. All files in the tree share one counter
// and its grand total, so progress moves 0→100% across the whole directory
// instead of resetting for every file.
func (s *FTPFileService) uploadDir(conn *ftp.ServerConn, id, localDir, remoteDir string, counter *transferCounter, plan *localPlan) error {
	entries, ok := plan.dirs[localDir]
	if !ok {
		var err error
		entries, err = os.ReadDir(localDir)
		if err != nil {
			return err
		}
	}
	if err := ensureRemoteDir(conn, remoteDir); err != nil {
		return err
	}
	for _, e := range entries {
		lp := filepath.Join(localDir, e.Name())
		rp := joinRemote(remoteDir, e.Name())
		if e.IsDir() {
			if err := s.uploadDir(conn, id, lp, rp, counter, plan); err != nil {
				return err
			}
		} else {
			if err := s.uploadFile(conn, id, lp, rp, counter); err != nil {
				return err
			}
		}
	}
	return nil
}

// uploadFile uploads one file. A nil counter means a standalone transfer
// (progress is reported per file); directory transfers pass a shared counter.
func (s *FTPFileService) uploadFile(conn *ftp.ServerConn, id, localPath, remotePath string, counter *transferCounter) error {
	local, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer local.Close()

	// uploadDir 已确保目录存在，只有单文件上传才需要在这里补建父目录
	if counter == nil {
		if err := ensureRemoteDir(conn, path.Dir(remotePath)); err != nil {
			return err
		}
	}

	total := int64(0)
	if st, err := local.Stat(); err == nil {
		total = st.Size()
	}
	if counter == nil {
		counter = &transferCounter{id: id, op: "upload", name: path.Base(remotePath), total: total}
	}

	// 断点续传：远端已有部分内容且小于本地大小，则从该偏移继续
	offset := int64(0)
	if sz, err := conn.FileSize(remotePath); err == nil && sz > 0 && sz < total {
		offset = sz
	}

	pr := &progressReader{r: local, c: counter}

	if offset > 0 {
		// 已存在的远端内容也计入进度，续传时进度不会卡在 0
		counter.add(int(offset))
		// StorFrom 需要服务器支持 REST STREAM，失败则退回整文件上传
		if err := conn.StorFrom(remotePath, pr, uint64(offset)); err != nil {
			if _, serr := local.Seek(0, io.SeekStart); serr != nil {
				return err
			}
			counter.done -= offset
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
		// 一次遍历同时得到总大小和各级目录列表，下载时不再重复列目录
		plan := scanRemoteTree(sess.conn, remotePath)
		counter := &transferCounter{
			id:    id,
			op:    "download",
			name:  path.Base(remotePath),
			total: plan.total,
		}
		return ftpErrHint(s.downloadDir(sess.conn, id, remotePath, localPath, counter, plan))
	}
	return ftpErrHint(s.downloadFile(sess.conn, id, remotePath, localPath, nil))
}

func (s *FTPFileService) downloadDir(conn *ftp.ServerConn, id, remoteDir, localDir string, counter *transferCounter, plan *remotePlan) error {
	entries, ok := plan.dirs[remoteDir]
	if !ok {
		var err error
		entries, err = conn.List(remoteDir)
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		rp := joinRemote(remoteDir, e.Name)
		lp := filepath.Join(localDir, e.Name)
		if e.Type == ftp.EntryTypeFolder {
			if err := s.downloadDir(conn, id, rp, lp, counter, plan); err != nil {
				return err
			}
		} else if e.Type == ftp.EntryTypeFile {
			if err := s.downloadFile(conn, id, rp, lp, counter); err != nil {
				return err
			}
		}
	}
	return nil
}

// downloadFile downloads one file. A nil counter means a standalone transfer;
// directory transfers pass a shared counter with the tree's grand total.
func (s *FTPFileService) downloadFile(conn *ftp.ServerConn, id, remotePath, localPath string, counter *transferCounter) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	total := int64(0)
	if sz, err := conn.FileSize(remotePath); err == nil && sz >= 0 {
		total = sz
	}
	if counter == nil {
		counter = &transferCounter{id: id, op: "download", name: path.Base(remotePath), total: total}
	}

	// 断点续传：本地已有部分内容，则从该偏移继续
	offset := int64(0)
	if li, err := os.Stat(localPath); err == nil && li.Size() > 0 && li.Size() < total {
		offset = li.Size()
	}
	if offset >= total && total > 0 {
		counter.add(int(total)) // 已完成，计入总进度
		return nil
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
		counter.add(int(offset))
	}

	_, err = io.Copy(local, &progressReader{r: rc, c: counter})
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

// transferCounter accumulates bytes for one logical transfer (a single file or
// a whole directory tree) and throttles progress events.
type transferCounter struct {
	id    string
	op    string
	name  string
	total int64
	done  int64
	last  time.Time
}

func (c *transferCounter) add(n int) {
	if n <= 0 {
		return
	}
	c.done += int64(n)
	now := time.Now()
	if now.Sub(c.last) >= 150*time.Millisecond || c.done >= c.total {
		c.last = now
		application.Get().Event.Emit("transfer:progress", types.TransferProgress{
			SessionID: c.id,
			Op:        c.op,
			Name:      c.name,
			Done:      c.done,
			Total:     c.total,
		})
	}
}

// progressReader counts transferred bytes into a shared counter.
type progressReader struct {
	r io.Reader
	c *transferCounter
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	if n > 0 {
		p.c.add(n)
	}
	return n, err
}

// localPlan caches one traversal of a local directory tree: the grand total
// size plus per-directory entries, so the upload does not list dirs twice.
type localPlan struct {
	total int64
	dirs  map[string][]os.DirEntry
}

func scanLocalTree(root string) *localPlan {
	p := &localPlan{dirs: make(map[string][]os.DirEntry)}
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		p.dirs[dir] = entries
		for _, e := range entries {
			if e.IsDir() {
				walk(filepath.Join(dir, e.Name()))
			} else if info, ierr := e.Info(); ierr == nil {
				p.total += info.Size()
			}
		}
	}
	walk(root)
	return p
}

// remotePlan caches one traversal of a remote directory tree: the grand total
// size plus per-directory entries, so the download does not list dirs twice.
type remotePlan struct {
	total int64
	dirs  map[string][]*ftp.Entry
}

func scanRemoteTree(conn *ftp.ServerConn, root string) *remotePlan {
	p := &remotePlan{dirs: make(map[string][]*ftp.Entry)}
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := conn.List(dir)
		if err != nil {
			return
		}
		p.dirs[dir] = entries
		for _, e := range entries {
			switch e.Type {
			case ftp.EntryTypeFolder:
				walk(joinRemote(dir, e.Name))
			case ftp.EntryTypeFile:
				if e.Size > 0 {
					p.total += int64(e.Size)
				} else if sz, szErr := conn.FileSize(joinRemote(dir, e.Name)); szErr == nil && sz > 0 {
					p.total += sz
				}
			}
		}
	}
	walk(root)
	return p
}
