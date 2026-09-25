//go:build windows

package settings

import (
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var (
	dwmapi                    = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")

	user32           = syscall.NewLazyDLL("user32.dll")
	procSetWindowPos = user32.NewProc("SetWindowPos")
)

const (
	dwmwaUseImmersiveDarkMode = 20 // Windows 10 1809+
	swpNoMove                 = 0x0002
	swpNoSize                 = 0x0001
	swpNoZOrder               = 0x0004
	swpFrameChanged           = 0x0020 // 强制重绘非客户区（标题栏）
)

// ApplyWindowTheme 立即对指定窗口设置标题栏明暗。
func ApplyWindowTheme(w *application.WebviewWindow, theme string) {
	hwnd := w.NativeWindow()
	if hwnd == nil {
		return
	}
	dark := theme != "light"
	val := uintptr(0)
	if dark {
		val = 1
	}
	procDwmSetWindowAttribute.Call(
		uintptr(hwnd),
		uintptr(dwmwaUseImmersiveDarkMode),
		uintptr(unsafe.Pointer(&val)),
		unsafe.Sizeof(val),
	)
	procSetWindowPos.Call(
		uintptr(hwnd), 0, 0, 0, 0, 0,
		uintptr(swpNoMove|swpNoSize|swpNoZOrder|swpFrameChanged),
	)
}

func setTitleBarTheme(theme string) {
	for _, w := range application.Get().Window.GetAll() {
		ww, ok := w.(*application.WebviewWindow)
		if !ok {
			continue
		}
		ApplyWindowTheme(ww, theme)
	}
}
