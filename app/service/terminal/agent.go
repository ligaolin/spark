// SSH Agent 转发（ssh-agent forwarding）。
//
// 连接本地 SSH Agent（Unix 走 $SSH_AUTH_SOCK，Windows 走 OpenSSH 的命名管道），
// 注册 auth-agent@openssh.com 通道处理器，并向会话请求转发。每个转发通道单独
// 连一条本地 agent 连接做纯字节透传（等价于 OpenSSH 客户端把 agent 流量原样
// 转发到本机 $SSH_AUTH_SOCK），远端即可复用本机 agent 里的密钥。
package terminal

import (
	"fmt"
	"io"
	"net"
	"sync"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const agentChannelType = "auth-agent@openssh.com"

// setupAgentForwarding wires agent forwarding onto the given SSH client and
// session. A missing/unreachable local agent is returned as an error (so the
// caller can fail fast with a clear message); a server-side denial of the
// forwarding request is treated as non-fatal (the interactive shell still works).
func setupAgentForwarding(client *xssh.Client, sess *xssh.Session) error {
	// 先探测本地 agent 是否可达，失败则快速给出明确提示。
	probe, err := dialLocalAgent()
	if err != nil {
		return fmt.Errorf("无法连接本地 SSH Agent：%w", err)
	}
	_ = probe.Close()

	channels := client.HandleChannelOpen(agentChannelType)
	if channels == nil {
		return fmt.Errorf("配置 Agent 转发失败：该 SSH 连接已注册 agent 通道处理器")
	}

	go func() {
		for ch := range channels {
			channel, reqs, err := ch.Accept()
			if err != nil {
				continue
			}
			go xssh.DiscardRequests(reqs)
			go func() {
				_, _ = io.Copy(io.Discard, channel.Stderr())
			}()
			go func() {
				conn, err := dialLocalAgent()
				if err != nil {
					_ = channel.Close()
					return
				}
				relayAgent(channel, conn)
			}()
		}
	}()

	// 服务器拒绝转发时不阻断连接（OpenSSH 默认允许转发）。
	_ = agent.RequestAgentForwarding(sess)
	return nil
}

// relayAgent 双向透传 SSH 通道与本地 agent 连接，任一方向结束即关闭两端。
func relayAgent(channel xssh.Channel, conn net.Conn) {
	defer channel.Close()
	defer conn.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(conn, channel)
		// 半关闭写方向，向本地 agent 传递 EOF（Unix socket 支持 CloseWrite）。
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(channel, conn)
		_ = channel.CloseWrite()
	}()
	wg.Wait()
}
