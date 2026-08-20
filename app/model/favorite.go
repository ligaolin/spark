package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

const TableNameFavorite = "favorites"

// Favorite bookmarks a directory (remote or local) for quick navigation.
// Kind: "remote" (belongs to a saved connection) or "local" (global local bookmarks).
type Favorite struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Kind         string    `gorm:"column:kind" json:"kind"` // remote | local
	ConnectionID uint      `gorm:"column:connection_id" json:"connectionId"`
	Path         string    `gorm:"column:path" json:"path"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func (*Favorite) TableName() string {
	return TableNameFavorite
}

// UnmarshalJSON tolerates empty time strings ("") for CreatedAt/UpdatedAt
// sent by the frontend (the binding maps time.Time to string).
func (f *Favorite) UnmarshalJSON(data []byte) error {
	type alias Favorite
	aux := &struct {
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
		*alias
	}{alias: (*alias)(f)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if aux.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, aux.CreatedAt); err == nil {
			f.CreatedAt = t
		}
	}
	if aux.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, aux.UpdatedAt); err == nil {
			f.UpdatedAt = t
		}
	}
	return nil
}

// BeforeUpdate preserves the original CreatedAt when the incoming value is zero.
func (f *Favorite) BeforeUpdate(tx *gorm.DB) error {
	if !f.CreatedAt.IsZero() {
		return nil
	}
	var old Favorite
	if err := tx.Select("created_at").First(&old, f.ID).Error; err == nil {
		f.CreatedAt = old.CreatedAt
	}
	return nil
}
