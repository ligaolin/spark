//go:build !windows

package settings

import "github.com/wailsapp/wails/v3/pkg/application"

// ApplyWindowTheme no-op on non-Windows platforms.
func ApplyWindowTheme(w *application.WebviewWindow, theme string) {}

func setTitleBarTheme(theme string) {}
