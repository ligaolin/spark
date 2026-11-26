// Package settings provides a generic key-value configuration store
// (persisted in SQLite) for application settings and keyboard shortcuts.
package settings

import (
	"errors"
	"strconv"
	"strings"
	"sync"

	"spark/app/model"
	"spark/app/service/db"
	"spark/app/service/version"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 设置读取非常频繁（每次心跳、自动刷新、AI 调用都会读若干键），逐次查库会
// 把 SQLite 连接打成瓶颈。这里做一次全量缓存：首次访问时读入内存，Set 时
// 同步更新，切换数据库后调用 Reload 失效重载。
var (
	cacheMu     sync.RWMutex
	cache       map[string]string
	cacheLoaded bool
)

// SettingsService exposes the configuration store to the frontend.
type SettingsService struct{}

// ServiceName implements application.ServiceName.
func (s *SettingsService) ServiceName() string { return "SettingsService" }

// GetVersion returns the application version (injected at build time).
func (s *SettingsService) GetVersion() string { return version.Version }

// GetAll returns every stored setting as a key-value map.
func (s *SettingsService) GetAll() (map[string]string, error) {
	cacheMu.RLock()
	loaded := cacheLoaded
	cacheMu.RUnlock()
	if !loaded {
		cacheMu.Lock()
		if !cacheLoaded {
			if err := loadLocked(); err != nil {
				cacheMu.Unlock()
				return nil, err
			}
		}
		cacheMu.Unlock()
	}

	cacheMu.RLock()
	defer cacheMu.RUnlock()
	out := make(map[string]string, len(cache))
	for k, v := range cache {
		out[k] = v
	}
	return out, nil
}

// loadLocked reads all settings into the cache. Caller must hold cacheMu.
func loadLocked() error {
	var list []model.Setting
	if err := db.GetDB().Find(&list).Error; err != nil {
		return err
	}
	m := make(map[string]string, len(list))
	for _, it := range list {
		m[it.Key] = it.Value
	}
	cache = m
	cacheLoaded = true
	return nil
}

// Reload drops the cache so the next read re-loads from the (possibly switched)
// database. Call after reconnecting to a different storage backend.
func Reload() {
	cacheMu.Lock()
	cache = nil
	cacheLoaded = false
	cacheMu.Unlock()
}

// Set stores (or updates) a setting value.
func (s *SettingsService) Set(key, value string) error {
	return Set(key, value)
}

// Set stores (or updates) a setting value (package-level helper).
func Set(key, value string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("配置键不能为空")
	}
	if err := db.GetDB().Save(&model.Setting{Key: key, Value: value}).Error; err != nil {
		return err
	}
	cacheMu.Lock()
	if cacheLoaded {
		cache[key] = value
	}
	cacheMu.Unlock()
	return nil
}

// GetString reads a setting, returning def when absent.
func GetString(key, def string) string {
	cacheMu.RLock()
	if cacheLoaded {
		v, ok := cache[key]
		cacheMu.RUnlock()
		if ok {
			return v
		}
		return def
	}
	cacheMu.RUnlock()

	cacheMu.Lock()
	defer cacheMu.Unlock()
	if !cacheLoaded {
		if err := loadLocked(); err != nil {
			return def
		}
	}
	if v, ok := cache[key]; ok {
		return v
	}
	return def
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

// GetFloat reads a float setting, returning def when absent or invalid.
func GetFloat(key string, def float64) float64 {
	v := GetString(key, "")
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return def
	}
	return f
}

// KeyAutoStartEnabled persists the user's 开机启动 preference in the settings
// table. main() re-registers autostart on every startup while it is "1", which
// keeps the Windows Run key pointing at the current executable across updates.
const KeyAutoStartEnabled = "autostart.enabled"

// SetAutoStart enables or disables launching the app at login/boot.
// 复用 Wails v3 内置的 AutostartManager（Windows 注册表 Run 键 / macOS
// SMAppService 或 LaunchAgent / Linux autostart）。操作成功后把开关状态
// 持久化到设置表，供下次启动恢复（见 main.go）。
func (s *SettingsService) SetAutoStart(enabled bool) error {
	am := application.Get().Autostart
	var err error
	if enabled {
		err = am.Enable()
	} else {
		err = am.Disable()
	}
	if err != nil {
		return err
	}
	v := "0"
	if enabled {
		v = "1"
	}
	return s.Set(KeyAutoStartEnabled, v)
}

// IsAutoStart reports whether the app is registered to start at login/boot.
func (s *SettingsService) IsAutoStart() bool {
	enabled, err := application.Get().Autostart.IsEnabled()
	if err != nil {
		return false
	}
	return enabled
}

// SetTitleBarTheme 运行时切换 Windows 标题栏明暗。theme 为 "light" 或 "dark"。
func (s *SettingsService) SetTitleBarTheme(theme string) {
	setTitleBarTheme(theme)
}
