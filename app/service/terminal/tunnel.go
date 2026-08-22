// SSH 端口转发 / 动态代理（SOCKS5）。
//
// 复用当前终端会话的 SSH 连接建立三类隧道，等价于：
//   - local  ：ssh -L <本机监听>：<远端目标>        （本地端口转发）
//   - remote ：ssh -R <远端监听>：<本机目标>        （远程端口转发）
//   - socks  ：ssh -D <本机监听>                    （动态 SOCKS5 代理）
//
// 隧道随会话关闭自动清理；监听默认绑定 127.0.0.1（纯端口时自动补前缀）。
package terminal

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	"changeme/app/service/types"

	xssh "golang.org/x/crypto/ssh"
)

// sshTunnel 描述一个活动隧道。listener 对 local/socks 是本机监听，对 remote
// 是经 client.Listen 在远端建立的监听；两种情况下 accept 循环结构一致。
type sshTunnel struct {
	id        string
	sessionID string
	kind      string
	bindAddr  string
	target    string
	client    *xssh.Client
	listener  net.Listener

	socksUser string
	socksPass string

	mu     sync.Mutex
	closed bool
	errMsg string
}

func (tn *sshTunnel) info() types.Tunnel {
	tn.mu.Lock()
	defer tn.mu.Unlock()
	status := "running"
	if tn.errMsg != "" {
		status = "error"
	} else if tn.closed {
		status = "stopped"
	}
	return types.Tunnel{
		ID:       tn.id,
		Kind:     tn.kind,
		BindAddr: tn.bindAddr,
		Target:   tn.target,
		Status:   status,
		Error:    tn.errMsg,
	}
}

func (tn *sshTunnel) setErr(msg string) {
	tn.mu.Lock()
	tn.errMsg = msg
	tn.mu.Unlock()
}

func (tn *sshTunnel) close() {
	tn.mu.Lock()
	if tn.closed {
		tn.mu.Unlock()
		return
	}
	tn.closed = true
	tn.mu.Unlock()
	_ = tn.listener.Close()
}

// Tunnels returns all tunnels of a session.
func (t *TerminalService) Tunnels(sessionID string) []types.Tunnel {
	t.tunnelMu.Lock()
	defer t.tunnelMu.Unlock()
	out := make([]types.Tunnel, 0)
	for _, tn := range t.tunnels {
		if tn.sessionID == sessionID {
			out = append(out, tn.info())
		}
	}
	return out
}

// OpenTunnel establishes a tunnel on the given session.
// kind ∈ {local, remote, socks}；bindAddr 为空或纯端口时默认 127.0.0.1（随机端口）。
// socksUser/socksPass 仅对 socks 生效：非空时为 SOCKS5 启用用户名/密码认证。
func (t *TerminalService) OpenTunnel(id, kind, bindAddr, target, socksUser, socksPass string) (types.Tunnel, error) {
	s := t.get(id)
	if s == nil {
		return types.Tunnel{}, fmt.Errorf("会话 %q 不存在", id)
	}

	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "local":
		if strings.TrimSpace(target) == "" {
			return types.Tunnel{}, errors.New("本地转发需要填写「远程目标」地址（host:port）")
		}
	case "remote":
		if strings.TrimSpace(target) == "" {
			return types.Tunnel{}, errors.New("远程转发需要填写「本机目标」地址（host:port）")
		}
	case "socks":
		target = ""
		socksUser = strings.TrimSpace(socksUser)
	default:
		return types.Tunnel{}, errors.New("不支持的转发类型（仅支持 local / remote / socks）")
	}

	addr, err := normalizeBindAddr(bindAddr)
	if err != nil {
		return types.Tunnel{}, err
	}

	var ln net.Listener
	if kind == "remote" {
		ln, err = s.client.Listen("tcp", addr)
	} else {
		ln, err = net.Listen("tcp", addr)
	}
	if err != nil {
		return types.Tunnel{}, fmt.Errorf("创建监听失败: %w", err)
	}

	t.ensure()
	t.tunnelMu.Lock()
	tn := &sshTunnel{
		id:        types.NewID(),
		sessionID: id,
		kind:      kind,
		bindAddr:  ln.Addr().String(),
		target:    strings.TrimSpace(target),
		client:    s.client,
		listener:  ln,
		socksUser: socksUser,
		socksPass: socksPass,
	}
	t.tunnels[tn.id] = tn
	t.tunnelMu.Unlock()

	go tn.run()
	return tn.info(), nil
}

// CloseTunnel closes and releases a tunnel by id.
func (t *TerminalService) CloseTunnel(tunnelID string) error {
	t.tunnelMu.Lock()
	tn := t.tunnels[tunnelID]
	delete(t.tunnels, tunnelID)
	t.tunnelMu.Unlock()
	if tn == nil {
		return nil
	}
	tn.close()
	return nil
}

// closeTunnelsFor closes every tunnel belonging to a session (called when the
// session is removed).
func (t *TerminalService) closeTunnelsFor(sessionID string) {
	t.tunnelMu.Lock()
	var toClose []*sshTunnel
	for id, tn := range t.tunnels {
		if tn.sessionID == sessionID {
			toClose = append(toClose, tn)
			delete(t.tunnels, id)
		}
	}
	t.tunnelMu.Unlock()
	for _, tn := range toClose {
		tn.close()
	}
}

// normalizeBindAddr 规范化监听地址：空/纯端口补 127.0.0.1，host:port 校验合法性。
func normalizeBindAddr(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "127.0.0.1:0", nil
	}
	if p, err := strconv.Atoi(addr); err == nil {
		if p < 0 || p > 65535 {
			return "", errors.New("端口超出范围（0-65535）")
		}
		return "127.0.0.1:" + addr, nil
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("监听地址格式应为 host:port（当前 %q）", addr)
	}
	if strings.TrimSpace(host) == "" {
		host = "127.0.0.1"
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 0 || p > 65535 {
		return "", fmt.Errorf("端口无效：%q", port)
	}
	return net.JoinHostPort(host, port), nil
}

// run 接受连接并转发，listener 关闭时退出。
func (tn *sshTunnel) run() {
	for {
		conn, err := tn.listener.Accept()
		if err != nil {
			return
		}
		go tn.handle(conn)
	}
}

func (tn *sshTunnel) handle(conn net.Conn) {
	defer conn.Close()

	switch tn.kind {
	case "socks":
		tn.handleSocks(conn)
	case "remote":
		// 远端收到的连接 → 本机目标
		local, err := net.Dial("tcp", tn.target)
		if err != nil {
			return
		}
		defer local.Close()
		relay(conn, local)
	default: // local
		remote, err := tn.client.Dial("tcp", tn.target)
		if err != nil {
			return
		}
		defer remote.Close()
		relay(conn, remote)
	}
}

// handleSocks 实现 SOCKS5 服务端（RFC 1928 CONNECT，可选 RFC 1929 认证）。
func (tn *sshTunnel) handleSocks(conn net.Conn) {
	br := bufio.NewReader(conn)
	target, err := socks5Connect(br, conn, tn.socksUser, tn.socksPass)
	if err != nil {
		return
	}
	remote, err := tn.client.Dial("tcp", target)
	if err != nil {
		return
	}
	defer remote.Close()

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(remote, br); done <- struct{}{} }() // 客户端(可能含缓冲) → 远端
	go func() { _, _ = io.Copy(conn, remote); done <- struct{}{} }()
	<-done
}

// relay 双向拷贝两个连接，任一方向结束即返回（由调用方负责关闭）。
func relay(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	<-done
}

// socks5Connect 完成 SOCKS5 握手并返回要连接的目标 host:port。
// user 非空时启用 RFC 1929 用户名/密码认证。
func socks5Connect(br *bufio.Reader, w io.Writer, user, pass string) (string, error) {
	// 1) 问候：VER + NMETHODS + METHODS
	var g [2]byte
	if _, err := io.ReadFull(br, g[:]); err != nil {
		return "", err
	}
	if g[0] != 0x05 {
		return "", errors.New("非 SOCKS5 协议")
	}
	if g[1] > 0 {
		if _, err := io.ReadFull(br, make([]byte, int(g[1]))); err != nil {
			return "", err
		}
	}
	if user != "" {
		if _, err := w.Write([]byte{0x05, 0x02}); err != nil { // 选择用户名/密码认证
			return "", err
		}
		if err := socks5Auth(br, w, user, pass); err != nil {
			return "", err
		}
	} else {
		if _, err := w.Write([]byte{0x05, 0x00}); err != nil { // 无需认证
			return "", err
		}
	}

	// 2) 请求：VER + CMD + RSV + ATYP + DST.ADDR + DST.PORT
	var h [4]byte
	if _, err := io.ReadFull(br, h[:]); err != nil {
		return "", err
	}
	if h[0] != 0x05 || h[1] != 0x01 { // 仅支持 CONNECT
		_, _ = w.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return "", errors.New("仅支持 CONNECT 命令")
	}

	var host string
	switch h[3] {
	case 0x01: // IPv4
		var b [4]byte
		if _, err := io.ReadFull(br, b[:]); err != nil {
			return "", err
		}
		host = net.IP(b[:]).String()
	case 0x03: // 域名
		var lb [1]byte
		if _, err := io.ReadFull(br, lb[:]); err != nil {
			return "", err
		}
		db := make([]byte, int(lb[0]))
		if _, err := io.ReadFull(br, db); err != nil {
			return "", err
		}
		host = string(db)
	case 0x04: // IPv6
		var b [16]byte
		if _, err := io.ReadFull(br, b[:]); err != nil {
			return "", err
		}
		host = net.IP(b[:]).String()
	default:
		_, _ = w.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return "", errors.New("不支持的地址类型")
	}

	var p [2]byte
	if _, err := io.ReadFull(br, p[:]); err != nil {
		return "", err
	}
	port := int(p[0])<<8 | int(p[1])

	// 3) 回复成功：绑定地址回 0.0.0.0:0
	if _, err := w.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// socks5Auth 完成 RFC 1929 用户名/密码认证。
func socks5Auth(br *bufio.Reader, w io.Writer, user, pass string) error {
	var h [2]byte
	if _, err := io.ReadFull(br, h[:]); err != nil {
		return err
	}
	if h[0] != 0x01 {
		return errors.New("SOCKS5 认证版本错误")
	}
	ub := make([]byte, int(h[1]))
	if _, err := io.ReadFull(br, ub); err != nil {
		return err
	}
	var pl [1]byte
	if _, err := io.ReadFull(br, pl[:]); err != nil {
		return err
	}
	pb := make([]byte, int(pl[0]))
	if _, err := io.ReadFull(br, pb); err != nil {
		return err
	}
	if string(ub) == user && string(pb) == pass {
		_, err := w.Write([]byte{0x01, 0x00})
		return err
	}
	_, _ = w.Write([]byte{0x01, 0x01})
	return errors.New("SOCKS5 认证失败（用户名或密码错误）")
}
