// Package sshkeys manages app-generated SSH key pairs: generation, import,
// fingerprint display and one-click deployment of a public key to a saved
// SSH server's ~/.ssh/authorized_keys.
package sshkeys

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"spark/app/model"
	"spark/app/service/db"
	"spark/app/service/sshconn"
	"spark/app/service/sshlib"

	xssh "golang.org/x/crypto/ssh"
)

// SSHKeyService exposes key management to the frontend.
type SSHKeyService struct{}

// ServiceName implements application.ServiceName.
func (s *SSHKeyService) ServiceName() string { return "SSHKeyService" }

// List returns all managed keys, newest first.
func (s *SSHKeyService) List() ([]model.SSHKey, error) {
	var list []model.SSHKey
	err := db.GetDB().Order("created_at desc").Find(&list).Error
	return list, err
}

// Generate creates a new key pair (ed25519 or rsa) and persists it.
func (s *SSHKeyService) Generate(name, keyType string, bits int, comment string) (model.SSHKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.SSHKey{}, errors.New("名称不能为空")
	}
	comment = strings.TrimSpace(comment)
	if comment == "" {
		comment = "spark-key-" + name
	}

	var privPEM []byte
	var signer xssh.Signer
	switch keyType {
	case "", "ed25519":
		keyType = "ed25519"
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return model.SSHKey{}, err
		}
		block, err := xssh.MarshalPrivateKey(priv, "")
		if err != nil {
			return model.SSHKey{}, err
		}
		privPEM = pem.EncodeToMemory(block)
		if signer, err = xssh.NewSignerFromKey(priv); err != nil {
			return model.SSHKey{}, err
		}
	case "rsa":
		if bits == 0 {
			bits = 4096
		}
		if bits < 2048 {
			return model.SSHKey{}, errors.New("RSA 密钥长度不能小于 2048")
		}
		key, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			return model.SSHKey{}, err
		}
		privPEM = pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		})
		if signer, err = xssh.NewSignerFromKey(key); err != nil {
			return model.SSHKey{}, err
		}
	default:
		return model.SSHKey{}, fmt.Errorf("不支持的密钥类型: %s", keyType)
	}

	key, err := persistKey(name, keyType, comment, privPEM, signer.PublicKey(), "")
	if err != nil {
		return model.SSHKey{}, err
	}
	return key, nil
}

// Import adds an existing private key (PEM, optionally passphrase-protected)
// under a new name. The public key is derived from the private key.
func (s *SSHKeyService) Import(name, privateKey, passphrase string) (model.SSHKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.SSHKey{}, errors.New("名称不能为空")
	}
	if strings.TrimSpace(privateKey) == "" {
		return model.SSHKey{}, errors.New("私钥内容不能为空")
	}
	pem := strings.TrimSpace(privateKey) + "\n"
	signer, err := sshlib.ParsePrivateKey(pem, passphrase)
	if err != nil {
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "passphrase") || strings.Contains(low, "encrypted") {
			return model.SSHKey{}, fmt.Errorf("解析私钥失败：私钥带有口令，请在「口令」中填写: %w", err)
		}
		return model.SSHKey{}, fmt.Errorf("解析私钥失败: %w", err)
	}
	comment := "spark-imported-" + name
	key, err := persistKey(name, keyTypeOf(signer.PublicKey()), comment, []byte(pem), signer.PublicKey(), passphrase)
	if err != nil {
		return model.SSHKey{}, err
	}
	return key, nil
}

func persistKey(name, keyType, comment string, privPEM []byte, pub xssh.PublicKey, passphrase string) (model.SSHKey, error) {
	line := strings.TrimSpace(string(xssh.MarshalAuthorizedKey(pub)))
	fingerprint := xssh.FingerprintSHA256(pub)
	var existing model.SSHKey
	if err := db.GetDB().Where("fingerprint = ?", fingerprint).First(&existing).Error; err == nil {
		return model.SSHKey{}, fmt.Errorf("该密钥已存在（%s），无需重复导入", existing.Name)
	}
	key := model.SSHKey{
		Name:        name,
		Type:        keyType,
		Comment:     comment,
		PublicKey:   line + " " + comment,
		PrivateKey:  string(privPEM),
		Passphrase:  passphrase,
		Fingerprint: fingerprint,
	}
	if err := db.GetDB().Create(&key).Error; err != nil {
		return model.SSHKey{}, err
	}
	return key, nil
}

func keyTypeOf(pub xssh.PublicKey) string {
	t := pub.Type()
	if strings.HasPrefix(t, "ssh-rsa") {
		return "rsa"
	}
	if strings.HasPrefix(t, "ssh-ed25519") {
		return "ed25519"
	}
	return t
}

// Delete removes a managed key.
func (s *SSHKeyService) Delete(id uint) error {
	if id == 0 {
		return errors.New("密钥 ID 不能为空")
	}
	return db.GetDB().Delete(&model.SSHKey{}, id).Error
}

// Deploy appends the key's public key to ~/.ssh/authorized_keys on a saved SSH
// connection (deduplicated). Returns a short human-readable result message.
func (s *SSHKeyService) Deploy(keyID, connID uint) (string, error) {
	var key model.SSHKey
	if err := db.GetDB().First(&key, keyID).Error; err != nil {
		return "", errors.New("密钥不存在")
	}
	var cn model.SavedConnection
	if err := db.GetDB().First(&cn, connID).Error; err != nil {
		return "", errors.New("连接不存在")
	}
	if cn.Type == "ftp" {
		return "", errors.New("FTP 连接不支持部署公钥")
	}
	if strings.TrimSpace(cn.Host) == "" {
		return "", errors.New("连接未配置主机地址")
	}

	opts := sshconn.Resolve(sshconn.OptionsFromSaved(cn))
	cfg, err := sshlib.BuildClientConfig(opts)
	if err != nil {
		return "", err
	}
	port := cn.Port
	if port <= 0 || port > 65535 {
		port = 22
	}
	addr := net.JoinHostPort(cn.Host, strconv.Itoa(port))
	client, err := xssh.Dial("tcp", addr, cfg)
	if err != nil {
		if hk := sshlib.AsHostKeyError(err); hk != nil {
			return "", fmt.Errorf("主机密钥校验未通过，请先在终端中连接并信任该服务器：%w", hk)
		}
		return "", fmt.Errorf("连接 %s 失败: %w", addr, err)
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	pub := shellQuote(key.PublicKey)
	cmd := "mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && " +
		"if grep -qxF " + pub + " ~/.ssh/authorized_keys; then echo __SPARK_EXISTS__; else printf '%s\\n' " + pub +
		" >> ~/.ssh/authorized_keys && echo __SPARK_ADDED__; fi"
	out, err := sess.CombinedOutput(cmd)
	text := strings.TrimSpace(string(out))
	if err != nil && !strings.Contains(text, "__SPARK_") {
		return "", fmt.Errorf("部署失败: %v %s", err, text)
	}
	if strings.Contains(text, "__SPARK_EXISTS__") {
		return "公钥已存在于目标服务器的 authorized_keys 中，无需重复部署", nil
	}
	if strings.Contains(text, "__SPARK_ADDED__") {
		return "公钥已追加到目标服务器的 ~/.ssh/authorized_keys", nil
	}
	return text, nil
}

// shellQuote wraps s in single quotes for POSIX shell, escaping inner quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}