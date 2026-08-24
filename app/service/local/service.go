// Package local exposes local filesystem operations and native dialogs
// to the frontend (used by the file-manager local pane).
package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"changeme/app/service/fileutil"
	"changeme/app/service/types"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// LocalService provides local directory listing, dialogs and simple file ops.
type LocalService struct{}

// ServiceName implements application.ServiceName.
func (l *LocalService) ServiceName() string { return "LocalService" }

// Home returns the current user's home directory.
func (l *LocalService) Home() (string, error) {
	return os.UserHomeDir()
}

// List returns the entries of a local directory (directories first, then by name).
func (l *LocalService) List(dir string) ([]types.FileEntry, error) {
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	dir = filepath.Clean(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]types.FileEntry, 0, len(entries))
	for _, de := range entries {
		fi, err := de.Info()
		if err != nil {
			continue
		}
		e := types.FileEntry{
			Name:    de.Name(),
			Path:    filepath.Join(dir, de.Name()),
			Size:    fi.Size(),
			Mode:    fi.Mode().String(),
			ModTime: fi.ModTime(),
			IsDir:   de.IsDir(),
		}
		if de.Type()&os.ModeSymlink != 0 {
			e.Symlink = true
			if target, err := os.Readlink(e.Path); err == nil {
				e.LinkTarget = target
			}
			if st, err := os.Stat(e.Path); err == nil {
				e.IsDir = st.IsDir()
			}
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// PickFiles opens a native multi-select file dialog for uploads.
func (l *LocalService) PickFiles() ([]string, error) {
	return application.Get().Dialog.OpenFile().
		SetTitle("选择要上传的文件").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		PromptForMultipleSelection()
}

// PickOpenFile opens a native single-file dialog for reading a local file.
func (l *LocalService) PickOpenFile(title string) (string, error) {
	if strings.TrimSpace(title) == "" {
		title = "选择文件"
	}
	return application.Get().Dialog.OpenFile().
		SetTitle(title).
		CanChooseFiles(true).
		CanChooseDirectories(false).
		PromptForSingleSelection()
}

// ReadTextFile reads a local text file (supports leading ~ for home dir).
func (l *LocalService) ReadTextFile(path string) (string, error) {
	path = expandUserPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func expandUserPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// PickDirectory opens a native directory chooser.
func (l *LocalService) PickDirectory() (string, error) {
	return application.Get().Dialog.OpenFile().
		SetTitle("选择目录").
		CanChooseFiles(false).
		CanChooseDirectories(true).
		PromptForSingleSelection()
}

// DefaultDownloadDir returns the user's Downloads folder (falls back to the
// home directory). Used as the save location when the user cancels the
// directory picker during a download.
func (l *LocalService) DefaultDownloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Downloads")
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir, nil
	}
	return home, nil
}

// PickSaveFile opens a native save dialog with a suggested filename.
func (l *LocalService) PickSaveFile(defaultName string) (string, error) {
	return application.Get().Dialog.SaveFile().
		SetFilename(defaultName).
		PromptForSingleSelection()
}

// Mkdir creates a local directory (including missing parents).
func (l *LocalService) Mkdir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// Rename renames or moves a local path.
func (l *LocalService) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

// Remove deletes a local file or directory (recursively).
func (l *LocalService) Remove(p string) error {
	return os.RemoveAll(p)
}

// Search walks a local directory recursively and returns filename matches
// (mode "name") or content matches (mode "content"). opts controls case
// sensitivity and regex (content only).
func (l *LocalService) Search(dir, pattern, mode string, opts types.SearchOptions) ([]types.SearchResult, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, errors.New("请输入搜索关键字")
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	dir = filepath.Clean(dir)
	contentMode := strings.EqualFold(mode, "content")
	if contentMode {
		if err := fileutil.ValidateContentPattern(pattern, opts.CaseSensitive, opts.UseRegex); err != nil {
			return nil, err
		}
	}
	excludes := fileutil.CompileExcludes(opts.Exclude)
	results := make([]types.SearchResult, 0, 64)
	err := filepath.Walk(dir, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			return nil // 跳过不可读的目录/文件
		}
		if p == dir {
			return nil // 跳过根目录本身
		}
		name := filepath.Base(p)
		if fileutil.MatchesExclude(excludes, p, name) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if len(results) >= fileutil.MaxSearchResults {
			return filepath.SkipAll
		}
		if info.IsDir() {
			if !contentMode && fileutil.MatchNameOpt(name, pattern, opts.CaseSensitive) {
				results = append(results, types.SearchResult{
					Path: p, Name: name, Size: info.Size(), ModTime: info.ModTime(), IsDir: true,
				})
			}
			return nil
		}
		if contentMode {
			if info.Size() <= 0 || info.Size() > fileutil.MaxContentSearchSize {
				return nil
			}
			data, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			if fileutil.IsBinary(data) {
				return nil
			}
			hits, herr := fileutil.MatchLinesOpt(data, pattern, fileutil.MaxMatchesPerFile, opts.CaseSensitive, opts.UseRegex)
			if herr != nil {
				return herr
			}
			for _, h := range hits {
				results = append(results, types.SearchResult{
					Path: p, Name: name, Size: info.Size(), ModTime: info.ModTime(),
					LineNo: h.LineNo, Line: h.Line,
				})
				if len(results) >= fileutil.MaxSearchResults {
					return filepath.SkipAll
				}
			}
			return nil
		}
		if fileutil.MatchNameOpt(name, pattern, opts.CaseSensitive) {
			results = append(results, types.SearchResult{
				Path: p, Name: name, Size: info.Size(), ModTime: info.ModTime(),
			})
		}
		return nil
	})
	return results, err
}

// Replace replaces every content match of pattern in files under dir,
// returning how many files and occurrences were changed.
func (l *LocalService) Replace(dir, pattern, replacement, mode string, opts types.SearchOptions) (types.ReplaceResult, error) {
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
		dir, _ = os.UserHomeDir()
	}
	dir = filepath.Clean(dir)
	excludes := fileutil.CompileExcludes(opts.Exclude)
	res := types.ReplaceResult{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			return nil
		}
		if p == dir {
			return nil
		}
		name := filepath.Base(p)
		if fileutil.MatchesExclude(excludes, p, name) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() || info.Size() <= 0 || info.Size() > fileutil.MaxContentSearchSize {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		if fileutil.IsBinary(data) {
			return nil
		}
		out, n, cerr := fileutil.ReplaceAllContent(data, pattern, replacement, opts.CaseSensitive, opts.UseRegex)
		if cerr != nil {
			return cerr
		}
		if n == 0 {
			return nil
		}
		if werr := os.WriteFile(p, out, info.Mode().Perm()); werr != nil {
			return werr
		}
		res.Files++
		res.Occurrences += n
		return nil
	})
	return res, err
}

// ReadFile reads a local text file for the built-in editor, enforcing a size
// limit and rejecting binary content.
func (l *LocalService) ReadFile(path string) (string, error) {
	path = expandUserPath(path)
	data, err := os.ReadFile(path)
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

// WriteFile writes text content to a local file.
func (l *LocalService) WriteFile(path, content string) error {
	path = expandUserPath(path)
	return os.WriteFile(path, []byte(content), 0o644)
}
