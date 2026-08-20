package model

import "changeme/app/service/db"

func Migrate() {
	d := db.GetDB()
	d.AutoMigrate(&SavedConnection{}, &CustomCommand{}, &Favorite{}, &Setting{}, &ConnectionGroup{}, &DocNode{})

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
