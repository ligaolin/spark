module changeme

go 1.26.4

require (
	github.com/aymanbagabas/go-pty v0.2.3
	github.com/glebarez/sqlite v1.11.0
	github.com/jlaffaye/ftp v0.2.2
	github.com/pkg/sftp v1.13.11
	github.com/wailsapp/wails/v3 v3.0.0-beta.4
	golang.org/x/crypto v0.55.0
	golang.org/x/sys v0.47.0
	gorm.io/gorm v1.31.2
)

// 本地补丁：部分 FTP 服务器对数据命令回复 200 而非标准 125/150/226，
// 上游 jlaffaye/ftp 会报错（如打开文件失败: 200 "Ready to proceed"）。
replace github.com/jlaffaye/ftp => ./third_party/jlaffaye/ftp

require (
	github.com/creack/pty v1.1.24 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/sijms/go-ora/v2 v2.9.0 // indirect
	github.com/u-root/u-root v0.16.0 // indirect
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/glebarez/go-sqlite v1.21.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-sql-driver/mysql v1.8.1 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/godoes/gorm-oracle v1.6.18
	github.com/golang-sql/civil v0.0.0-20220223132316-b832511892a9 // indirect
	github.com/golang-sql/sqlexp v0.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.10.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/microsoft/go-mssqldb v1.9.6 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	golang.org/x/exp v0.0.0-20260410095643-746e56fc9e2f // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	gorm.io/driver/mysql v1.6.0
	gorm.io/driver/postgres v1.6.2
	gorm.io/driver/sqlserver v1.6.4
	modernc.org/libc v1.67.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.44.3 // indirect
)
