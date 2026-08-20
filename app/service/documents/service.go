// Package documents implements the built-in document management module:
// a folder/file tree persisted in the database, with text content for files,
// rename / move / delete, and filename or content search.
package documents

import (
	"errors"
	"strings"

	"changeme/app/model"
	"changeme/app/service/db"
	"changeme/app/service/fileutil"
	"changeme/app/service/types"
)

// DocumentService exposes document CRUD + search to the frontend.
type DocumentService struct{}

// ServiceName implements application.ServiceName.
func (s *DocumentService) ServiceName() string { return "DocumentService" }

// List returns every document node without content (for building the tree),
// ordered by sort then name.
func (s *DocumentService) List() ([]model.DocNode, error) {
	var list []model.DocNode
	err := db.GetDB().Omit("content").Order("sort asc, name asc").Find(&list).Error
	return list, err
}

// Create adds a new folder or file under parentID (0 = root) and returns it.
func (s *DocumentService) Create(parentID uint, name, nodeType string) (model.DocNode, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.DocNode{}, errors.New("名称不能为空")
	}
	if nodeType != "folder" {
		nodeType = "file"
	}
	if err := checkNameConflict(parentID, name); err != nil {
		return model.DocNode{}, err
	}
	n := model.DocNode{
		ParentID: parentID,
		Name:     name,
		Type:     nodeType,
		Sort:     nextSort(parentID),
	}
	if err := db.GetDB().Create(&n).Error; err != nil {
		return n, err
	}
	return n, nil
}

// Rename renames a node (same-name is a no-op; duplicates rejected).
func (s *DocumentService) Rename(id uint, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("名称不能为空")
	}
	if id == 0 {
		return errors.New("节点 ID 不能为空")
	}
	var n model.DocNode
	if err := db.GetDB().First(&n, id).Error; err != nil {
		return err
	}
	if name != n.Name {
		if err := checkNameConflict(n.ParentID, name); err != nil {
			return err
		}
	}
	return db.GetDB().Model(&model.DocNode{}).Where("id = ?", id).Update("name", name).Error
}

// Move moves a node to a new parent (0 = root). Moving a folder into its own
// descendant is rejected.
func (s *DocumentService) Move(id uint, newParentID uint) error {
	if id == 0 {
		return errors.New("节点 ID 不能为空")
	}
	if id == newParentID {
		return errors.New("不能移动到自身")
	}
	if isDescendant(id, newParentID) {
		return errors.New("不能移动到自己的子目录中")
	}
	return db.GetDB().Model(&model.DocNode{}).Where("id = ?", id).Update("parent_id", newParentID).Error
}

// Delete removes a node and all of its descendants.
func (s *DocumentService) Delete(id uint) error {
	if id == 0 {
		return errors.New("节点 ID 不能为空")
	}
	ids := collectSubtreeIDs(id)
	return db.GetDB().Where("id IN ?", ids).Delete(&model.DocNode{}).Error
}

// GetContent returns the text content of a file node.
func (s *DocumentService) GetContent(id uint) (string, error) {
	var n model.DocNode
	if err := db.GetDB().First(&n, id).Error; err != nil {
		return "", err
	}
	if n.Type == "folder" {
		return "", errors.New("目录没有可编辑内容")
	}
	return n.Content, nil
}

// SaveContent overwrites the text content of a file node.
func (s *DocumentService) SaveContent(id uint, content string) error {
	if id == 0 {
		return errors.New("节点 ID 不能为空")
	}
	var n model.DocNode
	if err := db.GetDB().First(&n, id).Error; err != nil {
		return err
	}
	if n.Type == "folder" {
		return errors.New("目录不能保存内容")
	}
	return db.GetDB().Model(&model.DocNode{}).Where("id = ?", id).Update("content", content).Error
}

// Search searches the document tree by name (mode "name") or by file content
// (mode "content").
func (s *DocumentService) Search(pattern, mode string) ([]types.SearchResult, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, errors.New("请输入搜索关键字")
	}
	var nodes []model.DocNode
	if err := db.GetDB().Find(&nodes).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint]*model.DocNode, len(nodes))
	for i := range nodes {
		byID[nodes[i].ID] = &nodes[i]
	}
	pathOf := func(n *model.DocNode) string {
		parts := make([]string, 0, 8)
		cur := n
		for cur != nil {
			parts = append([]string{cur.Name}, parts...)
			cur = byID[cur.ParentID]
		}
		return "/" + strings.Join(parts, "/")
	}

	contentMode := strings.EqualFold(mode, "content")
	results := make([]types.SearchResult, 0, 64)
	for i := range nodes {
		n := &nodes[i]
		if len(results) >= fileutil.MaxSearchResults {
			break
		}
		if contentMode {
			if n.Type == "folder" {
				continue
			}
			for _, h := range fileutil.MatchLines([]byte(n.Content), pattern, fileutil.MaxMatchesPerFile) {
				results = append(results, types.SearchResult{
					Path:    pathOf(n),
					Name:    n.Name,
					Size:    int64(len(n.Content)),
					ModTime: n.UpdatedAt,
					LineNo:  h.LineNo,
					Line:    h.Line,
				})
				if len(results) >= fileutil.MaxSearchResults {
					break
				}
			}
		} else if fileutil.MatchName(n.Name, pattern) {
			var size int64
			if n.Type != "folder" {
				size = int64(len(n.Content))
			}
			results = append(results, types.SearchResult{
				Path:    pathOf(n),
				Name:    n.Name,
				Size:    size,
				ModTime: n.UpdatedAt,
				IsDir:   n.Type == "folder",
			})
		}
	}
	return results, nil
}

func checkNameConflict(parentID uint, name string) error {
	var count int64
	if err := db.GetDB().Model(&model.DocNode{}).
		Where("parent_id = ? AND name = ?", parentID, name).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("同级已存在同名节点")
	}
	return nil
}

func nextSort(parentID uint) int {
	var max int
	_ = db.GetDB().Model(&model.DocNode{}).
		Where("parent_id = ?", parentID).
		Select("COALESCE(MAX(sort), 0)").
		Scan(&max).Error
	return max + 1
}

func collectSubtreeIDs(rootID uint) []uint {
	var all []model.DocNode
	if err := db.GetDB().Omit("content").Find(&all).Error; err != nil {
		return []uint{rootID}
	}
	ids := []uint{rootID}
	queue := []uint{rootID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, n := range all {
			if n.ParentID == cur {
				ids = append(ids, n.ID)
				queue = append(queue, n.ID)
			}
		}
	}
	return ids
}

// isDescendant reports whether candidateParent is node id or one of id's
// descendants (used to reject cycles when moving a folder into itself).
func isDescendant(id, candidateParent uint) bool {
	cur := candidateParent
	for cur != 0 {
		if cur == id {
			return true
		}
		var n model.DocNode
		if err := db.GetDB().Select("parent_id").First(&n, cur).Error; err != nil {
			return false
		}
		cur = n.ParentID
	}
	return false
}
