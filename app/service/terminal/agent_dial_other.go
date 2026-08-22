//go:build !windows

package terminal

import (
	"errors"
	"net"
	"os"
)

// dialLocalAgent connects to the local SSH agent via $SSH_AUTH_SOCK (Unix-like
// platforms, including macOS).
func dialLocalAgent() (net.Conn, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("未找到本地 SSH Agent（$SSH_AUTH_SOCK 未设置）")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
