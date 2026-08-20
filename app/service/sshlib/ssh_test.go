package sshlib

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changeme/app/service/types"

	xssh "golang.org/x/crypto/ssh"
)

// startTestServer 启动一个内存 SSH 服务器，返回地址与主机公钥。
func startTestServer(t *testing.T) (addr string, pub xssh.PublicKey, cleanup func()) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := xssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	config := &xssh.ServerConfig{
		PasswordCallback: func(conn xssh.ConnMetadata, pw []byte) (*xssh.Permissions, error) {
			if string(pw) == "test-pass" {
				return nil, nil
			}
			return nil, fmt.Errorf("auth failed")
		},
	}
	config.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func(nc net.Conn) {
				_, chans, reqs, err := xssh.NewServerConn(nc, config)
				if err != nil {
					return
				}
				go xssh.DiscardRequests(reqs)
				for ch := range chans {
					if ch.ChannelType() != "session" {
						_ = ch.Reject(xssh.UnknownChannelType, "only session")
						continue
					}
					channel, requests, err := ch.Accept()
					if err != nil {
						continue
					}
					go func() {
						for req := range requests {
							switch req.Type {
							case "shell", "exec":
								_ = req.Reply(true, nil)
								_, _ = channel.Write([]byte("ok\r\n"))
								_ = channel.Close()
							default:
								_ = req.Reply(false, nil)
							}
						}
					}()
				}
			}(nc)
		}
	}()
	return ln.Addr().String(), signer.PublicKey(), func() {
		_ = ln.Close()
		<-done
	}
}

// backupKnownHosts 备份并清空真实 known_hosts，测试结束后恢复。
func backupKnownHosts(t *testing.T) {
	t.Helper()
	p := KnownHostsPath()
	orig := []byte(nil)
	if data, err := os.ReadFile(p); err == nil {
		orig = data
	}
	_ = os.Remove(p)
	t.Cleanup(func() {
		_ = os.Remove(p)
		if len(orig) > 0 {
			_ = os.MkdirAll(filepath.Dir(p), 0o700)
			_ = os.WriteFile(p, orig, 0o600)
		}
	})
}

func hostPort(addr string) (string, int) {
	h, p, _ := net.SplitHostPort(addr)
	var port int
	fmt.Sscanf(p, "%d", &port)
	return h, port
}

func TestHostKeyLifecycle(t *testing.T) {
	backupKnownHosts(t)
	addr, pub, cleanup := startTestServer(t)
	defer cleanup()
	host, port := hostPort(addr)

	// 1) 未知主机
	status, oldFP, err := CheckHostKey(host, port, pub)
	if err != nil {
		t.Fatalf("CheckHostKey: %v", err)
	}
	if status != "unknown" || oldFP != "" {
		t.Fatalf("expected unknown, got %q %q", status, oldFP)
	}

	// 2) 保存后为 known
	if err := SaveHostKey(host, port, pub); err != nil {
		t.Fatalf("SaveHostKey: %v", err)
	}
	status, _, err = CheckHostKey(host, port, pub)
	if err != nil || status != "known" {
		t.Fatalf("expected known, got %q err=%v", status, err)
	}

	// 3) ListHostKeys 包含该条目
	entries, err := ListHostKeys()
	if err != nil {
		t.Fatalf("ListHostKeys: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Host == host && e.Port == port {
			found = true
		}
	}
	if !found {
		data, _ := os.ReadFile(KnownHostsPath())
		t.Fatalf("ListHostKeys did not include saved entry; entries=%+v file=%q", entries, string(data))
	}

	// 4) 用 BuildClientConfig + Dial 成功连接（主机密钥通过校验）
	cfg, err := BuildClientConfig(types.ConnectOptions{
		Host: host, Port: port, Username: "u", Password: "test-pass",
	})
	if err != nil {
		t.Fatalf("BuildClientConfig: %v", err)
	}
	client, err := xssh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("Dial with known key failed: %v", err)
	}
	_ = client.Close()

	// 5) 换成不同密钥 => mismatch
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherSigner, _ := xssh.NewSignerFromKey(other)
	if err := SaveHostKey(host, port, otherSigner.PublicKey()); err != nil {
		t.Fatal(err)
	}
	status, oldFP, err = CheckHostKey(host, port, pub)
	if err != nil || status != "mismatch" || oldFP == "" {
		t.Fatalf("expected mismatch with old fingerprint, got %q %q err=%v", status, oldFP, err)
	}
	// 连接应失败并带 HOSTKEY_MISMATCH 标记
	cfg, _ = BuildClientConfig(types.ConnectOptions{
		Host: host, Port: port, Username: "u", Password: "test-pass",
	})
	_, err = xssh.Dial("tcp", addr, cfg)
	werr := AsHostKeyError(err)
	if werr == nil {
		t.Fatalf("expected host key mismatch error, got %v", err)
	}
	if !strings.Contains(werr.Error(), "HOSTKEY_MISMATCH|") {
		t.Fatalf("unexpected marker: %q", werr.Error())
	}

	// 6) 恢复正确密钥并删除 => unknown
	if err := SaveHostKey(host, port, pub); err != nil {
		t.Fatal(err)
	}
	if err := RemoveHostKey(host, port); err != nil {
		t.Fatalf("RemoveHostKey: %v", err)
	}
	status, _, _ = CheckHostKey(host, port, pub)
	if status != "unknown" {
		t.Fatalf("expected unknown after removal, got %q", status)
	}
}

func TestProbeHostKey(t *testing.T) {
	backupKnownHosts(t)
	addr, pub, cleanup := startTestServer(t)
	defer cleanup()
	host, port := hostPort(addr)

	got, err := ProbeHostKey(host, port)
	if err != nil {
		t.Fatalf("ProbeHostKey: %v", err)
	}
	if got.Type() != pub.Type() {
		t.Fatalf("key type mismatch: %s vs %s", got.Type(), pub.Type())
	}
	_ = time.Now // keep import if unused
}
