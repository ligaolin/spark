// Package secure provides credential encryption for persisted connection
// profiles. Values are encrypted with AES-256-GCM using a key derived from
// either a user-provided sync key (SetKeySeed) or machine-specific
// identifiers, so the ciphertext is only readable on this machine (or on any
// machine sharing the same sync key).
package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"sync"
)

const prefix = "enc:"

var (
	mu     sync.Mutex
	key    []byte
	seed   string
	hasKey bool
)

// SetKeySeed sets the credential encryption key seed (e.g. a sync key).
// Pass an empty string to fall back to the machine-bound key.
func SetKeySeed(s string) {
	mu.Lock()
	defer mu.Unlock()
	seed = s
	recomputeLocked()
}

// CurrentKeySeed returns the currently configured key seed.
func CurrentKeySeed() string {
	mu.Lock()
	defer mu.Unlock()
	return seed
}

func currentSeed() string {
	if seed != "" {
		return seed
	}
	return machineID()
}

func recomputeLocked() {
	sum := sha256.Sum256([]byte("spark-credential-key:" + currentSeed()))
	key = sum[:]
	hasKey = true
}

func machineID() string {
	if id, err := machineGuid(); err == nil && id != "" {
		return "guid:" + id
	}
	host, _ := os.Hostname()
	return "host:" + host + "|user:" + os.Getenv("USERPROFILE") + os.Getenv("HOME")
}

func currentKey() []byte {
	mu.Lock()
	defer mu.Unlock()
	if !hasKey {
		recomputeLocked()
	}
	return key
}

// Encrypt encrypts plain text. Empty values and already-encrypted values are
// returned unchanged (idempotent).
func Encrypt(plain string) (string, error) {
	if plain == "" || strings.HasPrefix(plain, prefix) {
		return plain, nil
	}
	k := currentKey()
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. Values without the encryption prefix (legacy
// plaintext) are returned unchanged.
func Decrypt(data string) (string, error) {
	if data == "" || !strings.HasPrefix(data, prefix) {
		return data, nil
	}
	k := currentKey()
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(data, prefix))
	if err != nil {
		return "", errors.New("凭据数据格式损坏")
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("凭据数据不完整")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", errors.New("凭据解密失败（密钥可能已变化）")
	}
	return string(plain), nil
}
