package model

import (
	"changeme/app/service/db"

	"gorm.io/gorm"
)

// migrateSiteAccountsToLink backfills link_id from the legacy site_id column
// (an intermediate schema put accounts under sites). Since a site can hold
// multiple links, accounts are best-effort attached to the site's first link;
// accounts whose site has no link are left unlinked (user can re-add).
func migrateSiteAccountsToLink(d *gorm.DB) {
	if !d.Migrator().HasColumn(&SiteAccount{}, "site_id") {
		return
	}

	type legacyAccount struct {
		ID     uint `gorm:"column:id"`
		LinkID uint `gorm:"column:link_id"`
		SiteID uint `gorm:"column:site_id"`
	}
	var rows []legacyAccount
	if err := d.Table(TableNameSiteAccount).Select("id, link_id, site_id").Find(&rows).Error; err != nil {
		return
	}

	var links []SiteLink
	if err := d.Order("sort asc, id asc").Find(&links).Error; err != nil {
		return
	}
	firstLink := make(map[uint]uint, len(links))
	for _, l := range links {
		if _, ok := firstLink[l.SiteID]; !ok {
			firstLink[l.SiteID] = l.ID
		}
	}

	for _, r := range rows {
		if r.LinkID == 0 && r.SiteID != 0 {
			if lid, ok := firstLink[r.SiteID]; ok {
				_ = d.Table(TableNameSiteAccount).Where("id = ?", r.ID).Update("link_id", lid).Error
			}
		}
	}
	_ = d.Migrator().DropColumn(&SiteAccount{}, "site_id")
}

func Migrate() {
	d := db.GetDB()
	d.AutoMigrate(&SavedConnection{}, &CustomCommand{}, &Favorite{}, &Setting{}, &ConnectionGroup{}, &DocNode{}, &SiteFolder{}, &Site{}, &SiteLink{}, &SiteAccount{})

	// 迁移：账号挂在链接下（link_id）。
	migrateSiteAccountsToLink(d)

	// 迁移旧版明文凭据：读取时 AfterFind 会原样透传非 enc: 前缀的值，
	// 这里重新 Save 一遍触发 BeforeSave 加密。
	var conns []SavedConnection
	if err := d.Find(&conns).Error; err != nil {
		return
	}
	for i := range conns {
		c := &conns[i]
		if c.Password != "" || c.Passphrase != "" || c.PrivateKey != "" {
			_ = d.Save(c).Error
		}
	}
}
