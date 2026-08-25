// Package sftp implements SFTP file operations exposed to the frontend
// as a Wails service. Upload/Download support directories (recursive)
// and resuming interrupted transfers.
package sftp

import (
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
	"changeme/app/service/settings"
	"changeme/app/service/sshlib"
	"changeme/app/service/types"

	sftplib "github.com/pkg/sftp"
	"github.com/wailsapp/wails/v3/pkg/application"
	xssh "golang.org/x/crypto/ssh"
)

// SFTPFileService manages SFTP sessions for remote file operations.
type SFTPFileService struct {
	mu       sync.Mutex
	sessions map[string]*sftpSession
}

type sftpSession struct {
	id     string
	client *xssh.Client
	sftp   *sftplib.Client

	stopKA chan struct{}
	kaOnce sync.Once
	kaWg   sync.WaitGroup
}

// ServiceName implements application.ServiceName.
func (s *SFTPFileService) ServiceName() string { return "SFTPFileService" }

// Connect dials a new SSH connection and opens the SFTP subsystem.
// It returns the new session id.
func (s *SFTPFileService) Connect(opts types.ConnectOptions) (string, error) {
	if strings.TrimSpace(opts.Host) == "" {
		return "", errors.New("主机地址不能为空")
	}
	if opts.Port <= 0 || opts.Port > 65535 {
		opts.Port = 22
	}

	config, err := sshlib.BuildClientConfig(opts)
	if err != nil {
		return "", err
	}

	client, err := xssh.Dial("tcp", net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port)), config)
	if err != nil {
		if werr := sshlib.AsHostKeyError(err); werr != nil {
			return "", werr
		}
		return "", fmt.Errorf("SSH 连接失败: %w", err)
	}

	sf, err := sftplib.NewClient(client)
	if err != nil {
		client.Close()
		return "", fmt.Errorf("初始化 SFTP 失败: %w", err)
	}

	id := opts.SessionID
	if id == "" {
		id = types.NewID()
	}

	sess := &sftpSession{id: id, client: client, sftp: sf, stopKA: make(chan struct{})}

	s.ensure()
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()

	// 保活：定期发送 SSH keepalive；连接死亡时通知前端并清理会话（间隔可在设置中调整）
	if ka := settings.GetInt("keepalive.interval", 20); ka > 0 {
		sess.kaWg.Add(1)
		go func() {
			defer sess.kaWg.Done()
			sshlib.KeepAliveLoop(sess.stopKA, func() error {
				_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
				return err
			}, time.Duration(ka)*time.Second, 10*time.Second, 3, func() {
				application.Get().Event.Emit("session:closed", types.SessionClosed{
					SessionID: id,
					Type:      "sftp",
					Reason:    "连接已断开",
				})
				_ = s.Disconnect(id)
			})
		}()
	}

	// Note: SFTP has no server-side "current directory"; the frontend
	// navigates to opts.DefaultDir itself after connecting.
	return id, nil
}

// Disconnect closes an SFTP session.
func (s *SFTPFileService) Disconnect(id string) error {
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
	_ = sess.sftp.Close()
	return sess.client.Close()
}

// Home returns the remote current directory.
func (s *SFTPFileService) Home(id string) (string, error) {
	sess, err := s.get(id)
	if err != nil {
		return "", err
	}
	return sess.sftp.Getwd()
}

// List returns the entries of a remote directory.
func (s *SFTPFileService) List(id, remotePath string) ([]types.FileEntry, error) {
	sess, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if remotePath == "" {
		remotePath, err = sess.sftp.Getwd()
		if err != nil {
			return nil, err
		}
	}
	infos, err := sess.sftp.ReadDir(remotePath)
	if err != nil {
		return nil, err
	}
	entries := make([]types.FileEntry, 0, len(infos))
	for _, fi := range infos {
		entries = append(entries, s.toFileEntry(sess.sftp, remotePath, fi))
	}
	return entries, nil
}

// Stat returns information about a remote path.
func (s *SFTPFileService) Stat(id, remotePath string) (*types.FileEntry, error) {
	sess, err := s.get(id)
	if err != nil {
		return nil, err
	}
	fi, err := sess.sftp.Stat(remotePath)
	if err != nil {
		return nil, err
	}
	e := s.toFileEntry(sess.sftp, path.Dir(remotePath), fi)
	e.Path = remotePath
	return &e, nil
}

// Mkdir creates a remote directory (including missing parents).
func (s *SFTPFileService) Mkdir(id, remotePath string) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	return ensureRemoteDir(sess.sftp, remotePath)
}

// Rename renames or moves a remote path.
func (s *SFTPFileService) Rename(id, oldPath, newPath string) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	return sess.sftp.Rename(oldPath, newPath)
}

// Remove deletes a remote file or directory (recursively).
func (s *SFTPFileService) Remove(id, remotePath string) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	return removeRemote(sess.sftp, remotePath)
}

// Chmod changes the permission mode of a remote path.
func (s *SFTPFileService) Chmod(id, remotePath string, mode uint32) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	return sess.sftp.Chmod(remotePath, os.FileMode(mode))
}

// Search walks a remote directory recursively and returns filename matches
// (mode "name") or content matches (mode "content"). opts controls case
// sensitivity and regex (content only).
func (s *SFTPFileService) Search(id, dir, pattern, mode string, opts types.SearchOptions) ([]types.SearchResult, error) {
	sess, err := s.get(id)
	if err != nil {
		return nil, err
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, errors.New("请输入搜索关键字")
	}
	if dir == "" {
		if dir, err = sess.sftp.Getwd(); err != nil {
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
	w := sess.sftp.Walk(dir)
	for w.Step() {
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
		info := w.Stat()
		name := path.Base(p)
		if fileutil.MatchesExclude(excludes, p, name) {
			if info.IsDir() {
				w.SkipDir()
			}
			continue
		}
		if info.IsDir() {
			if !contentMode && fileutil.MatchNameOpt(name, pattern, opts.CaseSensitive) {
				results = append(results, types.SearchResult{
					Path: p, Name: name, Size: info.Size(), ModTime: info.ModTime(), IsDir: true,
				})
			}
			continue
		}
		if contentMode {
			if info.Size() <= 0 || info.Size() > fileutil.MaxContentSearchSize {
				continue
			}
			f, oerr := sess.sftp.Open(p)
			if oerr != nil {
				continue
			}
			data, rerr := io.ReadAll(io.LimitReader(f, fileutil.MaxContentSearchSize+1))
			_ = f.Close()
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
					Path: p, Name: name, Size: info.Size(), ModTime: info.ModTime(),
					LineNo: h.LineNo, Line: h.Line,
				})
				if len(results) >= fileutil.MaxSearchResults {
					break
				}
			}
		} else if fileutil.MatchNameOpt(name, pattern, opts.CaseSensitive) {
			results = append(results, types.SearchResult{
				Path: p, Name: name, Size: info.Size(), ModTime: info.ModTime(),
			})
		}
	}
	return results, nil
}

// Replace replaces every content match of pattern in files under dir,
// returning how many files and occurrences were changed.
func (s *SFTPFileService) Replace(id, dir, pattern, replacement, mode string, opts types.SearchOptions) (types.ReplaceResult, error) {
	sess, err := s.get(id)
	if err != nil {
		return types.ReplaceResult{}, err
	}
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
		if dir, err = sess.sftp.Getwd(); err != nil {
			return types.ReplaceResult{}, err
		}
	}
	excludes := fileutil.CompileExcludes(opts.Exclude)
	res := types.ReplaceResult{}
	w := sess.sftp.Walk(dir)
	for w.Step() {
		if w.Err() != nil {
			continue
		}
		p := w.Path()
		if p == dir {
			continue
		}
		info := w.Stat()
		name := path.Base(p)
		if fileutil.MatchesExclude(excludes, p, name) {
			if info.IsDir() {
				w.SkipDir()
			}
			continue
		}
		if info.IsDir() || info.Size() <= 0 || info.Size() > fileutil.MaxContentSearchSize {
			continue
		}
		f, oerr := sess.sftp.Open(p)
		if oerr != nil {
			continue
		}
		data, rerr := io.ReadAll(io.LimitReader(f, fileutil.MaxContentSearchSize+1))
		_ = f.Close()
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
		if werr := s.WriteFile(id, p, string(out)); werr != nil {
			return types.ReplaceResult{}, werr
		}
		res.Files++
		res.Occurrences += n
	}
	return res, nil
}

// ReadFile reads a remote text file for the built-in editor, enforcing a size
// limit and rejecting binary content.
func (s *SFTPFileService) ReadFile(id, remotePath string) (string, error) {
	sess, err := s.get(id)
	if err != nil {
		return "", err
	}
	f, err := sess.sftp.Open(remotePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, fileutil.MaxEditSize+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > fileutil.MaxEditSize {
		return "", fmt.Errorf("文件过大（超过 %d MB），无法在编辑器中打开", fileutil.MaxEditSize>>20)
	}
	if fileutil.IsBinary(data) {
		return "", errors.New("该文件为二进制文件，无法用文本编辑器打开")
	}
	return string(data), nil
}

// WriteFile overwrites a remote text file with the given content.
func (s *SFTPFileService) WriteFile(id, remotePath, content string) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	f, err := sess.sftp.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write([]byte(content))
	return err
}

// Upload copies a local file or directory (recursively) to the remote path.
// Interrupted file transfers are resumed from the remote file size.
func (s *SFTPFileService) Upload(id, localPath, remotePath string) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	fi, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return s.uploadDir(sess.sftp, id, localPath, remotePath)
	}
	return s.uploadFile(sess.sftp, id, localPath, remotePath)
}

func (s *SFTPFileService) uploadDir(cl *sftplib.Client, id, localDir, remoteDir string) error {
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return err
	}
	if err := ensureRemoteDir(cl, remoteDir); err != nil {
		return err
	}
	for _, e := range entries {
		lp := filepath.Join(localDir, e.Name())
		rp := path.Join(remoteDir, e.Name())
		if e.IsDir() {
			if err := s.uploadDir(cl, id, lp, rp); err != nil {
				return err
			}
		} else {
			if err := s.uploadFile(cl, id, lp, rp); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SFTPFileService) uploadFile(cl *sftplib.Client, id, localPath, remotePath string) error {
	local, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer local.Close()

	if err := ensureRemoteDir(cl, path.Dir(remotePath)); err != nil {
		return err
	}

	total := int64(0)
	if st, err := local.Stat(); err == nil {
		total = st.Size()
	}

	// 断点续传：远端已有部分内容且小于本地大小，则从该偏移继续
	offset := int64(0)
	if ri, err := cl.Stat(remotePath); err == nil && !ri.IsDir() && ri.Size() > 0 && ri.Size() < total {
		offset = ri.Size()
	}

	var remote *sftplib.File
	if offset > 0 {
		remote, err = cl.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE)
		if err != nil {
			return err
		}
		if _, err := remote.Seek(offset, io.SeekStart); err != nil {
			remote.Close()
			return err
		}
		if _, err := local.Seek(offset, io.SeekStart); err != nil {
			remote.Close()
			return err
		}
	} else {
		remote, err = cl.Create(remotePath)
		if err != nil {
			return err
		}
	}
	defer remote.Close()

	_, err = io.Copy(remote, &progressReader{
		r:     local,
		svc:   s,
		id:    id,
		op:    "upload",
		name:  path.Base(remotePath),
		total: total,
		done:  offset,
	})
	return err
}

// Download copies a remote file or directory (recursively) to the local path.
// Interrupted file transfers are resumed from the local file size.
func (s *SFTPFileService) Download(id, remotePath, localPath string) error {
	sess, err := s.get(id)
	if err != nil {
		return err
	}
	fi, err := sess.sftp.Stat(remotePath)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return s.downloadDir(sess.sftp, id, remotePath, localPath)
	}
	return s.downloadFile(sess.sftp, id, remotePath, localPath)
}

func (s *SFTPFileService) downloadDir(cl *sftplib.Client, id, remoteDir, localDir string) error {
	entries, err := cl.ReadDir(remoteDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		rp := path.Join(remoteDir, e.Name())
		lp := filepath.Join(localDir, e.Name())
		if e.IsDir() {
			if err := s.downloadDir(cl, id, rp, lp); err != nil {
				return err
			}
		} else {
			if err := s.downloadFile(cl, id, rp, lp); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SFTPFileService) downloadFile(cl *sftplib.Client, id, remotePath, localPath string) error {
	remote, err := cl.Open(remotePath)
	if err != nil {
		return err
	}
	defer remote.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	total := int64(0)
	if st, err := remote.Stat(); err == nil {
		total = st.Size()
	}

	// 断点续传：本地已有部分内容，则从该偏移继续
	offset := int64(0)
	if li, err := os.Stat(localPath); err == nil && li.Size() > 0 && li.Size() < total {
		offset = li.Size()
	}
	if offset >= total && total > 0 {
		return nil // 已完成
	}

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
		if _, err := remote.Seek(offset, io.SeekStart); err != nil {
			return err
		}
		if _, err := local.Seek(offset, io.SeekStart); err != nil {
			return err
		}
	}

	_, err = io.Copy(local, &progressReader{
		r:     remote,
		svc:   s,
		id:    id,
		op:    "download",
		name:  path.Base(remotePath),
		total: total,
		done:  offset,
	})
	return err
}

func (s *SFTPFileService) toFileEntry(cl *sftplib.Client, parent string, fi os.FileInfo) types.FileEntry {
	e := types.FileEntry{
		Name:    fi.Name(),
		Path:    path.Join(parent, fi.Name()),
		Size:    fi.Size(),
		Mode:    fi.Mode().String(),
		ModTime: fi.ModTime(),
		IsDir:   fi.IsDir(),
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		e.Symlink = true
		if target, err := cl.ReadLink(e.Path); err == nil {
			e.LinkTarget = target
		}
		if st, err := cl.Stat(e.Path); err == nil {
			e.IsDir = st.IsDir()
		}
	}
	return e
}

func (s *SFTPFileService) get(id string) (*sftpSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		return sess, nil
	}
	return nil, fmt.Errorf("SFTP 会话 %q 不存在", id)
}

func (s *SFTPFileService) ensure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[string]*sftpSession)
	}
}

func ensureRemoteDir(cl *sftplib.Client, dir string) error {
	if dir == "" || dir == "." || dir == "/" {
		return nil
	}
	if _, err := cl.Stat(dir); err == nil {
		return nil
	}
	if err := ensureRemoteDir(cl, path.Dir(dir)); err != nil {
		return err
	}
	return cl.Mkdir(dir)
}

func removeRemote(cl *sftplib.Client, p string) error {
	fi, err := cl.Stat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return cl.Remove(p)
	}
	entries, err := cl.ReadDir(p)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := removeRemote(cl, path.Join(p, e.Name())); err != nil {
			return err
		}
	}
	return cl.RemoveDirectory(p)
}

// progressReader counts transferred bytes and emits progress events.
type progressReader struct {
	r     io.Reader
	svc   *SFTPFileService
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
