// Package customcmd provides CRUD operations for user-defined terminal
// commands, persisted in SQLite through GORM.
package customcmd

import (
	"errors"

	"changeme/app/model"
	"changeme/app/service/db"
)

// CustomCommandService exposes custom-command CRUD to the frontend.
type CustomCommandService struct{}

// ServiceName implements application.ServiceName.
func (c *CustomCommandService) ServiceName() string { return "CustomCommandService" }

// List returns all custom commands, most recently updated first.
func (c *CustomCommandService) List() ([]model.CustomCommand, error) {
	var list []model.CustomCommand
	err := db.GetDB().Order("updated_at desc").Find(&list).Error
	return list, err
}

// Create persists a new custom command and returns it with its id.
func (c *CustomCommandService) Create(cmd model.CustomCommand) (model.CustomCommand, error) {
	if cmd.Name == "" {
		return cmd, errors.New("命令名称不能为空")
	}
	if cmd.Command == "" {
		return cmd, errors.New("命令内容不能为空")
	}
	if err := db.GetDB().Create(&cmd).Error; err != nil {
		return cmd, err
	}
	return cmd, nil
}

// Update persists changes to an existing custom command.
func (c *CustomCommandService) Update(cmd model.CustomCommand) (model.CustomCommand, error) {
	if cmd.ID == 0 {
		return cmd, errors.New("命令 ID 不能为空")
	}
	if cmd.Name == "" {
		return cmd, errors.New("命令名称不能为空")
	}
	if cmd.Command == "" {
		return cmd, errors.New("命令内容不能为空")
	}
	if err := db.GetDB().Save(&cmd).Error; err != nil {
		return cmd, err
	}
	return cmd, nil
}

// Delete removes a custom command by id.
func (c *CustomCommandService) Delete(id uint) error {
	return db.GetDB().Delete(&model.CustomCommand{}, id).Error
}
