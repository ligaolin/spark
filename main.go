package main

import (
	"embed"
	"log"
	"os"
	"runtime"

	"spark/app/model"
	"spark/app/service/agent"
	"spark/app/service/ai"
	"spark/app/service/connections"
	"spark/app/service/customcmd"
	"spark/app/service/databases"
	"spark/app/service/db"
	"spark/app/service/documents"
	"spark/app/service/favorites"
	"spark/app/service/ftp"
	"spark/app/service/hostkeys"
	"spark/app/service/local"
	"spark/app/service/localterminal"
	"spark/app/service/rest"
	"spark/app/service/secure"
	"spark/app/service/settings"
	"spark/app/service/sftp"
	"spark/app/service/shellmenu"
	"spark/app/service/sites"
	"spark/app/service/sshconfig"
	"spark/app/service/terminal"
	"spark/app/service/tray"
	"spark/app/service/types"
	"spark/app/service/update"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

// 窗口尺寸记忆的设置键
const (
	keyWinWidth  = "window.width"
	keyWinHeight = "window.height"
)

func initEvents() {
	application.RegisterEvent[types.TerminalOutput]("terminal:output")
	application.RegisterEvent[types.TerminalExit]("terminal:exit")
	application.RegisterEvent[types.TerminalOutput]("localTerminal:output")
	application.RegisterEvent[types.TerminalExit]("localTerminal:exit")
	application.RegisterEvent[types.TransferProgress]("transfer:progress")
	application.RegisterEvent[types.SessionClosed]("session:closed")
	application.RegisterEvent[types.AIChatDelta]("ai:delta")
	application.RegisterEvent[types.AgentReply]("agent:reply")
	application.RegisterEvent[types.AgentStep]("agent:step")
	application.RegisterEvent[types.AgentAsk]("agent:ask")
	application.RegisterEvent[types.AgentOutput]("agent:output")
	application.RegisterEvent[types.AgentDone]("agent:done")
}

func initDatabase() {
	if err := db.InitDB(); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
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
}

func initAutoStart(app *application.App) {
	if settings.GetString(settings.KeyAutoStartEnabled, "") == "1" {
		if err := app.Autostart.Enable(); err != nil {
			log.Printf("登记开机启动失败: %v", err)
		}
	}
}

func initShellMenu() {
	if exe, err := os.Executable(); err == nil {
		if err := shellmenu.Register(exe); err != nil {
			log.Printf("注册系统右键菜单失败: %v", err)
		}
	} else {
		log.Printf("获取可执行文件路径失败，跳过系统右键菜单注册: %v", err)
	}
}

func initUpdater() {
	if runtime.GOOS != "android" && runtime.GOOS != "ios" {
		if err := update.Init(); err != nil {
			log.Printf("初始化更新器失败: %v", err)
		}
	}
}

func createServices(termSvc *terminal.TerminalService, shellMenuSvc *shellmenu.ShellMenuService) []application.Service {
	return []application.Service{
		application.NewService(&ai.AIService{}),
		application.NewService(&agent.AgentService{}),
		application.NewService(termSvc),
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
		application.NewService(&rest.RestService{}),
	}
}

func createWindow(app *application.App) *application.WebviewWindow {
	w := settings.GetInt(keyWinWidth, 1380)
	h := settings.GetInt(keyWinHeight, 880)
	if w < 640 {
		w = 640
	}
	if h < 480 {
		h = 480
	}

	// 根据当前主题设置窗口背景色及标题栏明暗
	bgR, bgG, bgB := uint8(18), uint8(18), uint8(24)
	winTheme := application.Dark
	theme := settings.GetString("app.theme", "dark")
	if theme == "light" {
		bgR, bgG, bgB = 248, 250, 252
		winTheme = application.Light
	}

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Spark",
		Width:  w,
		Height: h,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		Windows: application.WindowsWindow{
			Theme: winTheme,
		},
		BackgroundColour:           application.NewRGB(bgR, bgG, bgB),
		DefaultContextMenuDisabled: true,
		EnableFileDrop:             true,
		KeyBindings: map[string]func(window application.Window){
			"F12": func(window application.Window) {
				window.OpenDevTools()
			},
		},
		URL: "/",
	})

	settings.ApplyWindowTheme(win, theme)

	// 关闭窗口时记忆尺寸
	win.OnWindowEvent(events.Common.WindowClosing, func(event *application.WindowEvent) {
		ww, wh := win.Size()
		if ww > 0 && wh > 0 {
			_ = settings.Set(keyWinWidth, itoa(ww))
			_ = settings.Set(keyWinHeight, itoa(wh))
		}
	})

	return win
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	digits := make([]byte, 0, 12)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func init() {
	initEvents()
}

func main() {
	initDatabase()

	shellMenuSvc := &shellmenu.ShellMenuService{}
	if req := shellmenu.ParseArgs(os.Args[1:]); req != nil {
		shellMenuSvc.SetPending(req)
	}

	termSvc := &terminal.TerminalService{}
	terminal.SetGlobal(termSvc)

	var win *application.WebviewWindow

	app := application.New(application.Options{
		Name:        "spark 终端",
		Description: "终端 - SSH / SFTP / FTP",
		Services:    createServices(termSvc, shellMenuSvc),
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Icon: appIcon,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{
			AdditionalBrowserArgs: []string{"--ignore-certificate-errors"},
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "spark-terminal-desktop",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				if req := shellmenu.ParseArgs(data.Args); req != nil {
					shellMenuSvc.SetPending(req)
					application.Get().Event.Emit("app:open", req)
				}
				if win != nil {
					tray.Show(win)
				}
			},
		},
	})

	initAutoStart(app)
	initShellMenu()
	initUpdater()

	win = createWindow(app)

	tray.Setup(app, win, appIcon)

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
