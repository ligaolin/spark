package secure

import (
	"strings"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	plain := "P@ssw0rd-机密-私钥PEM"
	enc, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc == plain {
		t.Fatal("ciphertext must differ from plaintext")
	}
	if !strings.HasPrefix(enc, prefix) {
		t.Fatalf("missing enc prefix: %q", enc)
	}
	dec, err := Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if dec != plain {
		t.Fatalf("roundtrip mismatch: %q != %q", dec, plain)
	}
}

func TestEmptyPassthrough(t *testing.T) {
	enc, err := Encrypt("")
	if err != nil || enc != "" {
		t.Fatalf("empty encrypt: %q %v", enc, err)
	}
	dec, err := Decrypt("")
	if err != nil || dec != "" {
		t.Fatalf("empty decrypt: %q %v", dec, err)
	}
}

func TestLegacyPlaintextPassthrough(t *testing.T) {
	// 旧版明文数据没有 enc: 前缀，应原样透传（用于迁移）
	dec, err := Decrypt("plain-old-password")
	if err != nil || dec != "plain-old-password" {
		t.Fatalf("legacy passthrough: %q %v", dec, err)
	}
	// 已加密的值再次 Encrypt 不应二次加密
	enc, _ := Encrypt("secret")
	enc2, err := Encrypt(enc)
	if err != nil || enc2 != enc {
		t.Fatalf("double encrypt not idempotent: %q %v", enc2, err)
	}
}

func TestDifferentValuesEncryptDifferently(t *testing.T) {
	a, _ := Encrypt("same")
	b, _ := Encrypt("same")
	if a == b {
		t.Fatal("same plaintext should produce different ciphertext (random nonce)")
	}
}
