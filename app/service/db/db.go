package db

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	oracle "github.com/godoes/gorm-oracle"
	"github.com/wailsapp/wails/v3/pkg/application"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

var (
	db *gorm.DB
	mu sync.RWMutex
)

func InitDB() (err error) {
	mu.Lock()
	defer mu.Unlock()
	path := dataSourcePath()
	// 确保数据目录存在（桌面端首次启动时 %APPDATA%\spark 可能还不存在）
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("创建数据目录失败: %w", err)
		}
	}
	migrateLegacyData(path)
	db, err = gorm.Open(sqlite.Open(path), &gorm.Config{})
	return err
}

// dataSourcePath returns the SQLite file path. On mobile (Android/iOS) the
// process working directory is "/" and is not writable, so the database must
// live in the app's private files directory.
//
// On desktop the database lives in the per-user app data directory
// (%APPDATA%\spark on Windows), same place as dbconfig.json / known_hosts.
// Older versions used a relative "gorm.db" resolved against the process
// working directory, which is why launching the app from Explorer (系统右键
// 菜单打开 / 开机启动) used to drop empty gorm.db files into whatever folder
// the app happened to start from — or failed to start at all at login when
// that directory was not writable.
func dataSourcePath() string {
	if dir := application.Mobile.StoragePath(); dir != "" {
		return filepath.Join(dir, "gorm.db")
	}
	var dir string
	if d, err := os.UserConfigDir(); err == nil {
		dir = d
	} else {
		home, _ := os.UserHomeDir()
		dir = home
	}
	return filepath.Join(dir, "spark", "gorm.db")
}

// migrateLegacyData moves an existing database left behind by older versions
// (which stored "gorm.db" next to the process) into the fixed data path above.
//
// Only files that really look like a SQLite database are migrated: the buggy
// right-click / login launches created 0-byte strays that must never be
// promoted over a real database. The executable directory is preferred over
// the current working directory (the latter varies with the launch method and
// may contain empty strays from the old behaviour). Migration is best-effort
// and never blocks startup.
func migrateLegacyData(target string) {
	if _, err := os.Stat(target); err == nil {
		return // 新位置已有数据库
	}

	var candidates []string
	add := func(p string) {
		if p == "" {
			return
		}
		for _, c := range candidates {
			if strings.EqualFold(c, p) {
				return
			}
		}
		candidates = append(candidates, p)
	}
	if exe, err := os.Executable(); err == nil {
		add(filepath.Join(filepath.Dir(exe), "gorm.db"))
	}
	if wd, err := os.Getwd(); err == nil {
		add(filepath.Join(wd, "gorm.db"))
	}

	for _, cand := range candidates {
		if !looksLikeSQLite(cand) {
			continue
		}
		if err := os.Rename(cand, target); err != nil {
			// 跨分区 / 权限等原因导致 rename 失败时退化为拷贝
			if err = copyFile(cand, target); err != nil {
				log.Printf("迁移旧数据库 %s 失败: %v", cand, err)
				continue
			}
		}
		log.Printf("已将旧的本地数据库从 %s 迁移到 %s", cand, target)
		return
	}
}

// looksLikeSQLite reports whether p exists, is non-empty and starts with the
// SQLite file header, i.e. it is a real database rather than one of the
// 0-byte files left behind by buggy launches.
func looksLikeSQLite(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.Size() == 0 {
		return false
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 16)
	if _, err := io.ReadFull(f, head); err != nil {
		return false
	}
	return string(head) == "SQLite format 3\x00"
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// Reconnect switches the global database to the given dialect + DSN
// (used when the user configures a remote database).
func Reconnect(dialect, dsn string) error {
	ndb, err := Open(dialect, dsn)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if db != nil {
		if old, err := db.DB(); err == nil {
			_ = old.Close()
		}
	}
	db = ndb
	return nil
}

// Open opens a standalone database handle without touching the global one
// (used for connection tests and data migration). The connection attempt is
// bounded by a timeout so an unreachable host cannot hang the application.
func Open(dialect, dsn string) (*gorm.DB, error) {
	dialector := dialectorFor(dialect, dsn)
	if dialector == nil {
		return nil, fmt.Errorf("不支持的数据库类型: %s", dialect)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan struct{})
	var gdb *gorm.DB
	var err error
	go func() {
		defer close(done)
		gdb, err = gorm.Open(dialector, &gorm.Config{})
	}()
	select {
	case <-done:
		return gdb, err
	case <-ctx.Done():
		return nil, fmt.Errorf("连接数据库超时（%s）", dialect)
	}
}

func dialectorFor(dialect, dsn string) gorm.Dialector {
	switch dialect {
	case "mysql":
		return mysql.Open(dsn)
	case "postgres":
		return postgres.Open(dsn)
	case "sqlserver":
		return sqlserver.Open(dsn)
	case "oracle":
		return oracle.New(oracle.Config{
			DSN:                       dsn,
			VarcharSizeIsCharLength:   true,
			RowNumberAliasForOracle11: "ROW_NUM",
		})
	default:
		return sqlite.Open(dsn)
	}
}

func GetDB() *gorm.DB {
	mu.RLock()
	defer mu.RUnlock()
	return db
}
