//go:build !windows

package localterminal

import (
	"os"
	"path/filepath"
	"strings"
)

// platformDefaultShell 返回 Unix 默认 shell（$SHELL，兜底 /bin/sh）。
func platformDefaultShell() string {
	if v := os.Getenv("SHELL"); v != "" {
		return v
	}
	return "/bin/sh"
}

// resolveShell 原样返回（Unix 下 exec.Command 自行走 PATH 解析）。
func resolveShell(shell string) string {
	return shell
}

// platformShellArgs 返回 shell 启动参数：
//   - pwsh：-NoLogo -NoExit
//   - 其他（bash/zsh/sh 等）：-i 交互模式（真实 TTY 下可正常读取 rc 文件、
//     显示提示符、支持作业控制）
func platformShellArgs(shell string) []string {
	base := strings.ToLower(filepath.Base(shell))
	if strings.Contains(base, "pwsh") {
		return []string{"-NoLogo", "-NoExit"}
	}
	return []string{"-i"}
}

// platformHomeDir 返回用户主目录（shell 的起始目录）。
func platformHomeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
