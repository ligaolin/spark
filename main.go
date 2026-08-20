package main

import (
	"embed"
	"log"

	"changeme/app/model"
	"changeme/app/service/connections"
	"changeme/app/service/customcmd"
	"changeme/app/service/databases"
	"changeme/app/service/db"
	"changeme/app/service/documents"
	"changeme/app/service/favorites"
	"changeme/app/service/ftp"
	"changeme/app/service/hostkeys"
	"changeme/app/service/local"
	"changeme/app/service/secure"
	"changeme/app/service/settings"
	"changeme/app/service/sftp"
	"changeme/app/service/sshconfig"
	"changeme/app/service/terminal"
	"changeme/app/service/types"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[types.TerminalOutput]("terminal:output")
	application.RegisterEvent[types.TerminalExit]("terminal:exit")
	application.RegisterEvent[types.TransferProgress]("transfer:progress")
	application.RegisterEvent[types.SessionClosed]("session:closed")
}

func main() {
	if err := db.InitDB(); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	// 应用启动：按已保存的数据库配置连接目标库（远程库不可用时回退本地）
	if cfg, ok := databases.Load(); ok {
		seed := databases.KeySeedFor(cfg)
		if cfg.Dialect == "" || cfg.Dialect == "sqlite" {
			secure.SetKeySeed(seed)
		} else if err := db.Reconnect(cfg.Dialect, cfg.DSN()); err != nil {
			log.Printf("连接远程数据库失败，本次使用本地数据库: %v", err)
		} else {
			secure.SetKeySeed(seed)
		}
	}

	model.Migrate()

	app := application.New(application.Options{
		Name:        "spark",
		Description: "终端工具 - SSH / SFTP / FTP",
		Services: []application.Service{
			application.NewService(&terminal.TerminalService{}),
			application.NewService(&sftp.SFTPFileService{}),
			application.NewService(&ftp.FTPFileService{}),
			application.NewService(&connections.ConnService{}),
			application.NewService(&customcmd.CustomCommandService{}),
			application.NewService(&documents.DocumentService{}),
			application.NewService(&favorites.FavoriteService{}),
			application.NewService(&settings.SettingsService{}),
			application.NewService(&databases.DatabaseService{}),
			application.NewService(&local.LocalService{}),
			application.NewService(&hostkeys.HostKeyService{}),
			application.NewService(&sshconfig.SshConfigService{}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Spark 终端工具",
		Width:  1380,
		Height: 880,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour:           application.NewRGB(18, 18, 24),
		DefaultContextMenuDisabled: true,
		URL:                        "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
