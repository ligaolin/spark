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

// fileKindByExt 按扩展名判定文档类型（大小写不敏感）。新增类型时在这里加一条，
// 并在前端 utils/fileKind.ts 同步（两侧保持一致），然后在 DocumentsView 增加
// 对应编辑器的渲染分支。
var fileKindByExt = map[string]string{
	".md":       "md",
	".markdown": "md",
}

// kindForName 根据文件名的扩展名返回文档类型；无匹配时返回 "text"。
func kindForName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	for ext, kind := range fileKindByExt {
		if strings.HasSuffix(lower, ext) {
			return kind
		}
	}
	return "text"
}

// Create adds a new folder or file under parentID (0 = root) and returns it.
// 文件类型由文件名扩展名决定（如 .md → Markdown），新建时无需专门选择类型。
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
	if nodeType == "file" {
		n.Kind = kindForName(name)
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
	if n.Type != "folder" {
		// 重命名时按新扩展名同步文件类型（如 .txt → .md 切换 Markdown 编辑器）
		newKind := kindForName(name)
		if newKind != n.Kind {
			_ = db.GetDB().Model(&model.DocNode{}).Where("id = ?", id).Update("kind", newKind).Error
		}
	}
	return db.GetDB().Model(&model.DocNode{}).Where("id = ?", id).Update("name", name).Error
}

// Move moves a node to a new parent (0 = root). Moving a folder into its own
// descendant is rejected. The node is appended to the end of the new parent.
func (s *DocumentService) Move(id uint, newParentID uint) error {
	return s.Reorder(id, newParentID, 0, "after")
}

// Reorder moves a node to newParentID and positions it before/after targetID
// among its same-type siblings (targetID == 0 appends at the end). This is the
// backend for the frontend drag-and-drop sort: folders reorder with folders,
// files with files, and the frontend keeps folders grouped first. The siblings'
// Sort values are rewritten to a contiguous 0..n range afterwards.
func (s *DocumentService) Reorder(id uint, newParentID uint, targetID uint, position string) error {
	if id == 0 {
		return errors.New("节点 ID 不能为空")
	}
	var n model.DocNode
	if err := db.GetDB().First(&n, id).Error; err != nil {
		return err
	}
	if n.ParentID != newParentID {
		if id == newParentID {
			return errors.New("不能移动到自身")
		}
		if isDescendant(id, newParentID) {
			return errors.New("不能移动到自己的子目录中")
		}
		if err := db.GetDB().Model(&model.DocNode{}).Where("id = ?", id).Update("parent_id", newParentID).Error; err != nil {
			return err
		}
	}

	// 同级同类型节点（排除自身），按 sort,id 排序。
	var siblings []model.DocNode
	if err := db.GetDB().
		Where("parent_id = ? AND type = ?", newParentID, n.Type).
		Order("sort asc, id asc").
		Find(&siblings).Error; err != nil {
		return err
	}
	ids := make([]uint, 0, len(siblings))
	for _, sib := range siblings {
		if sib.ID == id {
			continue
		}
		ids = append(ids, sib.ID)
	}

	pos := len(ids)
	if targetID != 0 {
		for i, sid := range ids {
			if sid == targetID {
				pos = i
				if position == "after" {
					pos = i + 1
				}
				break
			}
		}
	}

	ids = append(ids, 0)
	copy(ids[pos+1:], ids[pos:])
	ids[pos] = id
	for i, sid := range ids {
		if err := db.GetDB().Model(&model.DocNode{}).Where("id = ?", sid).Update("sort", i).Error; err != nil {
			return err
		}
	}
	return nil
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
