//go:build windows

package shellmenu

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// Register writes the Explorer context-menu entries into HKCU (no admin rights
// required). Registering against the current executable path on every startup
// keeps the entries pointing at the right binary across updates.
func Register(exePath string) error {
	if exePath == "" {
		return fmt.Errorf("shellmenu: empty exe path")
	}

	// 挂载位置：文件、文件夹、文件夹空白处、驱动器。
	targets := []struct {
		class string
		arg   string // %1 = 文件/文件夹；%V = 当前文件夹
	}{
		{`*\shell`, `%1`},
		{`Directory\shell`, `%1`},
		{`Directory\Background\shell`, `%V`},
		{`Drive\shell`, `%1`},
	}

	menus := []struct {
		name string
		flag string
	}{
		{"Spark终端打开", "--terminal"},
		{"Spark编辑器打开", "--editor"},
	}

	for _, t := range targets {
		for _, m := range menus {
			shellKey := fmt.Sprintf(`%s\%s`, t.class, m.name)
			if err := writeShellKey(shellKey, m.name, exePath); err != nil {
				return err
			}
			if err := writeCommandKey(shellKey+`\command`, exePath, m.flag, t.arg); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeShellKey 设置菜单项的显示名称与图标。
func writeShellKey(keyPath, name, exePath string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\`+keyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	// 默认值即菜单显示名（子键名已是中文，这里显式再设一次，兼容不同系统）。
	if err := k.SetStringValue("", name); err != nil {
		return err
	}
	if err := k.SetStringValue("Icon", fmt.Sprintf(`"%s",0`, exePath)); err != nil {
		return err
	}
	return nil
}

// writeCommandKey 写入命令： "exe" --terminal "%1"。
func writeCommandKey(keyPath, exePath, flag, arg string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\`+keyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	cmd := fmt.Sprintf(`"%s" %s "%s"`, exePath, flag, arg)
	return k.SetStringValue("", cmd)
}
