//go:build windows

package terminal

import (
	"errors"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// opensshAgentPipe is the named pipe of the OpenSSH ssh-agent service on
// Windows. It speaks the standard ssh-agent protocol directly over the pipe.
const opensshAgentPipe = `\\.\pipe\openssh-ssh-agent`

// dialLocalAgent connects to the local OpenSSH agent on Windows.
func dialLocalAgent() (net.Conn, error) {
	timeout := 2 * time.Second
	conn, err := winio.DialPipe(opensshAgentPipe, &timeout)
	if err != nil {
		return nil, errors.New("未找到本地 SSH Agent（请确认 OpenSSH 的 ssh-agent 服务已启动）")
	}
	return conn, nil
}
