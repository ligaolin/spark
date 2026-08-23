// Package tray wires the notification-area (system tray) icon and implements
// the configurable behaviour of the window close button.
//
// Two behaviours are supported, selectable at runtime from 设置 → 通用:
//
//	minimize (默认) 点关闭按钮把窗口隐藏到任务栏右下角的托盘区，程序继续在后台运行
//	exit            点关闭按钮直接退出程序
//
// The tray icon is always created, so a hidden window can be brought back with
// a left click, and the app can always be quit from the right-click menu.
package tray

import (
	"sync/atomic"

	"changeme/app/service/settings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// SettingKey is the key in the settings table holding the close-button behaviour.
const SettingKey = "window.closeAction"

// Supported values of SettingKey.
const (
	// ActionMinimize hides the window to the notification area.
	ActionMinimize = "minimize"
	// ActionExit quits the application.
	ActionExit = "exit"
)

// tooltip / menu labels.
const trayTooltip = "Spark 终端"

// quitting is set once a real shutdown has started. The WindowClosing hook must
// not veto those closes, otherwise App.Quit's cleanup (which closes every
// window) would be blocked and the tray icon / services would never be torn
// down.
var quitting atomic.Bool

// CloseAction returns the configured close-button behaviour. Anything other
// than an explicit "exit" is treated as "minimize", which is the default.
func CloseAction() string {
	if settings.GetString(SettingKey, ActionMinimize) == ActionExit {
		return ActionExit
	}
	return ActionMinimize
}

// Setup installs the close-button interception on win and creates the tray
// icon. icon is the raw PNG/ICO image shown in the notification area; when
// empty, Wails falls back to the application icon.
//
// It must be called after the window exists but before app.Run: the tray is
// created lazily by Wails once the application is running.
func Setup(app *application.App, win *application.WebviewWindow, icon []byte) {
	installCloseHook(app, win)
	installTray(app, win, icon)
}

// installCloseHook makes the close button honour the configured action.
//
// The hook runs before Wails' own WindowClosing listener (the one that destroys
// the window), so cancelling the event is what keeps the window alive.
func installCloseHook(app *application.App, win *application.WebviewWindow) {
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		// A shutdown is already in progress: let the window close for real.
		if quitting.Load() {
			return
		}

		// Always veto the native close first, then decide what to do. Quitting
		// through App.Quit (instead of letting the last window close the app)
		// makes sure the tray icon is removed and the services are shut down.
		e.Cancel()

		if CloseAction() == ActionExit {
			Quit(app)
			return
		}
		Hide(win)
	})
}

// installTray creates the notification-area icon and its right-click menu.
func installTray(app *application.App, win *application.WebviewWindow, icon []byte) {
	menu := application.NewMenu()
	menu.Add("显示主窗口").OnClick(func(*application.Context) { Show(win) })
	menu.Add("隐藏到托盘").OnClick(func(*application.Context) { Hide(win) })
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { Quit(app) })

	tray := app.SystemTray.New()
	tray.SetTooltip(trayTooltip)
	if len(icon) > 0 {
		tray.SetIcon(icon)
	}
	tray.SetMenu(menu)

	// Left click restores the window, right click opens the menu (which holds
	// 退出). Both handlers are set explicitly so Wails does not apply its
	// "tray popover" defaults, which would reposition the main window next to
	// the tray icon.
	tray.OnClick(func() { Show(win) })
	tray.OnDoubleClick(func() { Show(win) })
	tray.OnRightClick(func() { tray.OpenMenu() })
}

// Show brings the window back from the tray (or from a minimised state) and
// gives it focus.
func Show(win *application.WebviewWindow) {
	if win == nil {
		return
	}
	if win.IsMinimised() {
		win.UnMinimise()
	}
	win.Show()
	win.Focus()
}

// Hide hides the window; the tray icon stays available to bring it back.
func Hide(win *application.WebviewWindow) {
	if win == nil {
		return
	}
	win.Hide()
}

// Quit shuts the application down for real, bypassing the close hook.
//
// App.Quit runs its cleanup on the main thread; calling it from a goroutine
// keeps that work out of the window-message handler we may currently be in.
func Quit(app *application.App) {
	if app == nil {
		return
	}
	quitting.Store(true)
	go app.Quit()
}
