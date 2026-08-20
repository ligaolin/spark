// Package local exposes local filesystem operations and native dialogs
// to the frontend (used by the file-manager local pane).
package local

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

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
