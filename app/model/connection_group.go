package model

import "time"

const TableNameConnectionGroup = "connection_groups"

// ConnectionGroup records the user-defined groups that saved connections can
// be organized into. The group name is the primary key; Sort controls the
// order groups appear in the sidebar (created order, stable across renames).
type ConnectionGroup struct {
	Name      string    `gorm:"column:name;primaryKey" json:"name"`
	Sort      int       `gorm:"column:sort" json:"sort"`
	CreatedAt time.Time `json:"createdAt"`
}

func (*ConnectionGroup) TableName() string {
	return TableNameConnectionGroup
}
