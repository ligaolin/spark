package model

import (
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// 前端创建/编辑自定义命令时 createdAt/updatedAt 为空字符串，必须能正常反序列化并入库。
func TestCustomCommandUnmarshalEmptyTime(t *testing.T) {
	raw := `{"id":0,"name":"看日志","command":"tail -n 100 /var/log/syslog","createdAt":"","updatedAt":""}`
	var c CustomCommand
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal with empty times failed: %v", err)
	}
	if c.Name != "看日志" || c.Command != "tail -n 100 /var/log/syslog" {
		t.Fatalf("fields not populated: %+v", c)
	}
	if !c.CreatedAt.IsZero() {
		t.Fatalf("zero createdAt expected, got %v", c.CreatedAt)
	}
}

func TestCustomCommandDBSaveWithEmptyTimes(t *testing.T) {
	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&CustomCommand{}); err != nil {
		t.Fatal(err)
	}

	// Create：空时间戳
	createRaw := `{"id":0,"name":"磁盘","command":"df -h","createdAt":"","updatedAt":""}`
	var c1 CustomCommand
	if err := json.Unmarshal([]byte(createRaw), &c1); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := d.Create(&c1).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got1 CustomCommand
	if err := d.First(&got1, c1.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got1.Name != "磁盘" || got1.CreatedAt.IsZero() {
		t.Fatalf("create roundtrip failed: %+v", got1)
	}

	// Update：空时间戳，CreatedAt 不应被清零
	updateRaw := `{"id":1,"name":"磁盘-改","command":"df -hT","createdAt":"","updatedAt":""}`
	var c2 CustomCommand
	if err := json.Unmarshal([]byte(updateRaw), &c2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := d.Save(&c2).Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	var got2 CustomCommand
	if err := d.First(&got2, c2.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got2.Name != "磁盘-改" || got2.CreatedAt.IsZero() {
		t.Fatalf("update roundtrip failed: %+v", got2)
	}
}
