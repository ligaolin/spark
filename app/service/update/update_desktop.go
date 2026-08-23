//go:build !android && !ios

package update

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func checkUpdate() (*UpdateInfo, error) {
	u := application.Get().Updater
	rel, err := u.Check(context.Background())
	if err != nil {
		return nil, err
	}
	if rel == nil {
		return &UpdateInfo{Current: u.CurrentVersion(), HasUpdate: false}, nil
	}
	return &UpdateInfo{
		Current:   u.CurrentVersion(),
		Latest:    rel.Version,
		HasUpdate: true,
		Name:      rel.Name,
		Body:      rel.Notes,
	}, nil
}

func downloadAndInstall() error {
	return application.Get().Updater.DownloadAndInstall(context.Background())
}

func restart() error {
	return application.Get().Updater.Restart(context.Background())
}

func downloadApk() (string, error) {
	return "", errors.New("APK 下载仅支持安卓端")
}
