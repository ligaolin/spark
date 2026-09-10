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
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"changeme/app/service/types"

	xssh "golang.org/x/crypto/ssh"
)

// sshTunnel 描述一个活动隧道。listener 对 local/socks 是本机监听，对 remote
// 是经 client.Listen 在远端建立的监听；两种情况下 accept 循环结构一致。
type sshTunnel struct {
	id        string
	sessionID string
	seq       uint64 // 创建序号，仅用于列表稳定排序
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
	conns  map[net.Conn]struct{} // 活动连接，关闭隧道时一并断开
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

// setErr 记录最近一次转发失败的原因（隧道本身仍在监听，因此不改状态）。
func (tn *sshTunnel) setErr(msg string) {
	tn.mu.Lock()
	tn.errMsg = msg
	tn.mu.Unlock()
}

// clearErr 在某次转发成功后清空错误提示。
func (tn *sshTunnel) clearErr() {
	tn.mu.Lock()
	tn.errMsg = ""
	tn.mu.Unlock()
}

// track 记录一个活动连接，已关闭时返回 false。
func (tn *sshTunnel) track(c net.Conn) bool {
	tn.mu.Lock()
	defer tn.mu.Unlock()
	if tn.closed {
		return false
	}
	if tn.conns == nil {
		tn.conns = map[net.Conn]struct{}{}
	}
	tn.conns[c] = struct{}{}
	return true
}

func (tn *sshTunnel) untrack(c net.Conn) {
	tn.mu.Lock()
	delete(tn.conns, c)
	tn.mu.Unlock()
}

// close 关闭监听与所有活动连接（否则关掉隧道后已建立的连接仍会继续转发）。
func (tn *sshTunnel) close() {
	tn.mu.Lock()
	if tn.closed {
		tn.mu.Unlock()
		return
	}
	tn.closed = true
	conns := make([]net.Conn, 0, len(tn.conns))
	for c := range tn.conns {
		conns = append(conns, c)
	}
	tn.mu.Unlock()
	_ = tn.listener.Close()
	for _, c := range conns {
		_ = c.Close()
	}
}

// Tunnels returns all tunnels of a session, ordered by creation time.
func (t *TerminalService) Tunnels(sessionID string) []types.Tunnel {
	t.tunnelMu.Lock()
	list := make([]*sshTunnel, 0, len(t.tunnels))
	for _, tn := range t.tunnels {
		if tn.sessionID == sessionID {
			list = append(list, tn)
		}
	}
	t.tunnelMu.Unlock()

	// map 遍历顺序随机，必须显式按创建顺序排序，否则前端每 3 秒轮询都会跳动
	sort.Slice(list, func(i, j int) bool { return list[i].seq < list[j].seq })

	out := make([]types.Tunnel, 0, len(list))
	for _, tn := range list {
		out = append(out, tn.info())
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

	var err error
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "local":
		if strings.TrimSpace(target) == "" {
			return types.Tunnel{}, errors.New("本地转发需要填写「远程目标」地址（host:port）")
		}
		target, err = normalizeTarget(target)
		if err != nil {
			return types.Tunnel{}, fmt.Errorf("远程目标无效：%w", err)
		}
	case "remote":
		if strings.TrimSpace(target) == "" {
			return types.Tunnel{}, errors.New("远程转发需要填写「本机目标」地址（host:port）")
		}
		target, err = normalizeTarget(target)
		if err != nil {
			return types.Tunnel{}, fmt.Errorf("本机目标无效：%w", err)
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
	seq := atomic.AddUint64(&t.tunnelSeq, 1)
	t.tunnelMu.Lock()
	tn := &sshTunnel{
		id:        types.NewID(),
		sessionID: id,
		seq:       seq,
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

// normalizeTarget 规范化转发目标：必须是 host:port（裸端口按 127.0.0.1 处理）。
// 不校验会等到真正建连时才失败，而那时的错误只体现在某一次连接上，用户看不到。
func normalizeTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("地址不能为空")
	}
	// 允许只写端口：本机服务常见写法
	if p, err := strconv.Atoi(target); err == nil {
		if p < 1 || p > 65535 {
			return "", errors.New("端口超出范围（1-65535）")
		}
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(p)), nil
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return "", fmt.Errorf("格式应为 host:port（当前 %q）", target)
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return "", errors.New("缺少主机地址")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return "", fmt.Errorf("端口无效：%q", port)
	}
	return net.JoinHostPort(host, strconv.Itoa(p)), nil
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
		if !tn.track(conn) {
			_ = conn.Close()
			return
		}
		go tn.handle(conn)
	}
}

func (tn *sshTunnel) handle(conn net.Conn) {
	defer func() {
		tn.untrack(conn)
		_ = conn.Close()
	}()

	switch tn.kind {
	case "socks":
		tn.handleSocks(conn)
	case "remote":
		// 远端收到的连接 → 本机目标
		local, err := net.Dial("tcp", tn.target)
		if err != nil {
			tn.setErr(fmt.Sprintf("连接本机目标 %s 失败：%v", tn.target, err))
			return
		}
		defer local.Close()
		tn.clearErr()
		relay(conn, local)
	default: // local
		remote, err := tn.client.Dial("tcp", tn.target)
		if err != nil {
			tn.setErr(fmt.Sprintf("连接远端目标 %s 失败：%v", tn.target, err))
			return
		}
		defer remote.Close()
		tn.clearErr()
		relay(conn, remote)
	}
}

// handleSocks 实现 SOCKS5 服务端（RFC 1928 CONNECT，可选 RFC 1929 认证）。
// 先真正拨号成功再回应答，失败时回对应的 REP 码，避免客户端以为连上了。
func (tn *sshTunnel) handleSocks(conn net.Conn) {
	br := bufio.NewReader(conn)
	target, err := socks5Handshake(br, conn, tn.socksUser, tn.socksPass)
	if err != nil {
		return
	}
	remote, err := tn.client.Dial("tcp", target)
	if err != nil {
		_ = writeSocksReply(conn, socksRepForErr(err))
		tn.setErr(fmt.Sprintf("SOCKS5 连接 %s 失败：%v", target, err))
		return
	}
	defer remote.Close()
	tn.clearErr()
	if err := writeSocksReply(conn, socksRepSucceeded); err != nil {
		return
	}

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(remote, br); closeWrite(remote); done <- struct{}{} }() // 客户端(可能含缓冲) → 远端
	go func() { _, _ = io.Copy(conn, remote); closeWrite(conn); done <- struct{}{} }()
	<-done
	select {
	case <-done:
	case <-time.After(relayDrainTimeout):
	}
}

// relay 双向拷贝两个连接。任一方向读到 EOF 时先半关闭对端写方向（把 EOF 传递
// 过去），再等另一个方向把剩余数据传完；只等一个方向就整体关闭会截断响应。
func relay(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(b, a); closeWrite(b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(a, b); closeWrite(a); done <- struct{}{} }()
	<-done
	select {
	case <-done:
	case <-time.After(relayDrainTimeout):
	}
}

// relayDrainTimeout 是一个方向结束后，等待另一个方向收尾的最长时间。
// 取值宽松一些以覆盖「客户端先半关闭、服务端仍在返回大响应」的场景。
const relayDrainTimeout = 60 * time.Second

// closeWrite 半关闭连接（只发 FIN，不再写）。不支持的连接类型退化为整体关闭。
func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = c.Close()
}

// SOCKS5 应答码（RFC 1928 §6）。
const (
	socksRepSucceeded       = 0x00
	socksRepGeneralFailure  = 0x01
	socksRepNetworkUnreach  = 0x03
	socksRepHostUnreach     = 0x04
	socksRepConnectionRefus = 0x05
	socksRepTTLExpired      = 0x06
)

// writeSocksReply 写 SOCKS5 应答，绑定地址固定回 0.0.0.0:0。
func writeSocksReply(w io.Writer, rep byte) error {
	_, err := w.Write([]byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	return err
}

// socksRepForErr 把拨号错误映射成 SOCKS5 应答码，便于客户端给出准确提示。
func socksRepForErr(err error) byte {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return socksRepHostUnreach
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return socksRepTTLExpired
		}
		msg := strings.ToLower(opErr.Err.Error())
		switch {
		case strings.Contains(msg, "refused"):
			return socksRepConnectionRefus
		case strings.Contains(msg, "unreachable"), strings.Contains(msg, "no route"):
			return socksRepNetworkUnreach
		}
	}
	return socksRepGeneralFailure
}

// socks5Handshake 完成 SOCKS5 协商与请求解析，返回要连接的目标 host:port。
// 只负责方法选择应答与请求读取，成功应答由调用方在真正拨号后发送。
// user 非空时启用 RFC 1929 用户名/密码认证。
func socks5Handshake(br *bufio.Reader, w io.Writer, user, pass string) (string, error) {
	// 1) 问候：VER + NMETHODS + METHODS
	var g [2]byte
	if _, err := io.ReadFull(br, g[:]); err != nil {
		return "", err
	}
	if g[0] != 0x05 {
		return "", errors.New("非 SOCKS5 协议")
	}
	methods := make([]byte, int(g[1]))
	if len(methods) > 0 {
		if _, err := io.ReadFull(br, methods); err != nil {
			return "", err
		}
	}
	// 需要的认证方式：有用户名则要求 0x02，否则要求 0x00（RFC 1928 §3）
	want := byte(0x00)
	if user != "" {
		want = 0x02
	}
	ok := false
	for _, m := range methods {
		if m == want {
			ok = true
			break
		}
	}
	if !ok {
		_, _ = w.Write([]byte{0x05, 0xFF}) // 没有可接受的方法
		return "", errors.New("客户端未提供所需的 SOCKS5 认证方式")
	}
	if _, err := w.Write([]byte{0x05, want}); err != nil {
		return "", err
	}
	if want == 0x02 {
		if err := socks5Auth(br, w, user, pass); err != nil {
			return "", err
		}
	}

	// 2) 请求：VER + CMD + RSV + ATYP + DST.ADDR + DST.PORT
	var h [4]byte
	if _, err := io.ReadFull(br, h[:]); err != nil {
		return "", err
	}
	if h[0] != 0x05 || h[1] != 0x01 { // 仅支持 CONNECT
		_ = writeSocksReply(w, 0x07) // Command not supported
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
		_ = writeSocksReply(w, 0x08) // Address type not supported
		return "", errors.New("不支持的地址类型")
	}

	var p [2]byte
	if _, err := io.ReadFull(br, p[:]); err != nil {
		return "", err
	}
	port := int(p[0])<<8 | int(p[1])
	if port == 0 {
		_ = writeSocksReply(w, socksRepGeneralFailure)
		return "", errors.New("目标端口无效")
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
