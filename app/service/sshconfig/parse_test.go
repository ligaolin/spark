package sshconfig

import (
	"reflect"
	"testing"
)

func TestParseBasicHosts(t *testing.T) {
	content := `
# 生产环境
Host prod
    HostName 10.0.0.5
    User root
    Port 2222

Host github
    HostName github.com
    User git
    IdentityFile ~/.ssh/id_ed25519
`
	hosts, err := (&SshConfigService{}).Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d: %+v", len(hosts), hosts)
	}
	if hosts[0].Name != "prod" || hosts[0].HostName != "10.0.0.5" ||
		hosts[0].User != "root" || hosts[0].Port != 2222 {
		t.Fatalf("host 0 wrong: %+v", hosts[0])
	}
	if hosts[1].Name != "github" || hosts[1].HostName != "github.com" ||
		hosts[1].IdentityFile != "~/.ssh/id_ed25519" || hosts[1].Port != 22 {
		t.Fatalf("host 1 wrong: %+v", hosts[1])
	}
}

func TestParseSkipsWildcardAndInheritsDefaults(t *testing.T) {
	content := `
Host * 
    User root
    IdentityFile ~/.ssh/id_rsa

Host web
    HostName web.example.com
`
	hosts, err := (&SshConfigService{}).Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	h := hosts[0]
	if h.Name != "web" || h.HostName != "web.example.com" {
		t.Fatalf("host wrong: %+v", h)
	}
	if h.User != "root" || h.IdentityFile != "~/.ssh/id_rsa" {
		t.Fatalf("defaults not inherited: %+v", h)
	}
}

func TestParseQuotedAndInlineComment(t *testing.T) {
	content := `Host "my host" # 注释
    HostName 1.2.3.4 # 尾注
`
	hosts, err := (&SshConfigService{}).Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []SshHost{{Name: "my host", HostName: "1.2.3.4", Port: 22}}
	if !reflect.DeepEqual(hosts, want) {
		t.Fatalf("got %+v, want %+v", hosts, want)
	}
}
