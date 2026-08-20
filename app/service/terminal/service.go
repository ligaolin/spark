// Package terminal implements interactive SSH terminal sessions
// exposed to the frontend as a Wails service.
package terminal

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"changeme/app/service/settings"
	"changeme/app/service/sshlib"
	"changeme/app/service/types"

	"github.com/wailsapp/wails/v3/pkg/application"
	xssh "golang.org/x/crypto/ssh"
)

// TerminalService manages interactive SSH terminal sessions.
// Each session streams its output to the frontend through the
// "terminal:output" event and announces termination with "terminal:exit".
type TerminalService struct {
	mu       sync.Mutex
	sessions map[string]*sshSession
}

type sshSession struct {
	id     string
	client *xssh.Client
	sess   *xssh.Session
	stdin  io.WriteCloser

	stopKA chan struct{}
	kaOnce sync.Once

	mu     sync.Mutex
	closed bool
}

// ServiceName implements application.ServiceName.
func (t *TerminalService) ServiceName() string { return "TerminalService" }

// Connect establishes an SSH connection and starts an interactive shell.
// It returns the new session id.
func (t *TerminalService) Connect(opts types.ConnectOptions) (string, error) {
	if strings.TrimSpace(opts.Host) == "" {
		return "", errors.New("主机地址不能为空")
	}
	if opts.Port <= 0 || opts.Port > 65535 {
		opts.Port = 22
	}

	config, err := sshlib.BuildClientConfig(opts)
	if err != nil {
		return "", err
	}

	client, err := xssh.Dial("tcp", net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port)), config)
	if err != nil {
		if werr := sshlib.AsHostKeyError(err); werr != nil {
			return "", werr
		}
		return "", fmt.Errorf("SSH 连接失败: %w", err)
	}

	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return "", fmt.Errorf("创建 SSH 会话失败: %w", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return "", fmt.Errorf("获取标准输入失败: %w", err)
	}

	id := opts.SessionID
	if id == "" {
		id = types.NewID()
	}

	s := &sshSession{id: id, client: client, sess: sess, stdin: stdin, stopKA: make(chan struct{})}

	// 保活：定期发送 SSH keepalive，连接死亡（网络中断/空闲超时被断开）时
	// 关闭客户端，触发下方 watch 的 terminal:exit 通知前端（间隔可在设置中调整）
	if ka := settings.GetInt("keepalive.interval", 20); ka > 0 {
		go sshlib.KeepAliveLoop(s.stopKA, func() error {
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			return err
		}, time.Duration(ka)*time.Second, 10*time.Second, 3, func() { s.close() })
	}

	// Merge stdout and stderr into one stream forwarded to the frontend.
	out := &outputWriter{svc: t, id: id}
	sess.Stdout = out
	sess.Stderr = out

	rows, cols := opts.Rows, opts.Cols
	if rows <= 0 {
		rows = 24
	}
	if cols <= 0 {
		cols = 80
	}
	modes := xssh.TerminalModes{
		xssh.ECHO:          1,
		xssh.TTY_OP_ISPEED: 14400,
		xssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		sess.Close()
		client.Close()
		return "", fmt.Errorf("申请 PTY 失败: %w", err)
	}

	if opts.Shell != "" {
		err = sess.Start(opts.Shell)
	} else {
		err = sess.Shell()
	}
	if err != nil {
		sess.Close()
		client.Close()
		return "", fmt.Errorf("启动远程 shell 失败: %w", err)
	}

	t.ensure()
	t.mu.Lock()
	t.sessions[id] = s
	t.mu.Unlock()

	go t.watch(s)
	return id, nil
}

// Write forwards terminal input (keystrokes) to the remote session.
func (t *TerminalService) Write(id, data string) error {
	s := t.get(id)
	if s == nil {
		return fmt.Errorf("会话 %q 不存在", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("会话已关闭")
	}
	_, err := s.stdin.Write([]byte(data))
	return err
}

// Resize adjusts the remote PTY size (rows x cols).
func (t *TerminalService) Resize(id string, rows, cols int) error {
	s := t.get(id)
	if s == nil {
		return fmt.Errorf("会话 %q 不存在", id)
	}
	if rows <= 0 || cols <= 0 {
		return errors.New("行数和列数必须为正数")
	}
	return s.sess.WindowChange(rows, cols)
}

// Disconnect closes the session and its underlying SSH connection.
func (t *TerminalService) Disconnect(id string) error {
	t.removeSession(id)
	return nil
}

// IsConnected reports whether the session is still alive.
func (t *TerminalService) IsConnected(id string) bool {
	s := t.get(id)
	return s != nil && !s.isClosed()
}

// RunCommand executes a command on the session's SSH connection (separate
// channel, independent of the interactive PTY) and returns its combined
// output. Commands that exit non-zero still return their output.
func (t *TerminalService) RunCommand(id, command string) (string, error) {
	out, _, err := t.runCommandWithExit(id, command)
	return out, err
}

// runCommandWithExit runs a command and also reports its exit code.
// A non-zero exit code still returns the output without an error.
func (t *TerminalService) runCommandWithExit(id, command string) (string, int, error) {
	return t.runCommandWithTimeout(id, command, 120*time.Second)
}

// runCommandWithTimeout runs a command with a custom timeout.
func (t *TerminalService) runCommandWithTimeout(id, command string, timeout time.Duration) (string, int, error) {
	s := t.get(id)
	if s == nil {
		return "", -1, fmt.Errorf("会话 %q 不存在", id)
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return "", -1, errors.New("会话已关闭")
	}

	sess, err := s.client.NewSession()
	if err != nil {
		return "", -1, fmt.Errorf("创建命令会话失败: %w", err)
	}
	defer sess.Close()

	var buf bytes.Buffer
	sess.Stdout = &buf
	sess.Stderr = &buf

	if err := sess.Start(command); err != nil {
		return "", -1, fmt.Errorf("启动命令失败: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			var exitErr *xssh.ExitError
			if errors.As(err, &exitErr) {
				return buf.String(), exitErr.ExitStatus(), nil
			}
			return "", -1, fmt.Errorf("执行命令失败: %w", err)
		}
		return buf.String(), 0, nil
	case <-time.After(timeout):
		_ = sess.Signal(xssh.SIGKILL)
		return buf.String(), -1, fmt.Errorf("命令执行超时（%d 秒），已终止", int(timeout.Seconds()))
	}
}

// processListScript 列出远程进程（前 200 个，前端按 CPU 排序）
const processListScript = "ps -eo pid,ppid,user,%cpu,%mem,rss,stat,args 2>/dev/null | head -200"

const processListScriptFallback = "ps aux 2>/dev/null | head -200"

// ProcessList returns the remote process table (Linux ps).
func (t *TerminalService) ProcessList(id string) ([]types.ProcessInfo, error) {
	out, err := t.RunCommand(id, processListScript)
	if err != nil {
		return nil, err
	}
	list := parsePsEO(out)
	if len(list) == 0 {
		// 兼容 busybox 等不支持 -eo 的环境
		if out2, err2 := t.RunCommand(id, processListScriptFallback); err2 == nil {
			list = parsePsAux(out2)
		}
	}
	return list, nil
}

// KillProcess force-kills a remote process by PID.
func (t *TerminalService) KillProcess(id string, pid int) error {
	if pid <= 0 {
		return errors.New("无效的进程号")
	}
	out, code, err := t.runCommandWithExit(id, fmt.Sprintf("kill -9 %d", pid))
	if err != nil {
		return err
	}
	if code != 0 {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = fmt.Sprintf("kill 返回退出码 %d（可能权限不足）", code)
		}
		return errors.New(msg)
	}
	return nil
}

func parsePsEO(out string) []types.ProcessInfo {
	var list []types.ProcessInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "PID") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		cpu, _ := strconv.ParseFloat(fields[3], 64)
		mem, _ := strconv.ParseFloat(fields[4], 64)
		rss, _ := strconv.ParseInt(fields[5], 10, 64)
		list = append(list, types.ProcessInfo{
			PID:     pid,
			PPID:    ppid,
			User:    fields[2],
			CPU:     cpu,
			Mem:     mem,
			RSS:     rss,
			Stat:    fields[6],
			Command: strings.Join(fields[7:], " "),
		})
	}
	return list
}

func parsePsAux(out string) []types.ProcessInfo {
	var list []types.ProcessInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "USER") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		cpu, _ := strconv.ParseFloat(fields[2], 64)
		mem, _ := strconv.ParseFloat(fields[3], 64)
		rss, _ := strconv.ParseInt(fields[5], 10, 64)
		list = append(list, types.ProcessInfo{
			PID:     pid,
			User:    fields[0],
			CPU:     cpu,
			Mem:     mem,
			RSS:     rss,
			Stat:    fields[7],
			Command: strings.Join(fields[10:], " "),
		})
	}
	return list
}

// serverInfoScript 采集服务器基础信息。刻意只使用 cat / uname / nproc / df
// 这类最基础的命令，把 /proc 原始内容原样输出，解析全部在 Go 侧完成，
// 避免依赖 grep/awk/cut 等文本工具（最小化容器里可能缺失导致整段失败）。
//
// 关键稳定性措施：
// 1. df 使用 -l 只显示本地文件系统，避免 NFS/CIFS 等网络挂载卡死
// 2. df 失败时回退到无 -l 的版本（兼容 busybox）
// 3. 每个可能阻塞的 I/O 命令都用纯 shell 方式设置 5 秒超时
const serverInfoScript = `
echo "@@HOSTNAME@@"
cat /proc/sys/kernel/hostname 2>/dev/null
echo "@@OSRELEASE@@"
cat /etc/os-release 2>/dev/null
echo "@@KERNEL@@"
uname -r 2>/dev/null
echo "@@ARCH@@"
uname -m 2>/dev/null
echo "@@UPTIME@@"
cat /proc/uptime 2>/dev/null
echo "@@CPUINFO@@"
cat /proc/cpuinfo 2>/dev/null
echo "@@NPROC@@"
nproc 2>/dev/null
echo "@@LOAD@@"
cat /proc/loadavg 2>/dev/null
echo "@@MEMINFO@@"
cat /proc/meminfo 2>/dev/null
echo "@@DISKS@@"
# 优先本地文件系统（防 NFS 卡死）；busybox 不支持 -l 时回退
(df -kP -l 2>/dev/null || df -kP 2>/dev/null)
`

// shQuote 用单引号包裹 shell 参数（内部单引号做标准转义），
// 保证脚本无论服务器登录 shell 是 sh/bash/csh/fish 都能以 POSIX sh 语义执行。
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ServerInfo queries basic server information (OS, CPU, memory, disks).
// 内置重试机制：首次失败后会等待 800ms 重试一次，提高不稳定网络下的成功率。
func (t *TerminalService) ServerInfo(id string) (*types.ServerInfo, error) {
	info, err := t.serverInfoOnce(id)
	if err != nil || isServerInfoEmpty(info) {
		// 首次失败或拿到空数据，短暂等待后重试一次
		time.Sleep(800 * time.Millisecond)
		info2, err2 := t.serverInfoOnce(id)
		if err2 == nil && !isServerInfoEmpty(info2) {
			return info2, nil
		}
		// 重试也失败，如果首次有部分数据就返回首次的
		if info != nil && !isServerInfoEmpty(info) {
			return info, nil
		}
		if err2 != nil {
			return info2, err2
		}
	}
	return info, err
}

// serverInfoOnce 执行一次服务器信息采集（内部方法）。
func (t *TerminalService) serverInfoOnce(id string) (*types.ServerInfo, error) {
	out, _, err := t.runCommandWithTimeout(id, "sh -c "+shQuote(serverInfoScript), 20*time.Second)
	// 即使脚本超时或部分失败，也尝试解析已获取到的输出
	info := parseServerInfo(out)
	if err != nil && isServerInfoEmpty(info) {
		return nil, err
	}

	// 整段脚本异常时，逐条兜底获取最基本信息
	if info.Hostname == "" {
		info.Hostname = firstNonEmpty(t.runCommandQuiet(id, "hostname 2>/dev/null"))
	}
	if info.OS == "" {
		info.OS = firstNonEmpty(t.runCommandQuiet(id, "uname -s 2>/dev/null"))
	}
	if info.Kernel == "" {
		info.Kernel = firstNonEmpty(t.runCommandQuiet(id, "uname -r 2>/dev/null"))
	}
	return info, nil
}

// isServerInfoEmpty 判断服务器信息是否完全为空（所有关键字段都缺失）。
func isServerInfoEmpty(info *types.ServerInfo) bool {
	if info == nil {
		return true
	}
	return info.Hostname == "" && info.CPUCores == 0 && info.MemoryTotal == 0 && len(info.Disks) == 0
}

// runCommandQuiet 执行命令并忽略错误（只用于信息兜底）。
func (t *TerminalService) runCommandQuiet(id, command string) string {
	out, _, err := t.runCommandWithTimeout(id, command, 10*time.Second)
	if err != nil {
		return ""
	}
	return out
}

func firstNonEmpty(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func parseServerInfo(out string) *types.ServerInfo {
	info := &types.ServerInfo{}
	section := ""
	var cpuinfo, meminfo, diskLines []string
	nprocVal := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "@@") && strings.HasSuffix(line, "@@") {
			section = strings.Trim(line, "@")
			switch section {
			case "CPUINFO":
				cpuinfo = cpuinfo[:0]
			case "MEMINFO":
				meminfo = meminfo[:0]
			case "DISKS":
				diskLines = diskLines[:0]
			}
			continue
		}
		if line == "" {
			continue
		}
		switch section {
		case "HOSTNAME":
			if info.Hostname == "" {
				info.Hostname = line
			}
		case "OSRELEASE":
			if i := strings.Index(line, "PRETTY_NAME="); i >= 0 && info.OS == "" {
				info.OS = strings.Trim(strings.TrimPrefix(line[i+len("PRETTY_NAME="):], `"`), `"`)
			}
		case "KERNEL":
			if info.Kernel == "" {
				info.Kernel = line
			}
		case "ARCH":
			if info.Arch == "" {
				info.Arch = line
			}
		case "UPTIME":
			if info.Uptime == "" {
				fields := strings.Fields(line)
				if len(fields) >= 1 {
					// /proc/uptime 是 "秒 空闲秒" 的浮点数格式
					if sec, err := strconv.ParseFloat(fields[0], 64); err == nil {
						info.Uptime = humanUptime(int64(sec))
					}
				}
			}
		case "CPUINFO":
			cpuinfo = append(cpuinfo, line)
		case "NPROC":
			if nprocVal == "" {
				nprocVal = line
			}
		case "LOAD":
			if info.Load1 == 0 && info.Load5 == 0 && info.Load15 == 0 {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					info.Load1, _ = strconv.ParseFloat(fields[0], 64)
					info.Load5, _ = strconv.ParseFloat(fields[1], 64)
					info.Load15, _ = strconv.ParseFloat(fields[2], 64)
				}
			}
		case "MEMINFO":
			meminfo = append(meminfo, line)
		case "DISKS":
			if !strings.HasPrefix(line, "Filesystem") {
				diskLines = append(diskLines, line)
			}
		}
	}

	// CPU：/proc/cpuinfo 原始内容在 Go 里解析（兼容 Intel "model name" 与 ARM "Processor"/"Hardware"）
	info.CPUModel, info.CPUCores = parseCPUInfo(cpuinfo)
	if info.CPUCores == 0 {
		info.CPUCores, _ = strconv.Atoi(nprocVal)
	}

	// 内存：/proc/meminfo 在 Go 里解析（MemAvailable 缺失时退回 MemFree）
	info.MemoryTotal, info.MemoryAvail = parseMemInfo(meminfo)
	info.MemoryUsed = info.MemoryTotal - info.MemoryAvail
	if info.MemoryUsed < 0 {
		info.MemoryUsed = 0
	}

	// 磁盘：df -kP 输出
	for _, l := range diskLines {
		fields := strings.Fields(l)
		if len(fields) < 5 {
			continue
		}
		fs, size, used, avail := fields[0], fields[1], fields[2], fields[3]
		if isPseudoFs(fs) {
			continue
		}
		pct, _ := strconv.Atoi(strings.TrimSuffix(fields[4], "%"))
		// 挂载点可能含空格，取第 5 列之后的内容
		mount := strings.Join(fields[5:], " ")
		info.Disks = append(info.Disks, types.DiskInfo{
			Mount:      mount,
			Size:       kbToBytes(size),
			Used:       kbToBytes(used),
			Avail:      kbToBytes(avail),
			UsePercent: pct,
		})
	}
	// 只有完全没拿到任何信息时才提示"非 Linux"；部分信息缺失时静默展示已有的
	if info.Hostname == "" && info.CPUCores == 0 && info.MemoryTotal == 0 && len(info.Disks) == 0 {
		info.Error = "未能获取系统信息（可能不是 Linux 服务器）"
	}
	return info
}

// parseCPUInfo 从 /proc/cpuinfo 原始内容解析 CPU 型号与核心数。
func parseCPUInfo(lines []string) (model string, cores int) {
	for _, line := range lines {
		if strings.HasPrefix(line, "processor") {
			cores++
		}
		if model != "" {
			continue
		}
		if i := strings.Index(line, ":"); i >= 0 {
			key := strings.TrimSpace(line[:i])
			switch key {
			case "model name", "Processor", "Hardware":
				model = strings.TrimSpace(line[i+1:])
			}
		}
	}
	return model, cores
}

// parseMemInfo 从 /proc/meminfo 原始内容解析内存总量与可用量（bytes）。
func parseMemInfo(lines []string) (total, avail int64) {
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		val, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			total = val * 1024
		case "MemAvailable":
			avail = val * 1024
		case "MemFree":
			if avail == 0 {
				avail = val * 1024
			}
		}
	}
	return total, avail
}

func isPseudoFs(fs string) bool {
	switch fs {
	case "tmpfs", "overlay", "devtmpfs", "udev", "none", "proc", "sysfs", "cgroup", "cgroup2", "mqueue", "devpts", "securityfs", "debugfs", "pstore", "bpf", "hugetlbfs", "tracefs", "configfs", "fusectl", "binfmt_misc", "autofs", "efivarfs":
		return true
	}
	return strings.HasPrefix(fs, "shm")
}

func kbToBytes(v string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return 0
	}
	return n * 1024
}

func humanUptime(sec int64) string {
	if sec <= 0 {
		return ""
	}
	days := sec / 86400
	hours := (sec % 86400) / 3600
	mins := (sec % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("%d 天 %d 小时 %d 分", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%d 小时 %d 分", hours, mins)
	}
	return fmt.Sprintf("%d 分", mins)
}

func (t *TerminalService) watch(s *sshSession) {
	code := 0
	msg := ""
	if err := s.sess.Wait(); err != nil {
		var exitErr *xssh.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitStatus()
		} else {
			code = -1
		}
		msg = err.Error()
	}
	application.Get().Event.Emit("terminal:exit", types.TerminalExit{
		SessionID: s.id,
		Code:      code,
		Error:     msg,
	})
	t.removeSession(s.id)
}

// outputWriter forwards SSH output chunks to the frontend.
type outputWriter struct {
	svc *TerminalService
	id  string
}

func (w *outputWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	application.Get().Event.Emit("terminal:output", types.TerminalOutput{
		SessionID: w.id,
		Data:      string(p),
	})
	return len(p), nil
}

func (t *TerminalService) ensure() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sessions == nil {
		t.sessions = make(map[string]*sshSession)
	}
}

func (t *TerminalService) get(id string) *sshSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessions[id]
}

func (t *TerminalService) removeSession(id string) {
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

func (s *sshSession) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *sshSession) close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	s.kaOnce.Do(func() { close(s.stopKA) })
	return s.client.Close()
}
