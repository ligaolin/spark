// Package hostkeys exposes SSH host key verification and known_hosts
// management to the frontend.
package hostkeys

import (
	"errors"
	"strings"

	"changeme/app/service/sshlib"
	"changeme/app/service/types"

	xssh "golang.org/x/crypto/ssh"
)

// HostKeyService manages the known_hosts database.
type HostKeyService struct{}

// ServiceName implements application.ServiceName.
func (h *HostKeyService) ServiceName() string { return "HostKeyService" }

// HostKeyStatus describes the verification result for a host's current key.
type HostKeyStatus struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Status         string `json:"status"` // known | unknown | mismatch
	Fingerprint    string `json:"fingerprint"`
	OldFingerprint string `json:"oldFingerprint,omitempty"`
	KeyType        string `json:"keyType"`
}

// HostKeyInfo describes one entry in the known_hosts database.
type HostKeyInfo struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Fingerprint string `json:"fingerprint"`
	KeyType     string `json:"keyType"`
}

// CheckHostKey probes the host and reports whether its key is known,
// unknown or mismatched.
func (h *HostKeyService) CheckHostKey(opts types.ConnectOptions) (*HostKeyStatus, error) {
	if strings.TrimSpace(opts.Host) == "" {
		return nil, errors.New("主机地址不能为空")
	}
	if opts.Port <= 0 || opts.Port > 65535 {
		opts.Port = 22
	}
	key, err := sshlib.ProbeHostKey(opts.Host, opts.Port)
	if err != nil {
		return nil, err
	}
	status, oldFP, err := sshlib.CheckHostKey(opts.Host, opts.Port, key)
	if err != nil {
		return nil, err
	}
	return &HostKeyStatus{
		Host:           opts.Host,
		Port:           opts.Port,
		Status:         status,
		Fingerprint:    xssh.FingerprintSHA256(key),
		OldFingerprint: oldFP,
		KeyType:        key.Type(),
	}, nil
}

// AcceptHostKey trusts the host's current key and stores it in known_hosts,
// replacing any previous entry.
func (h *HostKeyService) AcceptHostKey(opts types.ConnectOptions) error {
	if strings.TrimSpace(opts.Host) == "" {
		return errors.New("主机地址不能为空")
	}
	if opts.Port <= 0 || opts.Port > 65535 {
		opts.Port = 22
	}
	key, err := sshlib.ProbeHostKey(opts.Host, opts.Port)
	if err != nil {
		return err
	}
	return sshlib.SaveHostKey(opts.Host, opts.Port, key)
}

// ListHostKeys returns all known_hosts entries.
func (h *HostKeyService) ListHostKeys() ([]HostKeyInfo, error) {
	entries, err := sshlib.ListHostKeys()
	if err != nil {
		return nil, err
	}
	out := make([]HostKeyInfo, 0, len(entries))
	for _, e := range entries {
		out = append(out, HostKeyInfo{
			Host:        e.Host,
			Port:        e.Port,
			Fingerprint: e.Fingerprint,
			KeyType:     e.KeyType,
		})
	}
	return out, nil
}

// RemoveHostKey deletes the known_hosts entry for the given host.
func (h *HostKeyService) RemoveHostKey(host string, port int) error {
	return sshlib.RemoveHostKey(host, port)
}
