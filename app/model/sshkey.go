package model

import (
	"time"

	"spark/app/service/secure"

	"gorm.io/gorm"
)

const TableNameSSHKey = "ssh_keys"

// SSHKey stores an app-managed SSH key pair. The private key is encrypted with
// the machine-bound AES key before being persisted (see app/service/secure).
type SSHKey struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"column:name" json:"name"`
	Type        string    `gorm:"column:type" json:"type"` // ed25519 | rsa
	Comment     string    `gorm:"column:comment" json:"comment"`
	PublicKey   string    `gorm:"column:public_key" json:"publicKey"`   // authorized_keys 单行格式（含备注）
	PrivateKey  string    `gorm:"column:private_key" json:"privateKey"` // PEM，加密存储
	Passphrase  string    `gorm:"column:passphrase" json:"passphrase"`  // 私钥口令（可空），加密存储
	Fingerprint string    `gorm:"column:fingerprint" json:"fingerprint"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (*SSHKey) TableName() string {
	return TableNameSSHKey
}

// BeforeSave encrypts the private key and passphrase before persisting.
func (k *SSHKey) BeforeSave(*gorm.DB) error {
	enc, err := secure.Encrypt(k.PrivateKey)
	if err != nil {
		return err
	}
	k.PrivateKey = enc
	encP, err := secure.Encrypt(k.Passphrase)
	if err != nil {
		return err
	}
	k.Passphrase = encP
	return nil
}

// AfterFind decrypts the private key and passphrase when loading from the
// database. Decryption failures are tolerated: the raw value stays so listing
// keys never breaks.
func (k *SSHKey) AfterFind(*gorm.DB) error {
	if dec, err := secure.Decrypt(k.PrivateKey); err == nil {
		k.PrivateKey = dec
	}
	if dec, err := secure.Decrypt(k.Passphrase); err == nil {
		k.Passphrase = dec
	}
	return nil
}