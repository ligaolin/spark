//go:build !windows

package shellmenu

// Register is a no-op on non-Windows platforms.
func Register(exePath string) error { return nil }
