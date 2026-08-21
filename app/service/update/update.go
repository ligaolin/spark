// Package update checks GitHub Releases for a newer version of the app and
// downloads the Windows release asset, so the frontend can prompt the user
// to update with a single click.
package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"changeme/app/service/version"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Repo is the GitHub repository that hosts releases (owner/name).
const Repo = "ligaolin/spark"

// UpdateInfo describes the latest release compared with the running version.
type UpdateInfo struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	HasUpdate   bool   `json:"hasUpdate"`
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	ReleaseURL  string `json:"releaseUrl"`
	AssetURL    string `json:"assetUrl"`
	AssetName   string `json:"assetName"`
	AssetSize   int64  `json:"assetSize"`
	PublishedAt string `json:"publishedAt"`
	Body        string `json:"body"`
}

// UpdateProgress is emitted while the update is being downloaded.
type UpdateProgress struct {
	Done  int64 `json:"done"`
	Total int64 `json:"total"`
}

// UpdateService exposes update checks and downloads to the frontend.
type UpdateService struct{}

// ServiceName implements application.ServiceName.
func (s *UpdateService) ServiceName() string { return "UpdateService" }

var httpClient = &http.Client{Timeout: 30 * time.Second}

type ghRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// CheckUpdate queries the GitHub Releases API and reports whether a newer
// version exists. Network errors are returned as errors (the frontend treats
// them as "check failed" and stays quiet).
func (s *UpdateService) CheckUpdate() (*UpdateInfo, error) {
	cur := version.Version
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "spark-terminal/"+cur)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 GitHub 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// 仓库还没有任何 release / 限流等情况
		return nil, fmt.Errorf("GitHub 返回状态码 %d", resp.StatusCode)
	}

	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("解析版本信息失败: %w", err)
	}
	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	info := &UpdateInfo{
		Current:     cur,
		Latest:      latest,
		HasUpdate:   compareVersions(latest, cur) > 0,
		Tag:         rel.TagName,
		Name:        rel.Name,
		ReleaseURL:  rel.HTMLURL,
		PublishedAt: rel.PublishedAt,
		Body:        strings.TrimSpace(rel.Body),
	}
	for _, a := range rel.Assets {
		if isWindowsAsset(a.Name) {
			info.AssetURL = a.BrowserDownloadURL
			info.AssetName = a.Name
			info.AssetSize = a.Size
			break
		}
	}
	if info.AssetURL == "" && len(rel.Assets) > 0 {
		info.AssetURL = rel.Assets[0].BrowserDownloadURL
		info.AssetName = rel.Assets[0].Name
		info.AssetSize = rel.Assets[0].Size
	}
	return info, nil
}

// DownloadUpdate downloads the latest release asset into the user's
// Downloads folder (falling back to the home directory) and returns the
// local file path. Progress is streamed through the "update:progress" event.
func (s *UpdateService) DownloadUpdate() (string, error) {
	info, err := s.CheckUpdate()
	if err != nil {
		return "", err
	}
	if info.AssetURL == "" {
		return "", errors.New("发布中没有找到可下载的安装包")
	}
	dir, err := defaultDownloadDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, info.AssetName)

	req, err := http.NewRequest(http.MethodGet, info.AssetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "spark-terminal/"+info.Current)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载失败：服务器返回状态码 %d", resp.StatusCode)
	}

	total := resp.ContentLength
	if total <= 0 {
		total = info.AssetSize
	}
	out, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("创建文件失败: %w", err)
	}
	defer out.Close()

	buf := make([]byte, 256<<10)
	var done int64
	last := time.Time{}
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return "", fmt.Errorf("写入文件失败: %w", werr)
			}
			done += int64(n)
			if time.Since(last) >= 150*time.Millisecond || (total > 0 && done >= total) {
				last = time.Now()
				application.Get().Event.Emit("update:progress", UpdateProgress{Done: done, Total: total})
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", fmt.Errorf("下载中断: %w", rerr)
		}
	}
	if total > 0 && done >= total {
		application.Get().Event.Emit("update:progress", UpdateProgress{Done: done, Total: total})
	}
	return dest, nil
}

// RevealInExplorer opens the file manager with the given path selected
// (Windows: explorer /select,; other platforms open the parent folder).
func (s *UpdateService) RevealInExplorer(path string) error {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("explorer", "/select,", path)
		if err := cmd.Start(); err != nil {
			return err
		}
		return nil
	}
	return openPath(filepath.Dir(path))
}

// LaunchApp starts a downloaded executable (e.g. the new version). The
// current app keeps running; the user closes it and switches to the new one.
func (s *UpdateService) LaunchApp(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	cmd := exec.Command(path)
	return cmd.Start()
}

// OpenReleasePage opens the GitHub release page in the system browser.
func (s *UpdateService) OpenReleasePage() error {
	return application.Get().Browser.OpenURL("https://github.com/" + Repo + "/releases/latest")
}

func isWindowsAsset(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "windows") && strings.HasSuffix(n, ".exe")
}

func defaultDownloadDir() (string, error) {
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

func openPath(path string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("explorer", path).Start()
	}
	cmd := exec.Command("open", path) // macOS
	if runtime.GOOS == "linux" {
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// compareVersions compares two version strings (e.g. "1.2.3", "v1.2.3",
// "1.2.3-beta.1"). Returns >0 when a > b. A "dev" version is treated as
// 0.0.0 so any tagged release counts as newer.
func compareVersions(a, b string) int {
	pa := parseVersion(a)
	pb := parseVersion(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(pa) {
			av = pa[i]
		}
		if i < len(pb) {
			bv = pb[i]
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	// 数值相同：带预发布后缀的版本比正式版旧（如 1.2.3-beta.1 < 1.2.3）
	sa := prereleaseSuffix(a)
	sb := prereleaseSuffix(b)
	if sa == "" && sb != "" {
		return 1
	}
	if sa != "" && sb == "" {
		return -1
	}
	return strings.Compare(sa, sb)
}

// parseVersion 解析版本号为数字分段列表（"v1.2.3-beta.1" -> [1,2,3]）。
func parseVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.SplitN(v, "-", 2)
	nums := strings.Split(parts[0], ".")
	out := make([]int, 0, len(nums))
	for _, s := range nums {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			break
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		out = []int{0}
	}
	return out
}

func prereleaseSuffix(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '-'); i >= 0 {
		return strings.ToLower(v[i+1:])
	}
	return ""
}
