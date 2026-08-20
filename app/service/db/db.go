package db

import (
	"context"
	"fmt"
	"time"

	oracle "github.com/godoes/gorm-oracle"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

var db *gorm.DB

func InitDB() (err error) {
	db, err = gorm.Open(sqlite.Open("gorm.db"), &gorm.Config{})
	return err
}

// Reconnect switches the global database to the given dialect + DSN
// (used when the user configures a remote database).
func Reconnect(dialect, dsn string) error {
	ndb, err := Open(dialect, dsn)
	if err != nil {
		return err
	}
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
	return db
}
