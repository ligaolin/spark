package model

import "time"

const TableNameDocNode = "doc_nodes"

// DocNode is one node in the built-in document tree. Folders and files share
// the same table; files carry their text content in Content.
type DocNode struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	ParentID uint   `gorm:"column:parent_id;index" json:"parentId"` // 0 = root
	Name     string `gorm:"column:name" json:"name"`
	Type     string `gorm:"column:type" json:"type"` // "folder" | "file"
	Content  string `gorm:"column:content" json:"content"`

	// Sort keeps siblings in a stable user-visible order (folders first is
	// applied on the frontend).
	Sort      int       `gorm:"column:sort" json:"sort"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*DocNode) TableName() string {
	return TableNameDocNode
}
