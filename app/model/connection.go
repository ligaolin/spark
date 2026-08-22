package model

import (
	"encoding/json"
	"time"

	"changeme/app/service/secure"

	"gorm.io/gorm"
)

const TableNameConnection = "connections"

// SavedConnection stores a connection profile (SSH terminal / SFTP / FTP)
// so users can reconnect without retyping the details.
// Sensitive fields (Password / Passphrase / PrivateKey) are encrypted with a
// machine-bound AES key before being written to SQLite (see app/service/secure).
type SavedConnection struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	Name       string    `gorm:"column:name" json:"name"`
	Group      string    `gorm:"column:group_name" json:"group"` // 分组名（空表示未分组）
	Type       string    `gorm:"column:type" json:"type"`        // ssh | ftp
	Host       string    `gorm:"column:host" json:"host"`
	Port       int       `gorm:"column:port" json:"port"`
	Username   string    `gorm:"column:username" json:"username"`
	Password   string    `gorm:"column:password" json:"password"`
	UseKey       bool      `gorm:"column:use_key" json:"useKey"`
	PrivateKey   string    `gorm:"column:private_key" json:"privateKey"` // PEM content
	Passphrase   string    `gorm:"column:passphrase" json:"passphrase"`
	ForwardAgent bool      `gorm:"column:forward_agent" json:"forwardAgent"` // SSH agent forwarding
	DefaultDir   string    `gorm:"column:default_dir" json:"defaultDir"`
	TLS          bool      `gorm:"column:tls" json:"tls"` // FTP explicit TLS
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (*SavedConnection) TableName() string {
	return TableNameConnection
}

// UnmarshalJSON tolerates empty time strings ("") for CreatedAt/UpdatedAt
// sent by the frontend (the binding maps time.Time to string, and the create/
// edit forms do not carry the original timestamps).
func (c *SavedConnection) UnmarshalJSON(data []byte) error {
	type alias SavedConnection
	aux := &struct {
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
		*alias
	}{alias: (*alias)(c)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if aux.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, aux.CreatedAt); err == nil {
			c.CreatedAt = t
		}
	}
	if aux.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, aux.UpdatedAt); err == nil {
			c.UpdatedAt = t
		}
	}
	return nil
}

// BeforeUpdate preserves the original CreatedAt when the incoming value is
// zero (the frontend edit form does not carry the original timestamp).
func (c *SavedConnection) BeforeUpdate(tx *gorm.DB) error {
	if !c.CreatedAt.IsZero() {
		return nil
	}
	var old SavedConnection
	if err := tx.Select("created_at").First(&old, c.ID).Error; err == nil {
		c.CreatedAt = old.CreatedAt
	}
	return nil
}

// BeforeSave encrypts the sensitive fields before persisting.
func (c *SavedConnection) BeforeSave(*gorm.DB) error {
	var err error
	if c.Password, err = secure.Encrypt(c.Password); err != nil {
		return err
	}
	if c.Passphrase, err = secure.Encrypt(c.Passphrase); err != nil {
		return err
	}
	if c.PrivateKey, err = secure.Encrypt(c.PrivateKey); err != nil {
		return err
	}
	return nil
}

// AfterFind decrypts the sensitive fields when loading from the database.
// Decryption failures (e.g. key changed) are tolerated: the raw value is kept
// so reading the list never breaks; the user can fix the key afterwards.
func (c *SavedConnection) AfterFind(*gorm.DB) error {
	decryptField(&c.Password)
	decryptField(&c.Passphrase)
	decryptField(&c.PrivateKey)
	return nil
}

func decryptField(f *string) {
	dec, err := secure.Decrypt(*f)
	if err == nil {
		*f = dec
	}
}
