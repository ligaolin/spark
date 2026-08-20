package model

// Setting is a generic key-value configuration entry persisted in SQLite.
// New settings can be added at any time without schema changes.
type Setting struct {
	Key   string `gorm:"primaryKey" json:"key"`
	Value string `gorm:"column:value" json:"value"`
}

func (*Setting) TableName() string {
	return "settings"
}
