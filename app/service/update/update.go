// Package update exposes application self-update to the frontend.
//
// 桌面端（Windows/macOS/Linux）走 Wails v3 内置 updater（app.Updater）：
// Check → DownloadAndInstall → Restart（自动替换二进制并重启）。
//
// 安卓端走另一条路：Check 用 GitHub Releases API 找最新 APK，DownloadApk
// 下载 APK 到应用私有目录，最后由前端调用 window.wails.installApk(path)
// （Java 桥，见 build/android）调起系统安装器。
package update

import (
	"changeme/app/service/version"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// Repo is the GitHub repository that hosts releases (owner/name).
const Repo = "ligaolin/spark"

// UpdateInfo describes the latest release compared with the running version.
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"hasUpdate"`
	Name      string `json:"name"`
	Body      string `json:"body"`
}

// UpdateService exposes update checks and installs to the frontend.
type UpdateService struct{}

// ServiceName implements application.ServiceName.
func (s *UpdateService) ServiceName() string { return "UpdateService" }

// Init configures the desktop updater once. Called from main.go on desktop
// only (main.go guards it with runtime.GOOS).
func Init() error {
	provider, err := github.New(github.Config{Repository: Repo})
	if err != nil {
		return err
	}
	return application.Get().Updater.Init(updater.Config{
		CurrentVersion: version.Version,
		Providers:      []updater.Provider{provider},
		Window:         updater.WindowNone, // 前端用自定义对话框
	})
}

// CheckUpdate checks for a newer release. When up to date it returns
// HasUpdate=false with a nil error.
func (s *UpdateService) CheckUpdate() (*UpdateInfo, error) { return checkUpdate() }

// DownloadAndInstall downloads, verifies and stages the desktop update.
func (s *UpdateService) DownloadAndInstall() error { return downloadAndInstall() }

// Restart swaps the staged desktop binary into place and restarts the app.
func (s *UpdateService) Restart() error { return restart() }

// DownloadApk downloads the latest Android APK and returns its local path.
func (s *UpdateService) DownloadApk() (string, error) { return downloadApk() }

// OpenReleasePage opens the GitHub release page (system browser on desktop,
// system intent on mobile).
func (s *UpdateService) OpenReleasePage() error {
	url := "https://github.com/" + Repo + "/releases/latest"
	if application.System.IsMobile() {
		application.Mobile.OpenURL(url)
		return nil
	}
	return application.Get().Browser.OpenURL(url)
}
