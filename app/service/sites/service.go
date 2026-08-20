// Package sites implements the site-management module: a tree of folders and
// sites; each site carries multiple links (URLs) and multiple accounts
// (username/password/note). Links can be opened in-app (embedded) or in the
// system browser.
package sites

import (
	"errors"
	"strings"

	"changeme/app/model"
	"changeme/app/service/db"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// SiteService exposes folder/site/link/account CRUD + URL opening.
type SiteService struct{}

// ServiceName implements application.ServiceName.
func (s *SiteService) ServiceName() string { return "SiteService" }

// ---------- 文件夹 ----------

// ListFolders returns all folder nodes (for building the tree).
func (s *SiteService) ListFolders() ([]model.SiteFolder, error) {
	var list []model.SiteFolder
	err := db.GetDB().Order("sort asc, id asc").Find(&list).Error
	return list, err
}

// CreateFolder creates a folder under parentID (0 = root).
func (s *SiteService) CreateFolder(parentID uint, name string) (model.SiteFolder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.SiteFolder{}, errors.New("文件夹名称不能为空")
	}
	if err := checkFolderNameConflict(parentID, name); err != nil {
		return model.SiteFolder{}, err
	}
	f := model.SiteFolder{ParentID: parentID, Name: name, Sort: nextFolderSort(parentID)}
	if err := db.GetDB().Create(&f).Error; err != nil {
		return f, err
	}
	return f, nil
}

// RenameFolder renames a folder.
func (s *SiteService) RenameFolder(id uint, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("文件夹名称不能为空")
	}
	if id == 0 {
		return errors.New("文件夹 ID 不能为空")
	}
	var f model.SiteFolder
	if err := db.GetDB().First(&f, id).Error; err != nil {
		return err
	}
	if name != f.Name {
		if err := checkFolderNameConflict(f.ParentID, name); err != nil {
			return err
		}
	}
	return db.GetDB().Model(&model.SiteFolder{}).Where("id = ?", id).Update("name", name).Error
}

// MoveFolder moves a folder to a new parent (cycle prevented).
func (s *SiteService) MoveFolder(id uint, newParentID uint) error {
	if id == 0 {
		return errors.New("文件夹 ID 不能为空")
	}
	if id == newParentID {
		return errors.New("不能移动到自身")
	}
	if isFolderDescendant(id, newParentID) {
		return errors.New("不能移动到自己的子目录中")
	}
	return db.GetDB().Model(&model.SiteFolder{}).Where("id = ?", id).Update("parent_id", newParentID).Error
}

// DeleteFolder removes a folder, its descendant folders, and the sites (with
// their links/accounts) inside them.
func (s *SiteService) DeleteFolder(id uint) error {
	if id == 0 {
		return errors.New("文件夹 ID 不能为空")
	}
	folderIDs := collectFolderIDs(id)
	var siteIDs []uint
	_ = db.GetDB().Model(&model.Site{}).Where("folder_id IN ?", folderIDs).Pluck("id", &siteIDs).Error
	if len(siteIDs) > 0 {
		var linkIDs []uint
		_ = db.GetDB().Model(&model.SiteLink{}).Where("site_id IN ?", siteIDs).Pluck("id", &linkIDs).Error
		if len(linkIDs) > 0 {
			_ = db.GetDB().Where("link_id IN ?", linkIDs).Delete(&model.SiteAccount{}).Error
		}
		_ = db.GetDB().Where("site_id IN ?", siteIDs).Delete(&model.SiteLink{}).Error
		_ = db.GetDB().Where("id IN ?", siteIDs).Delete(&model.Site{}).Error
	}
	return db.GetDB().Where("id IN ?", folderIDs).Delete(&model.SiteFolder{}).Error
}

// ---------- 站点 ----------

// ListSites returns all sites (with their folderId).
func (s *SiteService) ListSites() ([]model.Site, error) {
	var list []model.Site
	err := db.GetDB().Order("sort asc, id asc").Find(&list).Error
	return list, err
}

// CreateSite creates a site under folderID (0 = root).
func (s *SiteService) CreateSite(folderID uint, name, note string) (model.Site, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Site{}, errors.New("站点名称不能为空")
	}
	if err := checkSiteNameConflict(folderID, name); err != nil {
		return model.Site{}, err
	}
	site := model.Site{FolderID: folderID, Name: name, Note: strings.TrimSpace(note), Sort: nextSiteSort(folderID)}
	if err := db.GetDB().Create(&site).Error; err != nil {
		return site, err
	}
	return site, nil
}

// UpdateSite updates a site's name / note.
func (s *SiteService) UpdateSite(id uint, name, note string) (model.Site, error) {
	if id == 0 {
		return model.Site{}, errors.New("站点 ID 不能为空")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Site{}, errors.New("站点名称不能为空")
	}
	var site model.Site
	if err := db.GetDB().First(&site, id).Error; err != nil {
		return site, err
	}
	if name != site.Name {
		if err := checkSiteNameConflict(site.FolderID, name); err != nil {
			return site, err
		}
	}
	site.Name = name
	site.Note = strings.TrimSpace(note)
	if err := db.GetDB().Save(&site).Error; err != nil {
		return site, err
	}
	return site, nil
}

// MoveSite moves a site to a different folder (0 = root).
func (s *SiteService) MoveSite(id uint, newFolderID uint) error {
	if id == 0 {
		return errors.New("站点 ID 不能为空")
	}
	return db.GetDB().Model(&model.Site{}).Where("id = ?", id).Update("folder_id", newFolderID).Error
}

// DeleteSite removes a site and its links and accounts.
func (s *SiteService) DeleteSite(id uint) error {
	if id == 0 {
		return errors.New("站点 ID 不能为空")
	}
	var linkIDs []uint
	_ = db.GetDB().Model(&model.SiteLink{}).Where("site_id = ?", id).Pluck("id", &linkIDs).Error
	if len(linkIDs) > 0 {
		_ = db.GetDB().Where("link_id IN ?", linkIDs).Delete(&model.SiteAccount{}).Error
	}
	_ = db.GetDB().Where("site_id = ?", id).Delete(&model.SiteLink{}).Error
	return db.GetDB().Delete(&model.Site{}, id).Error
}

// ---------- 链接 ----------

// ListLinks returns the links of a site.
func (s *SiteService) ListLinks(siteID uint) ([]model.SiteLink, error) {
	var list []model.SiteLink
	err := db.GetDB().Where("site_id = ?", siteID).Order("sort asc, id asc").Find(&list).Error
	return list, err
}

// CreateLink creates a link under a site. Name is optional; when empty it
// defaults to the URL host.
func (s *SiteService) CreateLink(siteID uint, name, url, note string) (model.SiteLink, error) {
	if siteID == 0 {
		return model.SiteLink{}, errors.New("请先选择站点")
	}
	name = strings.TrimSpace(name)
	url = strings.TrimSpace(url)
	if url == "" {
		return model.SiteLink{}, errors.New("链接地址不能为空")
	}
	if name == "" {
		name = linkDisplayName(url)
	}
	link := model.SiteLink{SiteID: siteID, Name: name, URL: url, Note: strings.TrimSpace(note), Sort: nextLinkSort(siteID)}
	if err := db.GetDB().Create(&link).Error; err != nil {
		return link, err
	}
	return link, nil
}

// UpdateLink updates a link. Name is optional; when empty it defaults to the
// URL host.
func (s *SiteService) UpdateLink(id uint, name, url, note string) (model.SiteLink, error) {
	if id == 0 {
		return model.SiteLink{}, errors.New("链接 ID 不能为空")
	}
	name = strings.TrimSpace(name)
	url = strings.TrimSpace(url)
	if url == "" {
		return model.SiteLink{}, errors.New("链接地址不能为空")
	}
	if name == "" {
		name = linkDisplayName(url)
	}
	var link model.SiteLink
	if err := db.GetDB().First(&link, id).Error; err != nil {
		return link, err
	}
	link.Name = name
	link.URL = url
	link.Note = strings.TrimSpace(note)
	if err := db.GetDB().Save(&link).Error; err != nil {
		return link, err
	}
	return link, nil
}

// DeleteLink removes a link and its accounts.
func (s *SiteService) DeleteLink(id uint) error {
	if id == 0 {
		return errors.New("链接 ID 不能为空")
	}
	_ = db.GetDB().Where("link_id = ?", id).Delete(&model.SiteAccount{}).Error
	return db.GetDB().Delete(&model.SiteLink{}, id).Error
}

// ---------- 账号 ----------

// ListAccounts returns the accounts of a link (passwords decrypted).
func (s *SiteService) ListAccounts(linkID uint) ([]model.SiteAccount, error) {
	var list []model.SiteAccount
	err := db.GetDB().Where("link_id = ?", linkID).Order("id asc").Find(&list).Error
	return list, err
}

// CreateAccount creates an account under a link.
func (s *SiteService) CreateAccount(linkID uint, username, password, note string) (model.SiteAccount, error) {
	if linkID == 0 {
		return model.SiteAccount{}, errors.New("请先选择链接")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return model.SiteAccount{}, errors.New("账号不能为空")
	}
	acc := model.SiteAccount{LinkID: linkID, Username: username, Password: password, Note: strings.TrimSpace(note)}
	if err := db.GetDB().Create(&acc).Error; err != nil {
		return acc, err
	}
	return acc, nil
}

// UpdateAccount updates an account.
func (s *SiteService) UpdateAccount(id uint, username, password, note string) (model.SiteAccount, error) {
	if id == 0 {
		return model.SiteAccount{}, errors.New("账号 ID 不能为空")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return model.SiteAccount{}, errors.New("账号不能为空")
	}
	var acc model.SiteAccount
	if err := db.GetDB().First(&acc, id).Error; err != nil {
		return acc, err
	}
	acc.Username = username
	acc.Password = password
	acc.Note = strings.TrimSpace(note)
	if err := db.GetDB().Save(&acc).Error; err != nil {
		return acc, err
	}
	return acc, nil
}

// DeleteAccount removes an account.
func (s *SiteService) DeleteAccount(id uint) error {
	if id == 0 {
		return errors.New("账号 ID 不能为空")
	}
	return db.GetDB().Delete(&model.SiteAccount{}, id).Error
}

// ---------- 打开 ----------

// OpenInBrowser opens a URL in the system default browser.
func (s *SiteService) OpenInBrowser(url string) error {
	url = normalizeURL(url)
	if url == "" {
		return errors.New("链接地址不能为空")
	}
	return application.Get().Browser.OpenURL(url)
}

// OpenInApp opens a URL in a new in-app window (its own WebView). Kept as an
// alternative; the primary flow embeds the URL in the frontend instead.
func (s *SiteService) OpenInApp(url, title string) error {
	url = normalizeURL(url)
	if url == "" {
		return errors.New("链接地址不能为空")
	}
	if strings.TrimSpace(title) == "" {
		title = "站点"
	}
	application.Get().Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     title,
		URL:       url,
		Width:     1280,
		Height:    820,
		MinWidth:  480,
		MinHeight: 360,
	})
	return nil
}

// ---------- helpers ----------

func normalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	return u
}

// linkDisplayName derives a default link name from a URL host
// (e.g. "https://example.com/a/b" -> "example.com").
func linkDisplayName(url string) string {
	u := url
	for _, p := range []string{"https://", "http://"} {
		u = strings.TrimPrefix(u, p)
	}
	if i := strings.IndexByte(u, '/'); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimSpace(u)
	if u == "" {
		return url
	}
	return u
}

func checkFolderNameConflict(parentID uint, name string) error {
	var count int64
	if err := db.GetDB().Model(&model.SiteFolder{}).
		Where("parent_id = ? AND name = ?", parentID, name).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("同级已存在同名文件夹")
	}
	return nil
}

func checkSiteNameConflict(folderID uint, name string) error {
	var count int64
	if err := db.GetDB().Model(&model.Site{}).
		Where("folder_id = ? AND name = ?", folderID, name).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("同级已存在同名站点")
	}
	return nil
}

func nextFolderSort(parentID uint) int {
	var max int
	_ = db.GetDB().Model(&model.SiteFolder{}).Where("parent_id = ?", parentID).
		Select("COALESCE(MAX(sort), 0)").Scan(&max).Error
	return max + 1
}

func nextSiteSort(folderID uint) int {
	var max int
	_ = db.GetDB().Model(&model.Site{}).Where("folder_id = ?", folderID).
		Select("COALESCE(MAX(sort), 0)").Scan(&max).Error
	return max + 1
}

func nextLinkSort(siteID uint) int {
	var max int
	_ = db.GetDB().Model(&model.SiteLink{}).Where("site_id = ?", siteID).
		Select("COALESCE(MAX(sort), 0)").Scan(&max).Error
	return max + 1
}

func collectFolderIDs(rootID uint) []uint {
	var all []model.SiteFolder
	if err := db.GetDB().Find(&all).Error; err != nil {
		return []uint{rootID}
	}
	ids := []uint{rootID}
	queue := []uint{rootID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, f := range all {
			if f.ParentID == cur {
				ids = append(ids, f.ID)
				queue = append(queue, f.ID)
			}
		}
	}
	return ids
}

// isFolderDescendant reports whether candidateParent is folder id or one of
// id's descendants (used to reject cycles when moving a folder into itself).
func isFolderDescendant(id, candidateParent uint) bool {
	cur := candidateParent
	for cur != 0 {
		if cur == id {
			return true
		}
		var f model.SiteFolder
		if err := db.GetDB().Select("parent_id").First(&f, cur).Error; err != nil {
			return false
		}
		cur = f.ParentID
	}
	return false
}
