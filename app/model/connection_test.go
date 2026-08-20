package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// 前端绑定把 time.Time 映射为 string，新建/编辑表单会发送空字符串，
// 反序列化必须容错（之前会报 "parsing time \"\"" 错误）。
func TestSavedConnectionUnmarshalEmptyTime(t *testing.T) {
	raw := `{"name":"测试","type":"ssh","host":"1.2.3.4","port":22,"username":"root","password":"pwd","useKey":false,"privateKey":"","passphrase":"","defaultDir":"","tls":false,"createdAt":"","updatedAt":""}`
	var c SavedConnection
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal with empty times failed: %v", err)
	}
	if c.Host != "1.2.3.4" || c.Username != "root" {
		t.Fatalf("fields not populated: %+v", c)
	}
	if !c.CreatedAt.IsZero() || !c.UpdatedAt.IsZero() {
		t.Fatalf("zero times expected, got %v / %v", c.CreatedAt, c.UpdatedAt)
	}
}

func TestSavedConnectionUnmarshalValidTime(t *testing.T) {
	raw := `{"name":"x","type":"ftp","host":"h","port":21,"username":"u","password":"","useKey":false,"privateKey":"","passphrase":"","defaultDir":"","tls":true,"createdAt":"2026-01-02T15:04:05Z","updatedAt":"2026-01-02T15:04:05Z"}`
	var c SavedConnection
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if c.CreatedAt.IsZero() || c.CreatedAt.Year() != 2026 {
		t.Fatalf("createdAt not parsed: %v", c.CreatedAt)
	}
	if !c.TLS {
		t.Fatal("tls not parsed")
	}
}

func TestSavedConnectionMarshalStillWorks(t *testing.T) {
	c := SavedConnection{Name: "x", CreatedAt: time.Now()}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var back SavedConnection
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("roundtrip failed: %v", err)
	}
	if back.Name != "x" {
		t.Fatalf("roundtrip name: %q", back.Name)
	}
}

// 模拟前端 Create/Update：createdAt/updatedAt 传空字符串，走完整 GORM
// 保存链路（含加密钩子），复现并验证线上报错的修复。
func TestSavedConnectionDBSaveWithEmptyTimes(t *testing.T) {	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := d.AutoMigrate(&SavedConnection{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	createRaw := `{"name":"测试机","type":"ssh","host":"10.0.0.1","port":22,"username":"root","password":"secret","useKey":false,"privateKey":"","passphrase":"","defaultDir":"/root","tls":false,"createdAt":"","updatedAt":""}`
	var c1 SavedConnection
	if err := json.Unmarshal([]byte(createRaw), &c1); err != nil {
		t.Fatalf("unmarshal create payload: %v", err)
	}
	if err := d.Create(&c1).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	var got1 SavedConnection
	if err := d.First(&got1, c1.ID).Error; err != nil {
		t.Fatalf("first: %v", err)
	}
	if got1.Name != "测试机" || got1.Password != "secret" || got1.CreatedAt.IsZero() {
		t.Fatalf("create roundtrip failed: %+v", got1)
	}

	updateRaw := `{"id":1,"name":"测试机-改","type":"ssh","host":"10.0.0.1","port":2222,"username":"root","password":"new-secret","useKey":false,"privateKey":"","passphrase":"","defaultDir":"/home","tls":false,"createdAt":"","updatedAt":""}`
	var c2 SavedConnection
	if err := json.Unmarshal([]byte(updateRaw), &c2); err != nil {
		t.Fatalf("unmarshal update payload: %v", err)
	}
	if err := d.Save(&c2).Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	var got2 SavedConnection
	if err := d.First(&got2, c2.ID).Error; err != nil {
		t.Fatalf("first after update: %v", err)
	}
	if got2.Name != "测试机-改" || got2.Port != 2222 || got2.Password != "new-secret" {
		t.Fatalf("update roundtrip failed: %+v", got2)
	}
	if got2.CreatedAt.IsZero() {
		t.Fatal("createdAt was zeroed by update")
	}
}
