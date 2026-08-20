// Package connections provides CRUD operations for saved connections,
// persisted in SQLite through GORM.
package connections

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"changeme/app/model"
	"changeme/app/service/db"
	"changeme/app/service/sshlib"
	"changeme/app/service/types"

	"github.com/jlaffaye/ftp"
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

// DedupResult reports the outcome of RemoveDuplicates.
type DedupResult struct {
	Removed int      `json:"removed"`
	Summary []string `json:"summary"`
}

// RemoveDuplicates finds connections sharing the same host + type and keeps
// one of each group: the reachable one with the highest id if any is
// reachable, otherwise the highest id (the "last" created one).
//
// deep=false uses a plain TCP dial to host:port (no credentials needed).
// deep=true additionally performs a real login (SSH auth / FTP Login) so the
// keep-decision also distinguishes credentials that still work. Deep checks
// are slower and may trigger server-side login-failure policies.
func (c *ConnService) RemoveDuplicates(deep bool) (DedupResult, error) {
	var all []model.SavedConnection
	if err := db.GetDB().Order("id asc").Find(&all).Error; err != nil {
		return DedupResult{}, err
	}

	type key struct {
		host string
		typ  string
	}
	groups := map[key][]model.SavedConnection{}
	var order []key
	for _, conn := range all {
		k := key{conn.Host, conn.Type}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], conn)
	}

	// 仅对属于重复组的连接做连通性探测
	reachable := map[uint]bool{}
	var toProbe []model.SavedConnection
	for _, k := range order {
		if len(groups[k]) <= 1 {
			continue
		}
		toProbe = append(toProbe, groups[k]...)
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, conn := range toProbe {
		wg.Add(1)
		go func(cn model.SavedConnection) {
			defer wg.Done()
			ok := reachableConn(cn, deep)
			mu.Lock()
			reachable[cn.ID] = ok
			mu.Unlock()
		}(conn)
	}
	wg.Wait()

	stateWord := "可联通"
	if deep {
		stateWord = "可登录"
	}

	res := DedupResult{Summary: []string{}}
	for _, k := range order {
		g := groups[k]
		if len(g) <= 1 {
			continue
		}
		// g 已按 id 升序；默认保留最后一个（id 最大），优先保留可联通的最后一个
		keep := g[len(g)-1]
		for i := len(g) - 1; i >= 0; i-- {
			if reachable[g[i].ID] {
				keep = g[i]
				break
			}
		}
		note := stateWord
		if !reachable[keep.ID] {
			note = "均不可" + stateWord + "，保留最后一条"
		}
		for _, cn := range g {
			if cn.ID == keep.ID {
				continue
			}
			if err := db.GetDB().Delete(&model.SavedConnection{}, cn.ID).Error; err != nil {
				return res, err
			}
			res.Removed++
		}
		res.Summary = append(res.Summary, fmt.Sprintf(
			"%s（%s）：保留「%s」（%s），删除 %d 个",
			keep.Host, strings.ToUpper(keep.Type), keep.Name, note, len(g)-1,
		))
	}
	return res, nil
}

// reachableConn reports whether a saved connection is usable. With deep=true
// it performs a real login (SSH auth / FTP Login); otherwise a plain TCP dial.
func reachableConn(cn model.SavedConnection, deep bool) bool {
	if !deep {
		return tcpReachable(cn.Host, cn.Port)
	}
	if cn.Type == "ftp" {
		return ftpLoginReachable(cn.Host, cn.Port, cn.Username, cn.Password, cn.TLS)
	}
	opts := types.ConnectOptions{
		Host:       cn.Host,
		Port:       cn.Port,
		Username:   cn.Username,
		Password:   cn.Password,
		UseKey:     cn.UseKey,
		PrivateKey: cn.PrivateKey,
		Passphrase: cn.Passphrase,
	}
	return sshlib.TestLogin(opts) == nil
}

// tcpReachable dials host:port with a short timeout to test reachability.
func tcpReachable(host string, port int) bool {
	if port <= 0 || port > 65535 {
		port = 22
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// ftpLoginReachable dials an FTP(S) server and attempts a login. Certificate
// verification is skipped so a self-signed cert does not hide a working login.
func ftpLoginReachable(host string, port int, username, password string, tlsMode bool) bool {
	if port <= 0 || port > 65535 {
		port = 21
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	var conn *ftp.ServerConn
	var err error
	if tlsMode {
		tlsConf := &tls.Config{InsecureSkipVerify: true, ServerName: host}
		conn, err = ftp.Dial(addr,
			ftp.DialWithTimeout(8*time.Second),
			ftp.DialWithExplicitTLS(tlsConf),
		)
	} else {
		conn, err = ftp.Dial(addr, ftp.DialWithTimeout(8*time.Second))
	}
	if err != nil {
		return false
	}
	defer func() { _ = conn.Quit() }()
	return conn.Login(username, password) == nil
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
