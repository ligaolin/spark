// Package localterminal implements local (on-machine) interactive shell
// sessions, exposed to the frontend as a Wails service.
//
// 与 TerminalService（SSH 终端）不同，这里启动的是本机 shell（Windows 为
// cmd.exe，Unix 为 $SHELL），通过真实伪终端（Windows ConPTY / Unix openpty）
// 与前端 xterm 双向交互，支持同时打开多个会话。
package localterminal

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"changeme/app/service/types"

	"github.com/aymanbagabas/go-pty"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// LocalTerminalService manages local shell sessions.
// Each session streams its output to the frontend through the
// "localTerminal:output" event and announces termination with
// "localTerminal:exit".
type LocalTerminalService struct {
	mu       sync.Mutex
	sessions map[string]*localSession
}

type localSession struct {
	id    string
	pty   pty.Pty
	cmd   *pty.Cmd

	mu     sync.Mutex
	closed bool
}

// ServiceName implements application.ServiceName.
func (t *LocalTerminalService) ServiceName() string { return "LocalTerminalService" }

// Create spawns a local shell attached to a pseudo-terminal and returns a
// new session id. shell 为空时使用平台默认 shell；可传 "powershell" 等覆盖。
// rows / cols 为终端初始尺寸。
func (t *LocalTerminalService) Create(shell string, rows, cols int) (string, error) {
	if strings.TrimSpace(shell) == "" {
		shell = platformDefaultShell()
	}
	// Windows 下 go-pty 的 CreateProcess 对裸命令名按启动目录解析（不走 PATH），
	// 先统一解析成绝对路径再启动
	shell = resolveShell(shell)
	if rows <= 0 {
		rows = 24
	}
	if cols <= 0 {
		cols = 80
	}

	p, err := pty.New()
	if err != nil {
		return "", fmt.Errorf("创建本地终端失败: %w", err)
	}
	if err := p.Resize(cols, rows); err != nil {
		p.Close()
		return "", fmt.Errorf("设置终端尺寸失败: %w", err)
	}

	cmd := p.Command(shell, platformShellArgs(shell)...)
	cmd.Dir = platformHomeDir()
	if err := cmd.Start(); err != nil {
		p.Close()
		return "", fmt.Errorf("启动本地 shell 失败: %w", err)
	}

	id := types.NewID()
	s := &localSession{id: id, pty: p, cmd: cmd}
	t.ensure()
	t.mu.Lock()
	t.sessions[id] = s
	t.mu.Unlock()

	go t.readLoop(s)
	go t.watch(s)
	return id, nil
}

// Write forwards terminal input (keystrokes) to the local shell.
func (t *LocalTerminalService) Write(id, data string) error {
	s := t.get(id)
	if s == nil {
		return fmt.Errorf("会话 %q 不存在", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("会话已关闭")
	}
	return writeAll(s.pty, []byte(data))
}

// Resize adjusts the local terminal size (rows x cols).
func (t *LocalTerminalService) Resize(id string, rows, cols int) error {
	s := t.get(id)
	if s == nil {
		return fmt.Errorf("会话 %q 不存在", id)
	}
	if rows <= 0 || cols <= 0 {
		return errors.New("行数和列数必须为正数")
	}
	return s.pty.Resize(cols, rows)
}

// Disconnect terminates the local shell session.
func (t *LocalTerminalService) Disconnect(id string) error {
	t.removeSession(id)
	return nil
}

// IsRunning reports whether the session is still alive.
func (t *LocalTerminalService) IsRunning(id string) bool {
	s := t.get(id)
	return s != nil && !s.isClosed()
}

// readLoop 读取伪终端输出并转发给前端。
func (t *LocalTerminalService) readLoop(s *localSession) {
	buf := make([]byte, 8192)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			application.Get().Event.Emit("localTerminal:output", types.TerminalOutput{
				SessionID: s.id,
				Data:      string(buf[:n]),
			})
		}
		if err != nil {
			return
		}
	}
}

func (t *LocalTerminalService) watch(s *localSession) {
	code := 0
	msg := ""
	err := s.cmd.Wait()
	if s.cmd.ProcessState != nil {
		code = s.cmd.ProcessState.ExitCode()
	}
	if err != nil {
		if code == 0 {
			code = -1
		}
		msg = err.Error()
	}
	application.Get().Event.Emit("localTerminal:exit", types.TerminalExit{
		SessionID: s.id,
		Code:      code,
		Error:     msg,
	})
	t.removeSession(s.id)
}

func (t *LocalTerminalService) ensure() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sessions == nil {
		t.sessions = make(map[string]*localSession)
	}
}

func (t *LocalTerminalService) get(id string) *localSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessions[id]
}

func (t *LocalTerminalService) removeSession(id string) {
	t.mu.Lock()
	s, ok := t.sessions[id]
	if ok {
		delete(t.sessions, id)
	}
	t.mu.Unlock()
	if ok {
		s.close()
	}
}

func (s *localSession) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *localSession) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	// 先终止进程，再关闭伪终端（ConPTY 关闭会连带结束挂接的进程）
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	if s.pty != nil {
		_ = s.pty.Close()
	}
}

// writeAll 把数据完整写入，处理部分写入的情况。
func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}
