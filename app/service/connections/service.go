// Package connections provides CRUD operations for saved connections,
// persisted in SQLite through GORM.
package connections

import (
	"errors"
	"strings"

	"changeme/app/model"
	"changeme/app/service/db"

	"gorm.io/gorm"
)

// ConnService exposes saved-connection CRUD to the frontend.
type ConnService struct{}

// ServiceName implements application.ServiceName.
func (c *ConnService) ServiceName() string { return "ConnService" }

// List returns all saved connections, most recently updated first.
func (c *ConnService) List() ([]model.SavedConnection, error) {
	var list []model.SavedConnection
	err := db.GetDB().Order("updated_at desc").Find(&list).Error
	return list, err
}

// Create persists a new saved connection and returns it with its id.
func (c *ConnService) Create(conn model.SavedConnection) (model.SavedConnection, error) {
	if conn.Name == "" {
		return conn, errors.New("连接名称不能为空")
	}
	if conn.Host == "" {
		return conn, errors.New("主机地址不能为空")
	}
	if err := ensureGroup(conn.Group); err != nil {
		return conn, err
	}
	if err := db.GetDB().Create(&conn).Error; err != nil {
		return conn, err
	}
	return conn, nil
}

// Update persists changes to an existing saved connection.
func (c *ConnService) Update(conn model.SavedConnection) (model.SavedConnection, error) {
	if conn.ID == 0 {
		return conn, errors.New("连接 ID 不能为空")
	}
	if conn.Name == "" {
		return conn, errors.New("连接名称不能为空")
	}
	if conn.Host == "" {
		return conn, errors.New("主机地址不能为空")
	}
	if err := ensureGroup(conn.Group); err != nil {
		return conn, err
	}
	// CreatedAt 若为零值，由模型 BeforeUpdate 钩子保留数据库原值
	if err := db.GetDB().Save(&conn).Error; err != nil {
		return conn, err
	}
	return conn, nil
}

// Delete removes a saved connection by id.
func (c *ConnService) Delete(id uint) error {
	return db.GetDB().Delete(&model.SavedConnection{}, id).Error
}

// Get returns a saved connection by id.
func (c *ConnService) Get(id uint) (*model.SavedConnection, error) {
	var conn model.SavedConnection
	if err := db.GetDB().First(&conn, id).Error; err != nil {
		return nil, err
	}
	return &conn, nil
}

// SetGroup reassigns a connection to a group without touching its (encrypted)
// credential fields. An empty group means "未分组".
func (c *ConnService) SetGroup(id uint, group string) error {
	if id == 0 {
		return errors.New("连接 ID 不能为空")
	}
	group = strings.TrimSpace(group)
	if group != "" {
		if err := ensureGroup(group); err != nil {
			return err
		}
	}
	return db.GetDB().Model(&model.SavedConnection{}).
		Where("id = ?", id).
		Update("group_name", group).Error
}

// ListGroups returns all connection groups in sidebar order (creation order).
func (c *ConnService) ListGroups() ([]model.ConnectionGroup, error) {
	var groups []model.ConnectionGroup
	err := db.GetDB().Order("sort asc, name asc").Find(&groups).Error
	return groups, err
}

// CreateGroup creates a new empty group.
func (c *ConnService) CreateGroup(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("分组名称不能为空")
	}
	return ensureGroup(name)
}

// RenameGroup renames a group and reassigns all its connections.
func (c *ConnService) RenameGroup(oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return errors.New("分组名称不能为空")
	}
	if oldName == newName {
		return nil
	}
	var existing int64
	if err := db.GetDB().Model(&model.ConnectionGroup{}).
		Where("name = ? AND name <> ?", newName, oldName).
		Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return errors.New("已存在同名分组")
	}
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ConnectionGroup{}).
			Where("name = ?", oldName).
			Update("name", newName).Error; err != nil {
			return err
		}
		return tx.Model(&model.SavedConnection{}).
			Where("group_name = ?", oldName).
			Update("group_name", newName).Error
	})
}

// DeleteGroup removes a group and moves its connections back to "未分组".
func (c *ConnService) DeleteGroup(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("name = ?", name).
			Delete(&model.ConnectionGroup{}).Error; err != nil {
			return err
		}
		return tx.Model(&model.SavedConnection{}).
			Where("group_name = ?", name).
			Update("group_name", "").Error
	})
}

// ensureGroup creates the group if it does not exist yet (so a connection can
// reference a group created inline from the connection form).
func ensureGroup(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	var count int64
	if err := db.GetDB().Model(&model.ConnectionGroup{}).
		Where("name = ?", name).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	var total int64
	if err := db.GetDB().Model(&model.ConnectionGroup{}).
		Count(&total).Error; err != nil {
		return err
	}
	return db.GetDB().Create(&model.ConnectionGroup{
		Name: name,
		Sort: int(total) + 1,
	}).Error
}
