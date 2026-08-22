package databases

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"changeme/app/model"
	"changeme/app/service/db"
	"changeme/app/service/secure"

	"gorm.io/gorm"
)

func TestKeySeedFor(t *testing.T) {
	// 本地 SQLite：空种子 = 本机密钥
	if s := KeySeedFor(DatabaseConfig{Dialect: "sqlite"}); s != "" {
		t.Fatalf("sqlite seed: %q", s)
	}
	// 远程库：由 DSN 派生，同一配置派生一致
	a := DatabaseConfig{Dialect: "mysql", Host: "h", Port: 3306, Username: "u", Password: "p", Database: "d"}
	b := DatabaseConfig{Dialect: "mysql", Host: "h", Port: 3306, Username: "u", Password: "p", Database: "d"}
	sa, sb := KeySeedFor(a), KeySeedFor(b)
	if sa != sb || sa == "" {
		t.Fatalf("remote seed should be stable and non-empty: %q %q", sa, sb)
	}
	if !strings.HasPrefix(sa, "db:") {
		t.Fatalf("remote seed prefix: %q", sa)
	}
	// 不同密码 → 不同密钥
	c := DatabaseConfig{Dialect: "mysql", Host: "h", Port: 3306, Username: "u", Password: "other", Database: "d"}
	if KeySeedFor(c) == sa {
		t.Fatal("different db password should give different seed")
	}
	// 显式同步密钥优先
	d := DatabaseConfig{Dialect: "mysql", Host: "h", Username: "u", Password: "p", Database: "d", SyncKey: "explicit"}
	if KeySeedFor(d) != "explicit" {
		t.Fatalf("sync key override: %q", KeySeedFor(d))
	}
}

func TestDSN(t *testing.T) {
	cases := []struct {
		cfg  DatabaseConfig
		want string
	}{
		{DatabaseConfig{Dialect: "mysql", Host: "h", Port: 3306, Username: "u", Password: "p", Database: "d"},
			"u:p@tcp(h:3306)/d?charset=utf8mb4&parseTime=True&loc=Local"},
		{DatabaseConfig{Dialect: "postgres", Host: "h", Port: 5432, Username: "u", Password: "p", Database: "d"},
			"host=h port=5432 user=u password=p dbname=d sslmode=disable"},
		{DatabaseConfig{Dialect: "sqlserver", Host: "h", Port: 1433, Username: "u", Password: "p", Database: "d"},
			"sqlserver://u:p@h:1433?database=d"},
		{DatabaseConfig{Dialect: "oracle", Host: "h", Port: 1521, Username: "u", Password: "p", Database: "XE"},
			"oracle://u:p@h:1521/XE"},
	}
	for _, c := range cases {
		if got := c.cfg.DSN(); got != c.want {
			t.Errorf("%s DSN: %q != %q", c.cfg.Dialect, got, c.want)
		}
	}
	// 默认端口
	if got := (DatabaseConfig{Dialect: "mysql", Host: "h", Username: "u", Password: "p", Database: "d"}).DSN(); got != "u:p@tcp(h:3306)/d?charset=utf8mb4&parseTime=True&loc=Local" {
		t.Errorf("default port mysql: %q", got)
	}
}

// 数据迁移：源库用机器密钥加密，目标库用同步密钥重新加密，两边都能正确解密
func TestMigrateDataReencrypt(t *testing.T) {	oldSeed := secure.CurrentKeySeed()
	defer secure.SetKeySeed(oldSeed)

	dir := t.TempDir()
	src, err := db.Open("sqlite", filepath.Join(dir, "src.db"))
	if err != nil {
		t.Fatal(err)
	}
	dst, err := db.Open("sqlite", filepath.Join(dir, "dst.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s, err := src.DB(); err == nil {
			s.Close()
		}
		if d, err := dst.DB(); err == nil {
			d.Close()
		}
	}()
	models := []interface{}{
		&model.SavedConnection{}, &model.CustomCommand{}, &model.Favorite{}, &model.Setting{},
	}
	if err := src.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	if err := dst.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}

	// 源库按机器密钥写入
	secure.SetKeySeed("")
	if err := src.Create(&model.SavedConnection{
		Name: "测试机", Host: "10.0.0.1", Port: 22, Username: "root",
		Password: "secret-pass", UseKey: false,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := src.Create(&model.Setting{Key: "keepalive.interval", Value: "30"}).Error; err != nil {
		t.Fatal(err)
	}

	// 迁移到目标库（函数内部切换密钥：读源用机器密钥，写目标用同步密钥）
	if err := migrateData(src, dst, "sync-key-abc"); err != nil {
		t.Fatalf("migrateData: %v", err)
	}

	// 目标库用同步密钥可解密
	secure.SetKeySeed("sync-key-abc")
	var got model.SavedConnection
	if err := dst.First(&got, 1).Error; err != nil {
		t.Fatal(err)
	}
	if got.Password != "secret-pass" || got.Host != "10.0.0.1" {
		t.Fatalf("dst decrypt failed: %+v", got)
	}
	var s model.Setting
	if err := dst.First(&s, "key = ?", "keepalive.interval").Error; err != nil {
		t.Fatal(err)
	}
	if s.Value != "30" {
		t.Fatalf("setting not migrated: %q", s.Value)
	}

	// 源库仍用机器密钥可解密
	secure.SetKeySeed("")
	var orig model.SavedConnection
	if err := src.First(&orig, 1).Error; err != nil {
		t.Fatal(err)
	}
	if orig.Password != "secret-pass" {
		t.Fatalf("src decrypt failed: %q", orig.Password)
	}
}

// 连接串编码 / 解码：往返一致，且能识别非法输入
func TestConfigEncodeDecode(t *testing.T) {
	want := DatabaseConfig{
		Dialect: "mysql", Host: "db.example.com", Port: 3306,
		Username: "root", Password: "pw", Database: "spark",
		Params: "charset=utf8mb4", SyncKey: "sync-1",
	}
	s := encodeConfig(want)
	if !strings.HasPrefix(s, configStringPrefix) {
		t.Fatalf("export prefix: %q", s)
	}

	got, err := decodeConfig(s)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}

	// 非法输入
	if _, err := decodeConfig("hello world"); err == nil {
		t.Fatal("garbage should fail import")
	}
	if _, err := decodeConfig(configStringPrefix + "!!not-base64!!"); err == nil {
		t.Fatal("bad base64 should fail import")
	}
	if _, err := decodeConfig("sparkdb://" + base64.RawURLEncoding.EncodeToString([]byte(`{"dialect":"mongo"}`))); err == nil {
		t.Fatal("unknown dialect should fail import")
	}
	// 远程库缺 host / database
	bad := DatabaseConfig{Dialect: "postgres", Host: "", Username: "u", Password: "p", Database: "d"}
	data, _ := json.Marshal(bad)
	if _, err := decodeConfig(configStringPrefix + base64.RawURLEncoding.EncodeToString(data)); err == nil {
		t.Fatal("remote config without host should fail import")
	}
}

// 配置本地持久化：保存后能原样读回（重启后无需重新设置）
func TestConfigPersistence(t *testing.T) {	path := filepath.Join(t.TempDir(), "dbconfig.json")
	cfg := DatabaseConfig{
		Dialect: "mysql", Host: "db.example.com", Port: 3306,
		Username: "root", Password: "pw", Database: "spark", SyncKey: "sync-1",
	}
	if err := saveTo(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, ok := loadFrom(path)
	if !ok {
		t.Fatal("config not loaded")
	}
	if got.Dialect != cfg.Dialect || got.Host != cfg.Host || got.Port != cfg.Port ||
		got.Username != cfg.Username || got.Password != cfg.Password ||
		got.Database != cfg.Database || got.SyncKey != cfg.SyncKey {
		t.Fatalf("persistence mismatch: %+v", got)
	}
	// 默认 sqlite：未配置时返回 sqlite
	empty, ok := loadFrom(filepath.Join(t.TempDir(), "missing.json"))
	if ok || empty.Dialect != "" {
		t.Fatal("missing config should not load")
	}
	if (DatabaseConfig{}).DSN() != "gorm.db" {
		t.Fatalf("empty config should default to sqlite dsn, got %q", (DatabaseConfig{}).DSN())
	}
}

// 目标库已有数据时，不重复迁移（多机共享同一远程库的场景）
func TestTargetHasData(t *testing.T) {
	dir := t.TempDir()
	empty, err := db.Open("sqlite", filepath.Join(dir, "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB(t, empty)
	if err := empty.AutoMigrate(&model.SavedConnection{}); err != nil {
		t.Fatal(err)
	}
	if targetHasData(empty) {
		t.Fatal("empty db should have no data")
	}

	full, err := db.Open("sqlite", filepath.Join(dir, "full.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB(t, full)
	if err := full.AutoMigrate(&model.SavedConnection{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := full.Create(&model.Setting{Key: "k", Value: "v"}).Error; err != nil {
		t.Fatal(err)
	}
	if !targetHasData(full) {
		t.Fatal("db with a setting row should have data")
	}
}

func closeDB(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	if s, err := gdb.DB(); err == nil {
		s.Close()
	}
}
