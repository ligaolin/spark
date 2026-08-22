// SSH 本地端口转发：把站点链接的 host:port 通过已保存的 SSH 连接转发到
// 本机，让没有图形界面的 Linux 服务器上（仅服务器可达）的服务也能在本地
// 内嵌浏览器里打开。等价于 `ssh -L 127.0.0.1:<本地端口>:<目标host>:<端口>`。
//
// 已知限制（与原生 ssh -L 一致）：
//   - 转发的是原始 TCP 流，浏览器的 Host / SNI 会变成 127.0.0.1:<端口>；
//     对按 Host/SNI 严格区分虚拟主机的站点可能打不开（普通内网服务通常无此限制）。
package sites

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"changeme/app/model"
	"changeme/app/service/db"
	"changeme/app/service/sshlib"
	"changeme/app/service/types"

	xssh "golang.org/x/crypto/ssh"
)

// TunnelInfo 描述一个已建立的 SSH 本地端口转发。
type TunnelInfo struct {
	ID             string `json:"id"`
	ConnectionName string `json:"connectionName"`
	Target         string `json:"target"`   // 远程目标 host:port
	LocalURL       string `json:"localUrl"` // 可在本机浏览器打开的 URL
}

type sshTunnel struct {
	info     TunnelInfo
	listener net.Listener
	client   *xssh.Client
}

var (
	tunnelMu  sync.Mutex
	tunnels   = map[string]*sshTunnel{}
	tunnelSeq int
)

// ListTunnels 返回当前所有活动隧道。
func (s *SiteService) ListTunnels() []TunnelInfo {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()
	out := make([]TunnelInfo, 0, len(tunnels))
	for _, t := range tunnels {
		out = append(out, t.info)
	}
	return out
}

// OpenTunnel 通过已保存的 SSH 连接建立本地端口转发：随机占用一个本机端口，
// 把访问该端口的 TCP 流经 SSH 转发到 targetURL 指向的 host:port（在服务器侧解析）。
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

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = client.Close()
		return TunnelInfo{}, fmt.Errorf("创建本地监听失败: %w", err)
	}

	name := conn.Name
	if strings.TrimSpace(name) == "" {
		name = conn.Host
	}

	tunnelMu.Lock()
	tunnelSeq++
	t := &sshTunnel{
		info: TunnelInfo{
			ID:             strconv.Itoa(tunnelSeq),
			ConnectionName: name,
			Target:         target,
			LocalURL:       scheme + "://" + ln.Addr().String() + path,
		},
		listener: ln,
		client:   client,
	}
	tunnels[t.info.ID] = t
	tunnelMu.Unlock()

	go t.acceptLoop(host, port)
	return t.info, nil
}

// CloseTunnel 关闭并释放一个隧道。
func (s *SiteService) CloseTunnel(id string) error {
	tunnelMu.Lock()
	t := tunnels[id]
	delete(tunnels, id)
	tunnelMu.Unlock()
	if t == nil {
		return nil
	}
	_ = t.listener.Close()
	_ = t.client.Close()
	return nil
}

func (t *sshTunnel) acceptLoop(host string, port int) {
	target := net.JoinHostPort(host, strconv.Itoa(port))
	for {
		local, err := t.listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer local.Close()
			remote, err := t.client.Dial("tcp", target)
			if err != nil {
				return
			}
			defer remote.Close()
			done := make(chan struct{}, 2)
			go func() { _, _ = io.Copy(remote, local); done <- struct{}{} }()
			go func() { _, _ = io.Copy(local, remote); done <- struct{}{} }()
			<-done // 任一方向结束即关闭双方
		}()
	}
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
