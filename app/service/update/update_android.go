//go:build android

package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"changeme/app/service/version"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

func androidProvider() (*github.Provider, error) {
	return github.New(github.Config{Repository: Repo})
}

func androidCheckRequest() updater.CheckRequest {
	return updater.CheckRequest{
		CurrentVersion: version.Version,
		Platform:       "android",
		Arch:           runtime.GOARCH,
	}
}

func checkUpdate() (*UpdateInfo, error) {
	provider, err := androidProvider()
	if err != nil {
		return nil, err
	}
	rel, err := provider.Check(context.Background(), androidCheckRequest())
	if err != nil {
		return nil, err
	}
	if rel == nil {
		return &UpdateInfo{Current: version.Version, HasUpdate: false}, nil
	}
	return &UpdateInfo{
		Current:   version.Version,
		Latest:    rel.Version,
		HasUpdate: true,
		Name:      rel.Name,
		Body:      rel.Notes,
	}, nil
}

func downloadAndInstall() error {
	return errors.New("安卓端请使用「下载更新」后「安装」")
}

func restart() error {
	return errors.New("安卓端不支持重启更新")
}

// downloadApk 下载最新 APK 到应用私有 files/updates 目录（FileProvider 映射
// 为 content://<authority>/updates/spark-update.apk），返回绝对路径。
func downloadApk() (string, error) {
	provider, err := androidProvider()
	if err != nil {
		return "", err
	}
	rel, err := provider.Check(context.Background(), androidCheckRequest())
	if err != nil {
		return "", err
	}
	if rel == nil {
		return "", errors.New("已是最新版本")
	}

	base := application.Mobile.StoragePath()
	dir := filepath.Join(base, "updates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, "spark-update.apk")

	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	err = provider.Download(context.Background(), rel, out, func(written, total int64) {
		application.Get().Event.Emit("wails:updater:download-progress", map[string]int64{
			"written": written,
			"total":   total,
		})
	})
	closeErr := out.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	return dest, nil
}
