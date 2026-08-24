//go:build windows

package localterminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// platformDefaultShell 返回 Windows 默认 shell。
// 本地终端默认使用 PowerShell（优先 Windows PowerShell 5.x，其次 pwsh），
// 两者都不可用时退回 cmd.exe。
func platformDefaultShell() string {
	if p, err := exec.LookPath("powershell.exe"); err == nil {
		return p
	}
	if p, err := exec.LookPath("pwsh.exe"); err == nil {
		return p
	}
	if v := os.Getenv("COMSPEC"); v != "" {
		return v
	}
	return "cmd.exe"
}

// resolveShell 把裸命令名解析为绝对路径（go-pty 在 Windows 上不走 PATH）。
func resolveShell(shell string) string {
	if strings.ContainsAny(shell, `/\`) {
		return shell // 已含路径，原样使用
	}
	if p, err := exec.LookPath(shell); err == nil {
		return p
	}
	return shell
}

// platformShellArgs 按 shell 类型返回启动参数：
//   - cmd.exe：/Q 关闭启动横幅；/K 先执行 chcp 65001 切换到 UTF-8 代码页再保持交互
//   - PowerShell 5.x：-NoLogo -NoExit，并把控制台输出编码设为 UTF-8 + 切代码页
//   - pwsh（PowerShell 7+）：默认 UTF-8，只需 -NoLogo -NoExit
func platformShellArgs(shell string) []string {
	base := strings.ToLower(filepath.Base(shell))
	switch {
	case strings.Contains(base, "pwsh"):
		return []string{"-NoLogo", "-NoExit"}
	case strings.Contains(base, "powershell"):
		return []string{
			"-NoLogo", "-NoExit",
			"-Command", "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; chcp 65001 | Out-Null",
		}
	default: // cmd.exe
		return []string{"/Q", "/K", "chcp 65001>nul"}
	}
}

// platformHomeDir 返回用户主目录（shell 的起始目录）。
func platformHomeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
