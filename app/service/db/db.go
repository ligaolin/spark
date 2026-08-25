package db

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	oracle "github.com/godoes/gorm-oracle"
	"github.com/glebarez/sqlite"
	"github.com/wailsapp/wails/v3/pkg/application"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

var (
	db  *gorm.DB
	mu  sync.RWMutex
)

func InitDB() (err error) {
	mu.Lock()
	defer mu.Unlock()
	db, err = gorm.Open(sqlite.Open(dataSourcePath()), &gorm.Config{})
	return err
}

// dataSourcePath returns the SQLite file path. On mobile (Android/iOS) the
// process working directory is "/" and is not writable, so the database must
// live in the app's private files directory; elsewhere it keeps the historical
// relative "gorm.db" in the current directory.
func dataSourcePath() string {
	if dir := application.Mobile.StoragePath(); dir != "" {
		return filepath.Join(dir, "gorm.db")
	}
	return "gorm.db"
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
