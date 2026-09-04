package main

import (
	"embed"
	"log"
	"os"
	"runtime"

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
	"changeme/app/service/localterminal"
	"changeme/app/service/secure"
	"changeme/app/service/settings"
	"changeme/app/service/sftp"
	"changeme/app/service/shellmenu"
	"changeme/app/service/sites"
	"changeme/app/service/sshconfig"
	"changeme/app/service/terminal"
	"changeme/app/service/tray"
	"changeme/app/service/types"
	"changeme/app/service/update"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

// 托盘 / 任务栏图标（PNG，Wails 会自动缩放到系统小图标尺寸）
//
//go:embed build/appicon.png
var appIcon []byte

func init() {
	application.RegisterEvent[types.TerminalOutput]("terminal:output")
	application.RegisterEvent[types.TerminalExit]("terminal:exit")
	application.RegisterEvent[types.TerminalOutput]("localTerminal:output")
	application.RegisterEvent[types.TerminalExit]("localTerminal:exit")
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

	// 系统右键菜单：解析首次启动携带的打开请求（--terminal <path> / --editor <path>），
	// 先暂存到服务里，等前端挂载后通过 Consume 取走。
	shellMenuSvc := &shellmenu.ShellMenuService{}
	if req := shellmenu.ParseArgs(os.Args[1:]); req != nil {
		shellMenuSvc.SetPending(req)
	}

	var win *application.WebviewWindow

	app := application.New(application.Options{
		Name:        "spark 终端",
		Description: "终端 - SSH / SFTP / FTP",
		Services: []application.Service{
			application.NewService(&terminal.TerminalService{}),
			application.NewService(&localterminal.LocalTerminalService{}),
			application.NewService(&sftp.SFTPFileService{}),
			application.NewService(&ftp.FTPFileService{}),
			application.NewService(&connections.ConnService{}),
			application.NewService(&customcmd.CustomCommandService{}),
			application.NewService(&documents.DocumentService{}),
			application.NewService(&favorites.FavoriteService{}),
			application.NewService(&settings.SettingsService{}),
			application.NewService(&sites.SiteService{}),
			application.NewService(&databases.DatabaseService{}),
			application.NewService(&local.LocalService{}),
			application.NewService(&hostkeys.HostKeyService{}),
			application.NewService(&sshconfig.SshConfigService{}),
			application.NewService(&update.UpdateService{}),
			application.NewService(shellMenuSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Icon: appIcon,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{
			// 让 WebView2 原生忽略 SSL 证书错误（自签名 / 过期 / 无效证书）。
			// 内嵌浏览器（iframe）与「窗口打开」（顶层 WebView）都直接加载目标 URL，
			// 证书统一由这个开关忽略，不再走本地代理。全局生效。
			AdditionalBrowserArgs: []string{"--ignore-certificate-errors"},
		},
		// 单实例：系统右键菜单 / 二次双击启动都会把参数转发到已运行的实例。
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "spark-terminal-desktop",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				if req := shellmenu.ParseArgs(data.Args); req != nil {
					shellMenuSvc.SetPending(req)
					application.Get().Event.Emit("app:open", req)
				}
				// 无论是否带打开参数，二次启动都唤起主窗口。
				if win != nil {
					tray.Show(win)
				}
			},
		},
	})

	// 开机启动：设置里开启过则每次启动重新登记，使启动项始终指向当前 exe
	// （升级 / 更换安装路径后自动修复；官方推荐做法，见
	// https://v3.wails.io/features/autostart/basics/ —— Enable 可安全重复调用）。
	// 必须在数据库就绪（settings 表可读）之后执行。
	if settings.GetString(settings.KeyAutoStartEnabled, "") == "1" {
		if err := app.Autostart.Enable(); err != nil {
			log.Printf("登记开机启动失败: %v", err)
		}
	}

	// 注册系统右键菜单（HKCU，跟随当前 exe 路径，升级后自动更新）。
	if exe, err := os.Executable(); err == nil {
		if err := shellmenu.Register(exe); err != nil {
			log.Printf("注册系统右键菜单失败: %v", err)
		}
	} else {
		log.Printf("获取可执行文件路径失败，跳过系统右键菜单注册: %v", err)
	}

	// 内置更新器：仅桌面端初始化（安卓 / iOS 跳过，前端也已隐藏更新入口）
	if runtime.GOOS != "android" && runtime.GOOS != "ios" {
		if err := update.Init(); err != nil {
			log.Printf("初始化更新器失败: %v", err)
		}
	}

	win = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Spark",
		Width:  1380,
		Height: 880,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour:           application.NewRGB(18, 18, 24),
		DefaultContextMenuDisabled: true,
		// 允许从资源管理器拖入文件/目录（配合前端 files:dropped 事件实现面板上传）
		EnableFileDrop: true,
		// F12 打开调试工具：注册到原生层，内嵌站点 iframe 获得焦点时同样生效，
		// 便于调试「站点管理」中打开的内嵌页面。
		KeyBindings: map[string]func(window application.Window){
			"F12": func(window application.Window) {
				window.OpenDevTools()
			},
		},
		URL: "/",
	})

	// 托盘图标 + 关闭按钮行为（缩小到托盘 / 直接退出，见 设置 → 通用）
	tray.Setup(app, win, appIcon)

	// 外部拖拽上传：Wails 原生文件拖放（EnableFileDrop）把拖入的绝对路径
	// 通过窗口事件送达这里，转发给前端（files:dropped），由前端按落点
	// 坐标找到对应的远程面板并上传。
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		ctx := event.Context()
		if ctx == nil {
			return
		}
		files := ctx.DroppedFiles()
		if len(files) == 0 {
			return
		}
		x, y := 0, 0
		if d := ctx.DropTargetDetails(); d != nil {
			x, y = d.X, d.Y
		}
		app.Event.Emit("files:dropped", map[string]any{
			"filenames": files,
			"x":         x,
			"y":         y,
		})
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
