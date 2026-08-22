// Package types contains DTOs shared between the Wails backend services
// and the frontend (they are reflected into TypeScript bindings).
package types

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// ConnectOptions describes a connection request for SSH terminal, SFTP or FTP sessions.
type ConnectOptions struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`

	// UseKey enables public-key authentication; PrivateKey holds the PEM content
	// (RSA / EC / OPENSSH). Passphrase unlocks encrypted keys.
	UseKey     bool   `json:"useKey"`
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase"`

	// ForwardAgent enables SSH agent forwarding: the remote session can use the
	// local machine's SSH agent identities (SSH-only).
	ForwardAgent bool `json:"forwardAgent"`

	// Terminal options (SSH only)
	Rows  int    `json:"rows"`
	Cols  int    `json:"cols"`
	Shell string `json:"shell"` // remote shell to start, e.g. "/bin/bash"; empty = login shell

	// FTP options
	TLS      bool `json:"tls"`      // explicit FTPS
	Insecure bool `json:"insecure"` // skip TLS certificate verification

	// Optional initial remote directory for SFTP/FTP sessions
	DefaultDir string `json:"defaultDir"`

	// Optional pre-assigned session id (used to reuse an existing session)
	SessionID string `json:"sessionId"`
}

// FileEntry describes one file system entry (local, SFTP or FTP).
type FileEntry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	Mode       string    `json:"mode"`
	ModTime    time.Time `json:"modTime"`
	IsDir      bool      `json:"isDir"`
	Symlink    bool      `json:"symlink"`
	LinkTarget string    `json:"linkTarget,omitempty"`
}

// SearchResult describes one search hit: either a filename match or a
// content match (content matches also carry the line number and text).
type SearchResult struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	IsDir   bool      `json:"isDir"`

	// Content search only
	LineNo int    `json:"lineNo,omitempty"`
	Line   string `json:"line,omitempty"`
}

// TerminalOutput is emitted to the frontend with chunks of SSH terminal output.
type TerminalOutput struct {
	SessionID string `json:"sessionId"`
	Data      string `json:"data"`
}

// TerminalExit is emitted when an SSH terminal session ends.
type TerminalExit struct {
	SessionID string `json:"sessionId"`
	Code      int    `json:"code"`
	Error     string `json:"error,omitempty"`
}

// SessionClosed is emitted when a connection dies (detected by keep-alive)
// so the frontend can clear its session state.
type SessionClosed struct {
	SessionID string `json:"sessionId"`
	Type      string `json:"type"` // sftp | ftp
	Reason    string `json:"reason,omitempty"`
}

// TransferProgress is emitted while files are uploaded/downloaded.
type TransferProgress struct {
	SessionID string `json:"sessionId"`
	Op        string `json:"op"` // upload | download
	Name      string `json:"name"`
	Done      int64  `json:"done"`
	Total     int64  `json:"total"`
}

// SessionInfo describes an active session.
type SessionInfo struct {
	SessionID string `json:"sessionId"`
	Type      string `json:"type"` // terminal | sftp | ftp
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Connected bool   `json:"connected"`
}

// ProcessInfo describes one remote process (from ps).
type ProcessInfo struct {
	PID     int     `json:"pid"`
	PPID    int     `json:"ppid"`
	User    string  `json:"user"`
	CPU     float64 `json:"cpu"`
	Mem     float64 `json:"mem"`
	RSS     int64   `json:"rss"` // KB
	Stat    string  `json:"stat"`
	Command string  `json:"command"`
}

// DiskInfo describes one mounted filesystem (bytes).
type DiskInfo struct {
	Mount      string `json:"mount"`
	Size       int64  `json:"size"`
	Used       int64  `json:"used"`
	Avail      int64  `json:"avail"`
	UsePercent int    `json:"usePercent"`
}

// ServerInfo describes basic facts about a remote server (Linux first).
type ServerInfo struct {
	Hostname    string     `json:"hostname"`
	OS          string     `json:"os"`
	Kernel      string     `json:"kernel"`
	Arch        string     `json:"arch"`
	Uptime      string     `json:"uptime"`
	CPUModel    string     `json:"cpuModel"`
	CPUCores    int        `json:"cpuCores"`
	Load1       float64    `json:"load1"`
	Load5       float64    `json:"load5"`
	Load15      float64    `json:"load15"`
	MemoryTotal int64      `json:"memoryTotal"` // bytes
	MemoryUsed  int64      `json:"memoryUsed"`  // bytes
	MemoryAvail int64      `json:"memoryAvail"` // bytes
	Disks       []DiskInfo `json:"disks"`
	Error       string     `json:"error,omitempty"` // 非 Linux 等提示
}

// NetInterface describes one remote network interface.
type NetInterface struct {
	Name      string   `json:"name"`
	State     string   `json:"state"` // up | down | unknown
	Mac       string   `json:"mac"`
	MTU       int      `json:"mtu"`
	Addresses []string `json:"addresses"`
	RxBytes   int64    `json:"rxBytes"`
	TxBytes   int64    `json:"txBytes"`
	RxPackets int64    `json:"rxPackets"`
	TxPackets int64    `json:"txPackets"`
}

// NetListener describes one listening socket on the remote host.
type NetListener struct {
	Proto   string `json:"proto"`   // tcp | tcp6 | udp | udp6
	Address string `json:"address"` // local address:port
	PID     int    `json:"pid"`
	Process string `json:"process"`
}

// NetRoute describes one routing table entry.
type NetRoute struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway"`
	Interface   string `json:"interface"`
}

// NetworkInfo aggregates network facts about a remote server.
// 监听端口已拆分为 NetworkListeners，避免较慢的 ss/netstat 拖累快速信息。
type NetworkInfo struct {
	Interfaces []NetInterface `json:"interfaces"`
	Routes     []NetRoute     `json:"routes"`
	DNS        []string       `json:"dns"`
	Error      string         `json:"error,omitempty"` // 非 Linux 等提示
}

// IpStatus reports the remote host's IPs for the terminal status bar.
type IpStatus struct {
	IPs []string `json:"ips"` // 服务器网卡上的全部 IPv4（去重，不含回环/链路本地）
}

// Tunnel describes one active SSH tunnel on a terminal session.
type Tunnel struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`     // local | remote | socks
	BindAddr string `json:"bindAddr"` // 监听地址：local/socks=本机，remote=远端
	Target   string `json:"target"`   // 目标：local=远端目标，remote=本机目标，socks=空
	Status   string `json:"status"`   // running | stopped | error
	Error    string `json:"error,omitempty"`
}

// NewID returns a random hexadecimal session id.
func NewID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(b)
}
