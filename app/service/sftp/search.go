package sftp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"spark/app/service/fileutil"
	"spark/app/service/types"

	"github.com/wailsapp/wails/v3/pkg/application"
	xssh "golang.org/x/crypto/ssh"
)

const (
	searchEventName  = "sftp:search"
	searchWorkers    = 8
	searchBatchSize  = 40
	searchFlushEvery = 120 * time.Millisecond
	searchLineMax    = 500
)

// searchState 是一次搜索的取消标记，由前端 searchId 关联。
type searchState struct {
	cancel atomic.Bool
}

// searchProgress 汇总扫描进度并节流推送事件；Results 只会包含新增的命中。
type searchProgress struct {
	id        string
	sessionID string
	enabled   bool

	mu      sync.Mutex
	dirs    int
	files   int
	matches int
	batch   []types.SearchResult
	last    time.Time
}

func newSearchProgress(id, sessionID string) *searchProgress {
	return &searchProgress{id: id, sessionID: sessionID, enabled: id != "", last: time.Now()}
}

func (p *searchProgress) addDirs(n int) {
	if n <= 0 {
		return
	}
	p.mu.Lock()
	p.dirs += n
	p.maybeFlushLocked()
	p.mu.Unlock()
}

func (p *searchProgress) addFiles(n int) {
	if n <= 0 {
		return
	}
	p.mu.Lock()
	p.files += n
	p.maybeFlushLocked()
	p.mu.Unlock()
}

func (p *searchProgress) addResults(rs []types.SearchResult) {
	if len(rs) == 0 {
		return
	}
	p.mu.Lock()
	p.batch = append(p.batch, rs...)
	p.matches += len(rs)
	if len(p.batch) >= searchBatchSize {
		p.emitLocked(false, false)
	} else {
		p.maybeFlushLocked()
	}
	p.mu.Unlock()
}

func (p *searchProgress) maybeFlushLocked() {
	if !p.enabled || time.Since(p.last) < searchFlushEvery {
		return
	}
	p.emitLocked(false, false)
}

func (p *searchProgress) finish(cancelled bool) {
	p.mu.Lock()
	p.emitLocked(true, cancelled)
	p.mu.Unlock()
}

func (p *searchProgress) emitLocked(done, cancelled bool) {
	if !p.enabled {
		return
	}
	batch := p.batch
	p.batch = nil
	application.Get().Event.Emit(searchEventName, types.SearchProgress{
		SearchID:  p.id,
		SessionID: p.sessionID,
		Dirs:      p.dirs,
		Files:     p.files,
		Matches:   p.matches,
		Results:   batch,
		Done:      done,
		Cancelled: cancelled,
	})
	p.last = time.Now()
}

// Search walks a remote directory recursively and returns filename matches
// (mode "name") or content matches (mode "content"). opts controls case
// sensitivity and regex (content only).
func (s *SFTPFileService) Search(id, dir, pattern, mode string, opts types.SearchOptions) ([]types.SearchResult, error) {
	return s.search(id, dir, pattern, mode, "", opts)
}

// SearchStream 与 Search 相同，但会通过 sftp:search 事件推送进度与增量结果，
// 并可被 CancelSearch(searchID) 取消。
func (s *SFTPFileService) SearchStream(id, dir, pattern, mode, searchID string, opts types.SearchOptions) ([]types.SearchResult, error) {
	return s.search(id, dir, pattern, mode, searchID, opts)
}

// CancelSearch 请求取消指定 searchId 的搜索（尽力而为，已返回的结果仍有效）。
func (s *SFTPFileService) CancelSearch(searchID string) {
	if searchID == "" {
		return
	}
	if v, ok := s.searches.Load(searchID); ok {
		if st, ok := v.(*searchState); ok {
			st.cancel.Store(true)
		}
	}
}

func (s *SFTPFileService) search(id, dir, pattern, mode, searchID string, opts types.SearchOptions) ([]types.SearchResult, error) {
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

	st := &searchState{}
	if searchID != "" {
		s.searches.Store(searchID, st)
		defer s.searches.Delete(searchID)
	}
	rep := newSearchProgress(searchID, id)

	var results []types.SearchResult
	if contentMode {
		results, err = s.searchContent(sess, dir, pattern, opts, excludes, st, rep)
	} else {
		results = s.searchNames(sess, dir, pattern, opts, excludes, st, rep)
	}
	rep.finish(st.cancel.Load())
	return results, err
}

// ---------- 文件名搜索 ----------

// remoteEntry 是遍历得到的条目（ReadDir 的 FileInfo 已带大小/时间，无需逐个 Lstat）。
type remoteEntry struct {
	path    string
	base    string
	size    int64
	modTime time.Time
	isDir   bool
}

func (s *SFTPFileService) searchNames(sess *sftpSession, dir, pattern string, opts types.SearchOptions, excludes []*regexp.Regexp, st *searchState, rep *searchProgress) []types.SearchResult {
	if results, ok := s.findNames(sess, dir, pattern, opts, excludes, st, rep); ok {
		return results
	}

	results := make([]types.SearchResult, 0, 64)
	frontier := []string{dir}
	for len(frontier) > 0 && !st.cancel.Load() {
		next, entries := s.listLevel(sess, frontier, excludes, st, rep)
		batch := make([]types.SearchResult, 0, 16)
		for _, e := range entries {
			if !fileutil.MatchNameOpt(e.base, pattern, opts.CaseSensitive) {
				continue
			}
			batch = append(batch, types.SearchResult{
				Path: e.path, Name: e.base, Size: e.size, ModTime: e.modTime, IsDir: e.isDir,
			})
			if len(results)+len(batch) >= fileutil.MaxSearchResults {
				break
			}
		}
		results = append(results, batch...)
		rep.addResults(batch)
		if len(results) >= fileutil.MaxSearchResults {
			break
		}
		frontier = next
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })
	return results
}

// listLevel 并发 ReadDir 当前层的目录，返回下一层目录与按目录顺序排列的条目。
// 只依赖 SFTP ReadDir，不再对每个条目额外 Lstat，网络往返从 条目数+目录数 降为 目录数。
func (s *SFTPFileService) listLevel(sess *sftpSession, frontier []string, excludes []*regexp.Regexp, st *searchState, rep *searchProgress) ([]string, []remoteEntry) {
	type dirResult struct {
		infos []os.FileInfo
		err   error
	}
	out := make([]dirResult, len(frontier))
	sem := make(chan struct{}, searchWorkers)
	var wg sync.WaitGroup
	for i := range frontier {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			infos, err := sess.sftp.ReadDir(frontier[i])
			out[i] = dirResult{infos: infos, err: err}
		}(i)
	}
	wg.Wait()

	var next []string
	var entries []remoteEntry
	for i, r := range out {
		if r.err != nil {
			continue
		}
		rep.addDirs(1)
		infos := r.infos
		sort.Slice(infos, func(a, b int) bool { return infos[a].Name() < infos[b].Name() })
		for _, fi := range infos {
			p := path.Join(frontier[i], fi.Name())
			base := fi.Name()
			if fileutil.MatchesExclude(excludes, p, base) {
				continue
			}
			e := remoteEntry{path: p, base: base, size: fi.Size(), modTime: fi.ModTime(), isDir: fi.IsDir()}
			if e.isDir {
				next = append(next, p)
			} else {
				rep.addFiles(1)
			}
			entries = append(entries, e)
		}
	}
	return next, entries
}

// findNames 走服务端 find 快速路径（GNU find）。服务器不支持时返回 ok=false 回退。
func (s *SFTPFileService) findNames(sess *sftpSession, dir, pattern string, opts types.SearchOptions, excludes []*regexp.Regexp, st *searchState, rep *searchProgress) ([]types.SearchResult, bool) {
	sess.probeTools()
	if !sess.hasFind {
		return nil, false
	}
	cmd := fmt.Sprintf("find %s -mindepth 1 %s-printf %s 2>/dev/null",
		shQuote(dir), findPruneExpr, shQuote(`%y\t%s\t%T@\t%p\0`))

	session, err := sess.client.NewSession()
	if err != nil {
		return nil, false
	}
	defer session.Close()
	stdout, err := session.StdoutPipe()
	if err != nil {
		return nil, false
	}
	if err := session.Start(cmd); err != nil {
		return nil, false
	}

	results := make([]types.SearchResult, 0, 64)
	pending := make([]types.SearchResult, 0, searchBatchSize)
	flush := func() {
		rep.addResults(pending)
		pending = pending[:0]
	}
	reader := bufio.NewReaderSize(stdout, 64<<10)
	for {
		if st.cancel.Load() {
			flush()
			_ = session.Signal(xssh.SIGKILL)
			_ = session.Close()
			return results, true
		}
		rec, rerr := reader.ReadString(0)
		if rec != "" {
			rec = strings.TrimSuffix(rec, "\x00")
			if r := parseFindRecord(rec, pattern, opts.CaseSensitive, excludes, rep); r != nil {
				results = append(results, *r)
				pending = append(pending, *r)
				if len(pending) >= searchBatchSize {
					flush()
				}
				if len(results) >= fileutil.MaxSearchResults {
					flush()
					_ = session.Signal(xssh.SIGKILL)
					_ = session.Close()
					return results, true
				}
			}
		}
		if rerr != nil {
			break
		}
	}
	waitErr := session.Wait()
	if !remoteExitOk(waitErr) && len(results) == 0 {
		// find 不支持 -printf 等：回退到客户端并发遍历
		return nil, false
	}
	flush()
	sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })
	return results, true
}

// findPruneExpr 为默认排除目录生成 find 的 -prune 表达式（避免进入 node_modules 等）。
var findPruneExpr = func() string {
	var b strings.Builder
	for _, d := range fileutil.DefaultExcludeDirs {
		b.WriteString("-name ")
		b.WriteString(shQuote(d))
		b.WriteString(" -prune -o ")
	}
	return b.String()
}()

func parseFindRecord(rec, pattern string, caseSensitive bool, excludes []*regexp.Regexp, rep *searchProgress) *types.SearchResult {
	parts := strings.SplitN(rec, "\t", 4)
	if len(parts) != 4 {
		return nil
	}
	typ, sizeStr, mtStr, p := parts[0], parts[1], parts[2], parts[3]
	if p == "" {
		return nil
	}
	base := path.Base(p)
	if fileutil.MatchesExclude(excludes, p, base) {
		return nil
	}
	size, _ := strconv.ParseInt(sizeStr, 10, 64)
	mod := parseUnixFloat(mtStr)
	isDir := typ == "d"
	if !isDir {
		rep.addFiles(1)
	}
	if !fileutil.MatchNameOpt(base, pattern, caseSensitive) {
		return nil
	}
	return &types.SearchResult{Path: p, Name: base, Size: size, ModTime: mod, IsDir: isDir}
}

func parseUnixFloat(s string) time.Time {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return time.Time{}
	}
	sec := int64(f)
	nsec := int64((f - float64(sec)) * 1e9)
	return time.Unix(sec, nsec)
}

// ---------- 内容搜索 ----------

func (s *SFTPFileService) searchContent(sess *sftpSession, dir, pattern string, opts types.SearchOptions, excludes []*regexp.Regexp, st *searchState, rep *searchProgress) ([]types.SearchResult, error) {
	if results, ok := s.grepContent(sess, dir, pattern, opts, excludes, st, rep); ok {
		return results, nil
	}

	results := make([]types.SearchResult, 0, 64)
	frontier := []string{dir}
	for len(frontier) > 0 && !st.cancel.Load() && len(results) < fileutil.MaxSearchResults {
		next, entries := s.listLevel(sess, frontier, excludes, st, rep)
		cands := make([]remoteEntry, 0, len(entries))
		for _, e := range entries {
			if e.isDir || e.size <= 0 || e.size > fileutil.MaxContentSearchSize {
				continue
			}
			if fileutil.IsLikelyBinaryName(e.base) {
				continue
			}
			cands = append(cands, e)
		}
		hits, err := s.matchRemoteFiles(sess, cands, pattern, opts, st)
		if err != nil {
			return results, err
		}
		for _, r := range hits {
			results = append(results, r)
			if len(results) >= fileutil.MaxSearchResults {
				break
			}
		}
		rep.addResults(hits)
		frontier = next
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Path != results[j].Path {
			return results[i].Path < results[j].Path
		}
		return results[i].LineNo < results[j].LineNo
	})
	return results, nil
}

// matchRemoteFiles 并发读取候选文件并匹配内容（每层调用一次，结果按候选顺序返回）。
func (s *SFTPFileService) matchRemoteFiles(sess *sftpSession, cands []remoteEntry, pattern string, opts types.SearchOptions, st *searchState) ([]types.SearchResult, error) {
	if len(cands) == 0 {
		return nil, nil
	}
	perFile := make([][]types.SearchResult, len(cands))
	var (
		errMu    sync.Mutex
		firstErr error
	)
	jobs := make(chan int)
	var wg sync.WaitGroup
	workers := searchWorkers
	if len(cands) < workers {
		workers = len(cands)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if st.cancel.Load() {
					continue
				}
				c := cands[idx]
				f, oerr := sess.sftp.Open(c.path)
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
					errMu.Lock()
					if firstErr == nil {
						firstErr = herr
					}
					errMu.Unlock()
					continue
				}
				if len(hits) == 0 {
					continue
				}
				rs := make([]types.SearchResult, 0, len(hits))
				for _, h := range hits {
					rs = append(rs, types.SearchResult{
						Path: c.path, Name: c.base, Size: c.size, ModTime: c.modTime,
						LineNo: h.LineNo, Line: h.Line,
					})
				}
				perFile[idx] = rs
			}
		}()
	}
	for i := range cands {
		if st.cancel.Load() {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	results := make([]types.SearchResult, 0, 64)
	for _, rs := range perFile {
		results = append(results, rs...)
		if len(results) >= fileutil.MaxSearchResults {
			break
		}
	}
	return results, nil
}

// grepContent 走服务端 grep 快速路径（字面量模式、GNU/兼容 grep）。不支持时回退。
func (s *SFTPFileService) grepContent(sess *sftpSession, dir, pattern string, opts types.SearchOptions, excludes []*regexp.Regexp, st *searchState, rep *searchProgress) ([]types.SearchResult, bool) {
	if opts.UseRegex {
		return nil, false // 正则语义以 Go regexp 为准，不用 grep 近似
	}
	sess.probeTools()
	if !sess.hasGrep {
		return nil, false
	}
	var b strings.Builder
	b.WriteString("grep -rInZ -F")
	if !opts.CaseSensitive {
		b.WriteString(" -i")
	}
	for _, d := range fileutil.DefaultExcludeDirs {
		b.WriteString(" --exclude-dir=")
		b.WriteString(shQuote(d))
	}
	b.WriteString(" -e ")
	b.WriteString(shQuote(pattern))
	b.WriteString(" -- ")
	b.WriteString(shQuote(dir))
	b.WriteString(" 2>/dev/null")

	session, err := sess.client.NewSession()
	if err != nil {
		return nil, false
	}
	defer session.Close()
	stdout, err := session.StdoutPipe()
	if err != nil {
		return nil, false
	}
	if err := session.Start(b.String()); err != nil {
		return nil, false
	}

	results := make([]types.SearchResult, 0, 64)
	pending := make([]types.SearchResult, 0, searchBatchSize)
	lastFlush := time.Now()
	flush := func() {
		rep.addResults(pending)
		pending = pending[:0]
		lastFlush = time.Now()
	}
	reader := bufio.NewReaderSize(stdout, 64<<10)
	for {
		if st.cancel.Load() {
			flush()
			_ = session.Signal(xssh.SIGKILL)
			_ = session.Close()
			return results, true
		}
		name, rerr := reader.ReadString(0)
		if name == "" {
			break
		}
		p := strings.TrimSuffix(name, "\x00")
		rest, rerr2 := reader.ReadString('\n')
		rest = strings.TrimRight(rest, "\r\n")
		if p != "" && rest != "" {
			if r, ok := parseGrepLine(p, rest, excludes); ok {
				results = append(results, r)
				pending = append(pending, r)
				if len(pending) >= searchBatchSize || time.Since(lastFlush) >= searchFlushEvery {
					flush()
				}
				if len(results) >= fileutil.MaxSearchResults {
					flush()
					_ = session.Signal(xssh.SIGKILL)
					_ = session.Close()
					return results, true
				}
			}
		}
		if rerr != nil || rerr2 != nil {
			break
		}
	}
	waitErr := session.Wait()
	if !remoteExitOk(waitErr) && len(results) == 0 {
		// grep 不支持 -Z / -F 等：回退到客户端并发读取
		return nil, false
	}
	flush()
	sort.Slice(results, func(i, j int) bool {
		if results[i].Path != results[j].Path {
			return results[i].Path < results[j].Path
		}
		return results[i].LineNo < results[j].LineNo
	})
	return results, true
}

func parseGrepLine(p, rest string, excludes []*regexp.Regexp) (types.SearchResult, bool) {
	base := path.Base(p)
	if fileutil.MatchesExclude(excludes, p, base) {
		return types.SearchResult{}, false
	}
	colon := strings.IndexByte(rest, ':')
	if colon <= 0 {
		return types.SearchResult{}, false
	}
	lineNo, err := strconv.Atoi(rest[:colon])
	if err != nil || lineNo <= 0 {
		return types.SearchResult{}, false
	}
	line := rest[colon+1:]
	if len(line) > searchLineMax {
		line = line[:searchLineMax] + "…"
	}
	return types.SearchResult{
		Path: p, Name: base, IsDir: false, LineNo: lineNo, Line: line,
	}, true
}

// remoteExitOk 判断 find/grep 的退出码：0 正常，1 表示「无匹配 / 部分目录不可读」，
// 都不代表命令不可用（不可用时才会回退到客户端搜索）。
func remoteExitOk(err error) bool {
	if err == nil {
		return true
	}
	var ee *xssh.ExitError
	if errors.As(err, &ee) {
		return ee.ExitStatus() == 1
	}
	return false
}

// probeTools 探测服务端是否有可用的 GNU find / grep（每个连接只探测一次）。
func (sess *sftpSession) probeTools() {
	sess.toolOnce.Do(func() {
		sess.hasFind = runRemoteCommand(sess.client, "find . -maxdepth 0 -printf '' >/dev/null 2>&1") == nil
		sess.hasGrep = runRemoteCommand(sess.client, "printf x | grep -Z -F -e x >/dev/null 2>&1") == nil
	})
}