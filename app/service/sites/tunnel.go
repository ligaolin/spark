// SSH 本地端口转发：把站点链接的 host:port 通过已保存的 SSH 连接转发到
// 本机，让没有图形界面的 Linux 服务器上（仅服务器可达）的服务也能在本地
// 内嵌浏览器里打开。等价于 `ssh -L 127.0.0.1:<本地端口>:<目标host>:<端口>`。
//
// 已知限制（与原生 ssh -L 一致）：
//   - 转发的是原始 TCP 流，浏览器的 Host / SNI 会变成 127.0.0.1:<端口>；
//     对按 Host/SNI 严格区分虚拟主机的站点可能打不开（普通内网服务通常无此限制）。
//   - 链接协议（http/https）决定本地 URL 的协议与默认端口，转发本身不感知协议：
//     目标实际是 http 却写成 https（或反之）时页面会报协议错误，但隧道是通的。
package sites

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"spark/app/model"
	"spark/app/service/db"
	"spark/app/service/sshlib"
	"spark/app/service/types"

	xssh "golang.org/x/crypto/ssh"
)

// tunnelKeepAlive 是隧道心跳间隔。心跳既防止空闲连接被中间设备掐断，也用来
// 发现「SSH 已断开但隧道还挂在列表里」的情况（连续失败后自动摘掉隧道）。
// 做成变量以便测试调小。
var tunnelKeepAlive = 25 * time.Second

// tunnelRelayDrain 是一个方向结束后，等另一个方向把剩余数据传完的最长时间。
const tunnelRelayDrain = 30 * time.Second

// TunnelInfo 描述一个已建立的 SSH 本地端口转发。
type TunnelInfo struct {
	ID             string `json:"id"`
	ConnectionID   uint   `json:"connectionId"`
	ConnectionName string `json:"connectionName"`
	Target         string `json:"target"`   // 远程目标 host:port
	LocalURL       string `json:"localUrl"` // 可在本机浏览器打开的 URL
	LastError      string `json:"lastError,omitempty"`
}

type sshTunnel struct {
	id        string
	seq       int
	connID    uint
	connName  string
	target    string // 服务器侧目标 host:port
	localAddr string // 本机监听地址（127.0.0.1:port）
	listener  net.Listener
	client    *xssh.Client
	stopKA    chan struct{}
	kaOnce    sync.Once

	mu      sync.Mutex
	closed  bool
	lastErr string
	lastURL string
}

var (
	tunnelMu  sync.Mutex
	tunnels   = map[string]*sshTunnel{}
	tunnelSeq int
	// tunnelPorts 记住「连接 + 目标」上次用的本机端口，重复打开时尽量复用同一
	// 地址：这样已经打开的标签页 / 独立窗口在隧道重开后依然有效，
	// 前端按 URL 去重的逻辑也才能命中。
	tunnelPorts = map[string]int{}
)

// ListTunnels 返回当前所有活动隧道，按建立顺序排列。
func (s *SiteService) ListTunnels() []TunnelInfo {
	tunnelMu.Lock()
	list := make([]*sshTunnel, 0, len(tunnels))
	for _, t := range tunnels {
		list = append(list, t)
	}
	tunnelMu.Unlock()

	// map 遍历顺序随机，必须显式排序，否则隧道管理弹窗每次刷新都会重排
	sort.Slice(list, func(i, j int) bool { return list[i].seq < list[j].seq })

	out := make([]TunnelInfo, 0, len(list))
	for _, t := range list {
		out = append(out, t.snapshot())
	}
	return out
}

// OpenTunnel 通过已保存的 SSH 连接建立本地端口转发：占用一个本机端口，
// 把访问该端口的 TCP 流经 SSH 转发到 targetURL 指向的 host:port（在服务器侧解析）。
// 同一个「连接 + 目标」已有活动隧道时直接复用，不会重复建连 / 重复开标签页。
func (s *SiteService) OpenTunnel(connectionID uint, targetURL string) (TunnelInfo, error) {
	var conn model.SavedConnection
	if err := db.GetDB().First(&conn, connectionID).Error; err != nil {
		return TunnelInfo{}, errors.New("找不到指定的连接")
	}
	if strings.ToLower(strings.TrimSpace(conn.Type)) != "ssh" {
		return TunnelInfo{}, errors.New("只有 SSH 连接可用于隧道转发")
	}

	scheme, host, port, path, err := parseTunnelTarget(targetURL)
	if err != nil {
		return TunnelInfo{}, err
	}
	target := net.JoinHostPort(host, strconv.Itoa(port))

	if t := findLiveTunnel(connectionID, target); t != nil {
		return t.touch(scheme, path), nil
	}

	sshPort := conn.Port
	if sshPort <= 0 || sshPort > 65535 {
		sshPort = 22
	}
	opts := types.ConnectOptions{
		Host:       conn.Host,
		Port:       sshPort,
		Username:   conn.Username,
		Password:   conn.Password,
		UseKey:     conn.UseKey,
		PrivateKey: conn.PrivateKey,
		Passphrase: conn.Passphrase,
	}
	config, err := sshlib.BuildClientConfig(opts)
	if err != nil {
		return TunnelInfo{}, err
	}
	client, err := xssh.Dial("tcp", net.JoinHostPort(conn.Host, strconv.Itoa(sshPort)), config)
	if err != nil {
		if sshlib.AsHostKeyError(err) != nil {
			return TunnelInfo{}, errors.New("主机密钥未校验，请先在「SSH 终端」连接一次该主机并确认指纹")
		}
		return TunnelInfo{}, fmt.Errorf("SSH 连接失败: %w", err)
	}

	ln, err := listenLocal(connectionID, target)
	if err != nil {
		_ = client.Close()
		return TunnelInfo{}, fmt.Errorf("创建本地监听失败: %w", err)
	}

	name := conn.Name
	if strings.TrimSpace(name) == "" {
		name = conn.Host
	}
	t := &sshTunnel{
		connID:    connectionID,
		connName:  name,
		target:    target,
		localAddr: ln.Addr().String(),
		listener:  ln,
		client:    client,
		stopKA:    make(chan struct{}),
	}
	registerTunnel(t)

	go t.acceptLoop(target)
	go t.keepAliveLoop()
	return t.touch(scheme, path), nil
}

// CloseTunnel 关闭并释放一个隧道。
func (s *SiteService) CloseTunnel(id string) error {
	tunnelMu.Lock()
	t := tunnels[id]
	tunnelMu.Unlock()
	if t == nil {
		return nil
	}
	t.shutdown("")
	return nil
}

// findLiveTunnel 找出可复用的隧道；顺带清掉已经死掉但还没被心跳摘除的条目。
func findLiveTunnel(connID uint, target string) *sshTunnel {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()
	for id, t := range tunnels {
		if t.connID != connID || t.target != target {
			continue
		}
		if t.isClosed() {
			delete(tunnels, id)
			continue
		}
		return t
	}
	return nil
}

func registerTunnel(t *sshTunnel) {
	tunnelMu.Lock()
	tunnelSeq++
	t.id = strconv.Itoa(tunnelSeq)
	t.seq = tunnelSeq
	tunnels[t.id] = t
	tunnelMu.Unlock()
}

func unregisterTunnel(id string) {
	tunnelMu.Lock()
	delete(tunnels, id)
	tunnelMu.Unlock()
}

// listenLocal 优先复用该「连接 + 目标」上次使用的端口，占用失败再退回随机端口。
func listenLocal(connID uint, target string) (net.Listener, error) {
	key := tunnelPortKey(connID, target)
	tunnelMu.Lock()
	pref := tunnelPorts[key]
	tunnelMu.Unlock()

	if pref > 0 {
		if ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(pref))); err == nil {
			return ln, nil
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	if addr, ok := ln.Addr().(*net.TCPAddr); ok {
		tunnelMu.Lock()
		tunnelPorts[key] = addr.Port
		tunnelMu.Unlock()
	}
	return ln, nil
}

func tunnelPortKey(connID uint, target string) string {
	return strconv.FormatUint(uint64(connID), 10) + "|" + target
}

// touch 记录最近一次使用的本地 URL 并返回快照（同一隧道可能被不同链接/路径复用）。
func (t *sshTunnel) touch(scheme, path string) TunnelInfo {
	t.mu.Lock()
	t.lastURL = scheme + "://" + t.localAddr + path
	t.mu.Unlock()
	return t.snapshot()
}

func (t *sshTunnel) snapshot() TunnelInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	return TunnelInfo{
		ID:             t.id,
		ConnectionID:   t.connID,
		ConnectionName: t.connName,
		Target:         t.target,
		LocalURL:       t.lastURL,
		LastError:      t.lastErr,
	}
}

func (t *sshTunnel) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// setLastError 记录最近一次转发失败原因（隧道本身仍在监听，不改状态）。
func (t *sshTunnel) setLastError(msg string) {
	t.mu.Lock()
	t.lastErr = msg
	t.mu.Unlock()
}

func (t *sshTunnel) clearLastError() {
	t.mu.Lock()
	t.lastErr = ""
	t.mu.Unlock()
}

// shutdown 关闭监听与 SSH 连接，并把隧道从活动列表摘除。可重复调用。
func (t *sshTunnel) shutdown(reason string) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	if reason != "" {
		t.lastErr = reason
	}
	t.mu.Unlock()

	t.kaOnce.Do(func() { close(t.stopKA) })
	_ = t.listener.Close()
	_ = t.client.Close()
	unregisterTunnel(t.id)
}

// keepAliveLoop 定期心跳：发现 SSH 已断开时自动摘掉隧道，
// 否则前端会一直显示一条其实已经不通的「活动隧道」。
func (t *sshTunnel) keepAliveLoop() {
	sshlib.KeepAliveLoop(t.stopKA, func() error {
		_, _, err := t.client.SendRequest("keepalive@openssh.com", true, nil)
		return err
	}, tunnelKeepAlive, 10*time.Second, 3, func() {
		t.shutdown("SSH 连接已断开，隧道已自动关闭")
	})
}

func (t *sshTunnel) acceptLoop(target string) {
	for {
		local, err := t.listener.Accept()
		if err != nil {
			return
		}
		go t.handle(local, target)
	}
}

func (t *sshTunnel) handle(local net.Conn, target string) {
	defer local.Close()

	remote, err := t.client.Dial("tcp", target)
	if err != nil {
		t.setLastError(fmt.Sprintf("连接目标 %s 失败：%v", target, err))
		// SSH 通道本身没了就整体收摊，别留一条点不开的隧道
		if isSSHTransportDead(err) {
			t.shutdown("SSH 连接已断开，隧道已自动关闭")
		}
		return
	}
	defer remote.Close()
	t.clearLastError()
	relayTunnel(local, remote)
}

// relayTunnel 双向拷贝。任一方向读到 EOF 时先半关闭对端写方向（把 EOF 传过去），
// 再等另一个方向把剩余数据传完；只等一个方向就整体关闭会截断响应。
func relayTunnel(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(b, a); closeWriteHalf(b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(a, b); closeWriteHalf(a); done <- struct{}{} }()
	<-done
	select {
	case <-done:
	case <-time.After(tunnelRelayDrain):
	}
}

// closeWriteHalf 半关闭连接（只发 FIN）。不支持的连接类型退化为整体关闭。
func closeWriteHalf(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = c.Close()
}

// isSSHTransportDead 判断 Dial 报错是不是「SSH 连接已经没了」，
// 用来区分「目标服务没起来（隧道还能用）」和「隧道本身断了」。
func isSSHTransportDead(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{
		"use of closed network connection",
		"ssh: disconnect",
		"connection lost",
		"broken pipe",
		"eof",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// parseTunnelTarget 解析站点链接，返回协议、目标主机、端口与路径（含查询串）。
func parseTunnelTarget(raw string) (scheme, host string, port int, path string, err error) {
	u := normalizeURL(raw) // 无协议时补 https://
	parsed, perr := url.Parse(u)
	if perr != nil {
		return "", "", 0, "", fmt.Errorf("链接地址格式不正确: %w", perr)
	}
	scheme = parsed.Scheme
	host = parsed.Hostname()
	if host == "" {
		return "", "", 0, "", errors.New("无法从链接地址解析出目标主机")
	}
	if p := parsed.Port(); p != "" {
		if n, aerr := strconv.Atoi(p); aerr == nil && n > 0 && n <= 65535 {
			port = n
		}
	}
	if port == 0 {
		if scheme == "http" {
			port = 80
		} else {
			port = 443
		}
	}
	path = parsed.RequestURI()
	if path == "" {
		path = "/"
	}
	return scheme, host, port, path, nil
}
