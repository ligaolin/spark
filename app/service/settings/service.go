// Package settings provides a generic key-value configuration store
// (persisted in SQLite) for application settings and keyboard shortcuts.
package settings

import (
	"errors"
	"strconv"
	"strings"

	"changeme/app/model"
	"changeme/app/service/db"
	"changeme/app/service/version"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// SettingsService exposes the configuration store to the frontend.
type SettingsService struct{}

// ServiceName implements application.ServiceName.
func (s *SettingsService) ServiceName() string { return "SettingsService" }

// GetVersion returns the application version (injected at build time).
func (s *SettingsService) GetVersion() string { return version.Version }

// GetAll returns every stored setting as a key-value map.
func (s *SettingsService) GetAll() (map[string]string, error) {
	var list []model.Setting
	if err := db.GetDB().Find(&list).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(list))
	for _, it := range list {
		out[it.Key] = it.Value
	}
	return out, nil
}

// Set stores (or updates) a setting value.
func (s *SettingsService) Set(key, value string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("配置键不能为空")
	}
	return db.GetDB().Save(&model.Setting{Key: key, Value: value}).Error
}

// GetString reads a setting, returning def when absent.
func GetString(key, def string) string {
	var it model.Setting
	// 用 Find（而非 First）避免不存在的键每次都打 "record not found" 日志
	if err := db.GetDB().Model(&model.Setting{}).Where("key = ?", key).Limit(1).Find(&it).Error; err != nil {
		return def
	}
	return it.Value
}

// GetInt reads an integer setting, returning def when absent or invalid.
func GetInt(key string, def int) int {
	v := GetString(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}

// SetAutoStart enables or disables launching the app at login/boot.
// 复用 Wails v3 内置的 AutostartManager（Windows 注册表 Run 键 / macOS
// SMAppService 或 LaunchAgent / Linux autostart）。
func (s *SettingsService) SetAutoStart(enabled bool) error {
	am := application.Get().Autostart
	if enabled {
		return am.Enable()
	}
	return am.Disable()
}

// IsAutoStart reports whether the app is registered to start at login/boot.
func (s *SettingsService) IsAutoStart() bool {
	enabled, err := application.Get().Autostart.IsEnabled()
	if err != nil {
		return false
	}
	return enabled
}
