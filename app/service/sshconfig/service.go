// Package sshconfig parses OpenSSH client config files and imports hosts as
// saved connections (Host → 名称, HostName → 主机地址).
package sshconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"changeme/app/model"
	"changeme/app/service/connections"
	"changeme/app/service/db"
)

// SshConfigService parses and imports OpenSSH client config.
type SshConfigService struct{}

// ServiceName implements application.ServiceName.
func (s *SshConfigService) ServiceName() string { return "SshConfigService" }

// SshHost is a single importable host entry parsed from an SSH config.
type SshHost struct {
	Name         string `json:"name"`         // Host 别名（作为连接名称）
	HostName     string `json:"hostName"`     // 真实 IP / 域名
	User         string `json:"user"`
	Port         int    `json:"port"`
	IdentityFile string `json:"identityFile"` // 私钥路径（可为空）
}

// ImportResult reports the outcome of an import.
type ImportResult struct {
	Imported int      `json:"imported"`
	Warnings []string `json:"warnings"`
}

// DefaultConfigPath returns the platform SSH config path (~/.ssh/config).
func (s *SshConfigService) DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

// Parse parses OpenSSH config content into importable host entries.
// Wildcard hosts (containing * ? !) are skipped. Keywords are case-insensitive.
func (s *SshConfigService) Parse(content string) ([]SshHost, error) {
	var hosts []SshHost

	type defaults struct {
		user         string
		port         int
		identityFile string
	}
	var def defaults

	var cur *SshHost

	flush := func() {
		if cur != nil && cur.Name != "" && cur.HostName != "" {
			if cur.Port <= 0 || cur.Port > 65535 {
				cur.Port = 22
			}
			hosts = append(hosts, *cur)
		}
		cur = nil
	}

	lines := strings.Split(content, "\n")
	for _, raw := range lines {
		toks := tokenize(stripComment(raw))
		if len(toks) == 0 {
			continue
		}
		kw := strings.ToLower(toks[0])
		args := toks[1:]

		if kw == "host" {
			flush()
			name := ""
			for _, p := range args {
				if strings.ContainsAny(p, "*?!") {
					continue
				}
				name = p
				break
			}
			if name == "" {
				// 通配符块（如 Host *）：其指令作为后续主机的默认值
				continue
			}
			cur = &SshHost{Name: name, User: def.user, Port: def.port, IdentityFile: def.identityFile}
			continue
		}

		if len(args) == 0 {
			continue
		}
		switch kw {
		case "hostname":
			if cur != nil {
				cur.HostName = args[0]
			}
		case "user":
			if cur != nil {
				cur.User = args[0]
			} else {
				def.user = args[0]
			}
		case "port":
			if p, err := strconv.Atoi(args[0]); err == nil {
				if cur != nil {
					cur.Port = p
				} else {
					def.port = p
				}
			}
		case "identityfile":
			if cur != nil {
				if cur.IdentityFile == "" {
					cur.IdentityFile = args[0]
				}
			} else if def.identityFile == "" {
				def.identityFile = args[0]
			}
		}
	}
	flush()
	return hosts, nil
}

// Import creates saved connections for the given hosts. IdentityFile keys are
// read from disk (with ~ expansion); unreadable keys fall back to password
// auth. Existing connections with the same name are skipped.
func (s *SshConfigService) Import(hosts []SshHost, group string) (ImportResult, error) {
	group = strings.TrimSpace(group)
	res := ImportResult{Warnings: []string{}}
	svc := connections.ConnService{}

	for _, h := range hosts {
		name := strings.TrimSpace(h.Name)
		hostName := strings.TrimSpace(h.HostName)
		if name == "" || hostName == "" {
			res.Warnings = append(res.Warnings, "存在缺少 Host 或 HostName 的条目，已跳过")
			continue
		}

		var exists int64
		if err := db.GetDB().Model(&model.SavedConnection{}).
			Where("name = ?", name).Count(&exists).Error; err != nil {
			return res, err
		}
		if exists > 0 {
			res.Warnings = append(res.Warnings, fmt.Sprintf("「%s」已存在，跳过", name))
			continue
		}

		conn := model.SavedConnection{
			Name:     name,
			Group:    group,
			Type:     "ssh",
			Host:     hostName,
			Port:     h.Port,
			Username: h.User,
		}
		if conn.Port <= 0 || conn.Port > 65535 {
			conn.Port = 22
		}
		if h.IdentityFile != "" {
			if key, err := os.ReadFile(expandUserPath(h.IdentityFile)); err == nil {
				conn.UseKey = true
				conn.PrivateKey = string(key)
			} else {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("「%s」的密钥 %s 读取失败：%v（已按密码认证导入）", name, h.IdentityFile, err))
			}
		}

		if _, err := svc.Create(conn); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("「%s」导入失败：%v", name, err))
			continue
		}
		res.Imported++
	}
	return res, nil
}

// stripComment removes a trailing comment (a # preceded by whitespace or at
// line start) so values containing # that are not comments stay intact.
func stripComment(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return line[:i]
		}
	}
	return line
}

// tokenize splits a line on whitespace, honoring double quotes.
func tokenize(line string) []string {
	var toks []string
	var cur strings.Builder
	inQuote := false
	for _, r := range strings.TrimSpace(line) {
		switch {
		case r == '"':
			inQuote = !inQuote
		case unicode.IsSpace(r) && !inQuote:
			if cur.Len() > 0 {
				toks = append(toks, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		toks = append(toks, cur.String())
	}
	return toks
}

func expandUserPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		return filepath.Join(home, p[2:])
	}
	return p
}
