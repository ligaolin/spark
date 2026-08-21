// Package sshlib builds SSH client configurations and manages host key
// verification against a persistent known_hosts file.
package sshlib

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"changeme/app/service/types"

	"github.com/wailsapp/wails/v3/pkg/application"
	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKeyUnknownError reports an unknown host key during connect.
type HostKeyUnknownError struct {
	Fingerprint string
}

func (e *HostKeyUnknownError) Error() string {
	return "HOSTKEY_UNKNOWN|" + e.Fingerprint
}

// HostKeyMismatchError reports a host key mismatch (possible MITM).
type HostKeyMismatchError struct {
	Fingerprint string
	Old         string
}

func (e *HostKeyMismatchError) Error() string {
	return "HOSTKEY_MISMATCH|" + e.Fingerprint + "|" + e.Old
}

// HostKeyRevokedError reports a revoked host key.
type HostKeyRevokedError struct {
	Fingerprint string
}

func (e *HostKeyRevokedError) Error() string {
	return "HOSTKEY_REVOKED|" + e.Fingerprint
}

// KnownHostsPath returns the path of the app's known_hosts file.
func KnownHostsPath() string {
	// On mobile the OS user-config/home dirs (e.g. /sdcard) are not writable;
	// use the app's private files directory instead.
	var dir string
	if mobile := application.Mobile.StoragePath(); mobile != "" {
		dir = mobile
	} else if d, err := os.UserConfigDir(); err == nil {
		dir = d
	} else {
		home, _ := os.UserHomeDir()
		dir = home
	}
	return filepath.Join(dir, "spark", "known_hosts")
}

// ensureKnownHostsFile creates the known_hosts file if missing.
func ensureKnownHostsFile() error {
	p := KnownHostsPath()
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

// hostKeyCallback verifies the remote host key against known_hosts and maps
// verification failures to structured marker errors carrying the fingerprint.
func hostKeyCallback() (xssh.HostKeyCallback, error) {
	if err := ensureKnownHostsFile(); err != nil {
		return nil, err
	}
	cb, err := knownhosts.New(KnownHostsPath())
	if err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key xssh.PublicKey) error {
		err := cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		fp := xssh.FingerprintSHA256(key)
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) {
			if len(ke.Want) == 0 {
				return &HostKeyUnknownError{Fingerprint: fp}
			}
			olds := make([]string, 0, len(ke.Want))
			for _, w := range ke.Want {
				olds = append(olds, xssh.FingerprintSHA256(w.Key))
			}
			return &HostKeyMismatchError{Fingerprint: fp, Old: strings.Join(olds, ",")}
		}
		var re *knownhosts.RevokedError
		if errors.As(err, &re) {
			return &HostKeyRevokedError{Fingerprint: fp}
		}
		return err
	}, nil
}

// AsHostKeyError converts a dial error into the structured host-key error
// (unknown / mismatch / revoked), or nil if the error is unrelated.
func AsHostKeyError(err error) error {
	var hu *HostKeyUnknownError
	if errors.As(err, &hu) {
		return hu
	}
	var hm *HostKeyMismatchError
	if errors.As(err, &hm) {
		return hm
	}
	var hr *HostKeyRevokedError
	if errors.As(err, &hr) {
		return hr
	}
	return nil
}

// buildAuthMethods assembles SSH auth methods from connection options.
func buildAuthMethods(opts types.ConnectOptions) ([]xssh.AuthMethod, error) {
	if strings.TrimSpace(opts.Username) == "" {
		return nil, errors.New("用户名不能为空")
	}

	var auths []xssh.AuthMethod
	switch {
	case opts.UseKey:
		if strings.TrimSpace(opts.PrivateKey) == "" {
			return nil, errors.New("私钥内容不能为空")
		}
		key, err := ParsePrivateKey(opts.PrivateKey, opts.Passphrase)
		if err != nil {
			return nil, err
		}
		auths = append(auths, xssh.PublicKeys(key))
		if opts.Password != "" {
			auths = append(auths, xssh.Password(opts.Password))
		}
	case opts.Password != "":
		auths = append(auths, xssh.Password(opts.Password))
	default:
		return nil, errors.New("需要提供密码或私钥")
	}
	return auths, nil
}

// BuildClientConfig creates an SSH client config from connection options.
func BuildClientConfig(opts types.ConnectOptions) (*xssh.ClientConfig, error) {
	auths, err := buildAuthMethods(opts)
	if err != nil {
		return nil, err
	}

	cb, err := hostKeyCallback()
	if err != nil {
		return nil, err
	}

	return &xssh.ClientConfig{
		User:            opts.Username,
		Auth:            auths,
		HostKeyCallback: cb,
		Timeout:         15 * time.Second,
	}, nil
}

// TestLogin attempts a full SSH authentication against the host using the
// given credentials. It skips known_hosts verification (a login probe should
// not be blocked by an untrusted key) and returns nil on successful auth.
func TestLogin(opts types.ConnectOptions) error {
	auths, err := buildAuthMethods(opts)
	if err != nil {
		return err
	}
	port := opts.Port
	if port <= 0 || port > 65535 {
		port = 22
	}
	addr := net.JoinHostPort(opts.Host, strconv.Itoa(port))

	config := &xssh.ClientConfig{
		User: opts.Username,
		Auth: auths,
		HostKeyCallback: func(string, net.Addr, xssh.PublicKey) error {
			return nil // 测试用：跳过主机密钥校验
		},
		Timeout: 10 * time.Second,
	}

	conn, err := xssh.Dial("tcp", addr, config)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

// ParsePrivateKey parses a PEM-encoded private key, optionally protected by a passphrase.
func ParsePrivateKey(pem, passphrase string) (xssh.Signer, error) {
	keyBytes := []byte(pem)
	if passphrase != "" {
		return xssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(passphrase))
	}
	return xssh.ParsePrivateKey(keyBytes)
}

// ProbeHostKey connects to the host and returns its current public key,
// without performing user authentication.
func ProbeHostKey(host string, port int) (xssh.PublicKey, error) {
	if port <= 0 || port > 65535 {
		port = 22
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	var captured xssh.PublicKey
	config := &xssh.ClientConfig{
		User: "probe",
		Auth: []xssh.AuthMethod{xssh.Password("__spark_probe__")},
		HostKeyCallback: func(_ string, _ net.Addr, key xssh.PublicKey) error {
			captured = key
			return nil
		},
		Timeout: 10 * time.Second,
	}
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("连接 %s 失败: %w", addr, err)
	}
	defer conn.Close()
	sshConn, _, _, err := xssh.NewClientConn(conn, addr, config)
	if sshConn != nil {
		_ = sshConn.Close()
	}
	if captured == nil {
		if err == nil {
			return nil, errors.New("未能获取主机密钥")
		}
		return nil, fmt.Errorf("SSH 握手失败: %w", err)
	}
	return captured, nil
}

// hostAddr is a minimal net.Addr used when checking host keys without a
// live connection (knownhosts requires a non-nil remote address).
type hostAddr struct {
	host string
	port int
}

func (a hostAddr) Network() string { return "tcp" }
func (a hostAddr) String() string  { return net.JoinHostPort(a.host, strconv.Itoa(a.port)) }

// CheckHostKey returns "known"/"unknown"/"mismatch" for the host's current key.
func CheckHostKey(host string, port int, key xssh.PublicKey) (status string, oldFingerprint string, err error) {
	if err := ensureKnownHostsFile(); err != nil {
		return "", "", err
	}
	cb, err := knownhosts.New(KnownHostsPath())
	if err != nil {
		return "", "", err
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	err = cb(addr, hostAddr{host: host, port: port}, key)
	if err == nil {
		return "known", "", nil
	}
	var ke *knownhosts.KeyError
	if errors.As(err, &ke) {
		if len(ke.Want) == 0 {
			return "unknown", "", nil
		}
		olds := make([]string, 0, len(ke.Want))
		for _, w := range ke.Want {
			olds = append(olds, xssh.FingerprintSHA256(w.Key))
		}
		return "mismatch", strings.Join(olds, ","), nil
	}
	return "", "", err
}

// SaveHostKey writes the host key into known_hosts, replacing any existing
// entry for the same host (and same or different key).
func SaveHostKey(host string, port int, key xssh.PublicKey) error {
	if err := ensureKnownHostsFile(); err != nil {
		return err
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	norm := knownhosts.Normalize(addr)
	newLine := knownhosts.Line([]string{addr}, key)

	data, err := os.ReadFile(KnownHostsPath())
	if err != nil {
		return err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out = append(out, line)
			continue
		}
		fields := strings.Fields(trimmed)
		keyIdx := -1
		for i, f := range fields {
			if isKeyType(f) {
				keyIdx = i
				break
			}
		}
		if keyIdx > 0 {
			hosts := strings.Split(fields[keyIdx-1], ",")
			matched := false
			for _, h := range hosts {
				if h == norm {
					matched = true
					break
				}
			}
			if matched {
				continue // 移除旧条目
			}
		}
		out = append(out, line)
	}
	out = append(out, newLine)
	result := strings.Join(out, "\n") + "\n"
	return os.WriteFile(KnownHostsPath(), []byte(result), 0o600)
}

// RemoveHostKey removes the known_hosts entry for the given host.
func RemoveHostKey(host string, port int) error {
	if err := ensureKnownHostsFile(); err != nil {
		return err
	}
	norm := knownhosts.Normalize(net.JoinHostPort(host, strconv.Itoa(port)))
	data, err := os.ReadFile(KnownHostsPath())
	if err != nil {
		return err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out = append(out, line)
			continue
		}
		fields := strings.Fields(trimmed)
		keyIdx := -1
		for i, f := range fields {
			if isKeyType(f) {
				keyIdx = i
				break
			}
		}
		drop := false
		if keyIdx > 0 {
			for _, h := range strings.Split(fields[keyIdx-1], ",") {
				if h == norm {
					drop = true
					break
				}
			}
		}
		if !drop {
			out = append(out, line)
		}
	}
	return os.WriteFile(KnownHostsPath(), []byte(strings.Join(out, "\n")), 0o600)
}

// ListHostKeys parses known_hosts into structured entries for the management UI.
type HostKeyEntry struct {
	Host        string
	Port        int
	Fingerprint string
	KeyType     string
}

func ListHostKeys() ([]HostKeyEntry, error) {
	if err := ensureKnownHostsFile(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(KnownHostsPath())
	if err != nil {
		return nil, err
	}
	var out []HostKeyEntry
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if fields[0] == "@cert-authority" || fields[0] == "@revoked" {
			continue
		}
		keyIdx := -1
		for i, f := range fields {
			if isKeyType(f) {
				keyIdx = i
				break
			}
		}
		if keyIdx < 0 || keyIdx+1 >= len(fields) {
			continue
		}
		key, _, _, _, err := xssh.ParseAuthorizedKey([]byte(fields[keyIdx] + " " + fields[keyIdx+1]))
		if err != nil {
			continue
		}
		for _, h := range strings.Split(fields[keyIdx-1], ",") {
			host, port := splitKnownHost(h)
			out = append(out, HostKeyEntry{
				Host:        host,
				Port:        port,
				Fingerprint: xssh.FingerprintSHA256(key),
				KeyType:     key.Type(),
			})
		}
	}
	return out, nil
}

func isKeyType(f string) bool {
	return strings.HasPrefix(f, "ssh-") ||
		strings.HasPrefix(f, "ecdsa-") ||
		strings.HasPrefix(f, "sk-")
}

// splitKnownHost parses "host" or "[host]:port" patterns from known_hosts.
func splitKnownHost(pattern string) (string, int) {
	if strings.HasPrefix(pattern, "[") {
		if idx := strings.Index(pattern, "]"); idx > 0 {
			host := pattern[1:idx]
			rest := pattern[idx+1:]
			if strings.HasPrefix(rest, ":") {
				if p, err := strconv.Atoi(strings.TrimPrefix(rest, ":")); err == nil {
					return host, p
				}
			}
			return host, 22
		}
	}
	return pattern, 22
}
