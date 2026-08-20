package model

import (
	"time"

	"changeme/app/service/secure"

	"gorm.io/gorm"
)

const (
	TableNameSiteFolder  = "site_folders"
	TableNameSite        = "sites"
	TableNameSiteLink    = "site_links"
	TableNameSiteAccount = "site_accounts"
)

// SiteFolder is a folder node in the site tree (multi-level, like documents).
type SiteFolder struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ParentID  uint      `gorm:"column:parent_id;index" json:"parentId"` // 0 = root
	Name      string    `gorm:"column:name" json:"name"`
	Sort      int       `gorm:"column:sort" json:"sort"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*SiteFolder) TableName() string { return TableNameSiteFolder }

// Site is one website group (站点), sitting in a folder (or root).
type Site struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	FolderID  uint      `gorm:"column:folder_id;index" json:"folderId"` // 0 = root
	Name      string    `gorm:"column:name" json:"name"`
	Note      string    `gorm:"column:note" json:"note"`
	Sort      int       `gorm:"column:sort" json:"sort"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*Site) TableName() string { return TableNameSite }

// SiteLink is one URL belonging to a site (链接).
type SiteLink struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	SiteID    uint      `gorm:"column:site_id;index" json:"siteId"`
	Name      string    `gorm:"column:name" json:"name"`
	URL       string    `gorm:"column:url" json:"url"`
	Note      string    `gorm:"column:note" json:"note"`
	Sort      int       `gorm:"column:sort" json:"sort"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*SiteLink) TableName() string { return TableNameSiteLink }

// SiteAccount is a username/password pair belonging to a link (账号). The
// password is encrypted at rest (AES-256-GCM, same key as connections).
type SiteAccount struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	LinkID    uint      `gorm:"column:link_id;index" json:"linkId"`
	Username  string    `gorm:"column:username" json:"username"`
	Password  string    `gorm:"column:password" json:"password"`
	Note      string    `gorm:"column:note" json:"note"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*SiteAccount) TableName() string { return TableNameSiteAccount }

// BeforeSave encrypts the password before persisting.
func (a *SiteAccount) BeforeSave(*gorm.DB) error {
	var err error
	if a.Password, err = secure.Encrypt(a.Password); err != nil {
		return err
	}
	return nil
}

// AfterFind decrypts the password when loading. Decryption failures are
// tolerated (raw value kept) so listing never breaks.
func (a *SiteAccount) AfterFind(*gorm.DB) error {
	decryptField(&a.Password)
	return nil
}
