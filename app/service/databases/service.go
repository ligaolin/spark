// Package databases lets the user choose where the application data lives:
// the default local SQLite file or a remote database (MySQL / PostgreSQL /
// SQL Server). All data — saved connections, favorites, custom commands and
// settings (including shortcuts) — is migrated to the selected database, so
// multiple machines sharing the same remote database stay in sync.
package databases

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"changeme/app/model"
	"changeme/app/service/db"
	"changeme/app/service/secure"

	"github.com/wailsapp/wails/v3/pkg/application"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DatabaseConfig describes a storage backend.
type DatabaseConfig struct {
	Dialect  string `json:"dialect"` // sqlite | mysql | postgres | sqlserver
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
	Params   string `json:"params"`  // 附加连接参数（如 sslmode=require）
	SyncKey  string `json:"syncKey"` // 同步密钥：多机共用同一密钥时，凭据可跨机解密
}

// DSN builds the database connection string for the configured dialect.
func (c DatabaseConfig) DSN() string {
	switch c.Dialect {
	case "mysql":
		port := c.Port
		if port == 0 {
			port = 3306
		}
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			c.Username, c.Password, c.Host, port, c.Database)
		if c.Params != "" {
			dsn += "&" + strings.TrimPrefix(c.Params, "&")
		}
		return dsn
	case "postgres":
		port := c.Port
		if port == 0 {
			port = 5432
		}
		parts := []string{
			fmt.Sprintf("host=%s", c.Host),
			fmt.Sprintf("port=%d", port),
		}
		if c.Username != "" {
			parts = append(parts, "user="+c.Username)
		}
		if c.Password != "" {
			parts = append(parts, "password="+c.Password)
		}
		if c.Database != "" {
			parts = append(parts, "dbname="+c.Database)
		}
		parts = append(parts, "sslmode=disable")
		if c.Params != "" {
			parts = append(parts, c.Params)
		}
		return strings.Join(parts, " ")
	case "sqlserver":
		port := c.Port
		if port == 0 {
			port = 1433
		}
		dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s",
			c.Username, c.Password, c.Host, port, c.Database)
		if c.Params != "" {
			dsn += "&" + strings.TrimPrefix(c.Params, "&")
		}
		return dsn
	case "oracle":
		port := c.Port
		if port == 0 {
			port = 1521
		}
		// 纯 Go Oracle 驱动（go-ora）：Database 字段填服务名（SERVICE_NAME）
		dsn := fmt.Sprintf("oracle://%s:%s@%s:%d/%s",
			c.Username, c.Password, c.Host, port, c.Database)
		if c.Params != "" {
			dsn += "?" + strings.TrimPrefix(c.Params, "?")
		}
		return dsn
	default:
		return "gorm.db"
	}
}

func configPath() string {
	// On mobile the OS user-config/home dirs (e.g. /sdcard) are not writable;
	// use the app's private files directory instead (mirrors db.dataSourcePath).
	var dir string
	if mobile := application.Mobile.StoragePath(); mobile != "" {
		dir = mobile
	} else if d, err := os.UserConfigDir(); err == nil {
		dir = d
	} else {
		home, _ := os.UserHomeDir()
		dir = home
	}
	return filepath.Join(dir, "spark", "dbconfig.json")
}

// Load reads the saved database config from the local file.
func Load() (DatabaseConfig, bool) {
	return loadFrom(configPath())
}

func loadFrom(path string) (DatabaseConfig, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DatabaseConfig{}, false
	}
	var cfg DatabaseConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DatabaseConfig{}, false
	}
	return cfg, true
}

func (c DatabaseConfig) Save() error {
	return saveTo(configPath(), c)
}

func saveTo(path string, cfg DatabaseConfig) error {
	if cfg.Dialect == "" {
		cfg.Dialect = "sqlite"
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// DatabaseService exposes storage configuration to the frontend.
type DatabaseService struct{}

// ServiceName implements application.ServiceName.
func (s *DatabaseService) ServiceName() string { return "DatabaseService" }

// GetCurrent returns the current storage config.
func (s *DatabaseService) GetCurrent() DatabaseConfig {
	if cfg, ok := Load(); ok {
		return cfg
	}
	return DatabaseConfig{Dialect: "sqlite"}
}

// Test verifies that a connection to the given config can be established.
func (s *DatabaseService) Test(cfg DatabaseConfig) error {
	if cfg.Dialect == "sqlite" || cfg.Dialect == "" {
		return nil // 本地文件始终可用
	}
	if cfg.Host == "" {
		return errors.New("主机地址不能为空")
	}
	gdb, err := db.Open(cfg.Dialect, cfg.DSN())
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	return nil
}

// KeySeedFor 返回该存储配置对应的凭据加密密钥种子：
//   - 显式填写了同步密钥 → 用它（可选的额外加固）
//   - 远程数据库 → 由数据库连接信息（DSN）派生：能连接同一数据库的机器自动获得同一密钥，
//     无需任何额外设置（能连库即视为已授权）
//   - 本地 SQLite → 空种子 = 本机绑定密钥
func KeySeedFor(cfg DatabaseConfig) string {
	if cfg.SyncKey != "" {
		return cfg.SyncKey
	}
	if cfg.Dialect == "" || cfg.Dialect == "sqlite" {
		return ""
	}
	return "db:" + cfg.DSN()
}

// Switch makes the given database the active storage backend.
// - 目标库为空：把当前数据迁移过去（凭据按新同步密钥重新加密），然后切换
// - 目标库已有数据（例如多机同步时第二台机器加入）：只切换连接，不覆盖目标数据
// 配置保存到本机文件（%APPDATA%\spark\dbconfig.json），重启后自动使用，无需重新设置。
func (s *DatabaseService) Switch(cfg DatabaseConfig) error {
	if cfg.Dialect == "" {
		cfg.Dialect = "sqlite"
	}
	oldSeed := secure.CurrentKeySeed()

	// 1. 连接目标库
	target, err := db.Open(cfg.Dialect, cfg.DSN())
	if err != nil {
		return fmt.Errorf("连接目标数据库失败: %w", err)
	}
	targetSQL, err := target.DB()
	if err != nil {
		return err
	}
	defer targetSQL.Close()
	if err := targetSQL.Ping(); err != nil {
		return fmt.Errorf("连接目标数据库失败: %w", err)
	}

	// 2. 建表
	if err := target.AutoMigrate(
		&model.SavedConnection{}, &model.CustomCommand{}, &model.Favorite{}, &model.Setting{},
	); err != nil {
		return fmt.Errorf("初始化目标数据库失败: %w", err)
	}

	// 3. 目标库为空才迁移数据（源库解密 → 目标库按新密钥重新加密）
	if !targetHasData(target) {
		if err := migrateData(db.GetDB(), target, KeySeedFor(cfg)); err != nil {
			secure.SetKeySeed(oldSeed)
			return fmt.Errorf("数据迁移失败: %w", err)
		}
	}

	// 4. 重置全局数据库连接，运行期立即生效
	if err := db.Reconnect(cfg.Dialect, cfg.DSN()); err != nil {
		secure.SetKeySeed(oldSeed)
		return err
	}
	secure.SetKeySeed(KeySeedFor(cfg))

	// 5. 本地永久保存配置
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("保存数据库配置失败: %w", err)
	}
	return nil
}

// targetHasData reports whether the target database already contains any
// application data (used to avoid overwriting a shared remote database).
func targetHasData(gdb *gorm.DB) bool {
	silent := gdb.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	var n int64
	for _, m := range []interface{}{
		&model.SavedConnection{}, &model.CustomCommand{}, &model.Favorite{}, &model.Setting{},
	} {
		if err := silent.Model(m).Count(&n).Error; err == nil && n > 0 {
			return true
		}
	}
	return false
}

// migrateData copies every model from src to dst inside one transaction.
// Credentials are read with the CURRENT (source) key, then the encryption key
// is switched to targetKey before writing, so they are re-encrypted with the
// target key.
func migrateData(src, dst *gorm.DB, targetKey string) error {
	// 1. 先用当前（源）密钥读取
	var conns []model.SavedConnection
	if err := src.Find(&conns).Error; err != nil {
		return err
	}
	var cmds []model.CustomCommand
	if err := src.Find(&cmds).Error; err != nil {
		return err
	}
	var favs []model.Favorite
	if err := src.Find(&favs).Error; err != nil {
		return err
	}
	var sets []model.Setting
	if err := src.Find(&sets).Error; err != nil {
		return err
	}

	// 2. 切换到目标密钥，随后写入即按新密钥加密
	secure.SetKeySeed(targetKey)

	// 3. 事务写入
	return dst.Transaction(func(tx *gorm.DB) error {
		if len(conns) > 0 {
			if err := tx.Create(&conns).Error; err != nil {
				return fmt.Errorf("迁移连接失败: %w", err)
			}
		}
		if len(cmds) > 0 {
			if err := tx.Create(&cmds).Error; err != nil {
				return fmt.Errorf("迁移命令失败: %w", err)
			}
		}
		if len(favs) > 0 {
			if err := tx.Create(&favs).Error; err != nil {
				return fmt.Errorf("迁移收藏失败: %w", err)
			}
		}
		if len(sets) > 0 {
			if err := tx.Create(&sets).Error; err != nil {
				return fmt.Errorf("迁移配置失败: %w", err)
			}
		}
		return nil
	})
}
