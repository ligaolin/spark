package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

const TableNameCustomCommand = "custom_commands"

// CustomCommand stores a user-defined command that can be sent to the
// interactive SSH terminal with one click.
type CustomCommand struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"column:name" json:"name"`
	Command   string    `gorm:"column:command" json:"command"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*CustomCommand) TableName() string {
	return TableNameCustomCommand
}

// UnmarshalJSON tolerates empty time strings ("") for CreatedAt/UpdatedAt
// sent by the frontend forms (the binding maps time.Time to string).
func (c *CustomCommand) UnmarshalJSON(data []byte) error {
	type alias CustomCommand
	aux := &struct {
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
		*alias
	}{alias: (*alias)(c)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if aux.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, aux.CreatedAt); err == nil {
			c.CreatedAt = t
		}
	}
	if aux.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, aux.UpdatedAt); err == nil {
			c.UpdatedAt = t
		}
	}
	return nil
}

// BeforeUpdate preserves the original CreatedAt when the incoming value is
// zero (the frontend edit form does not carry the original timestamp).
func (c *CustomCommand) BeforeUpdate(tx *gorm.DB) error {
	if !c.CreatedAt.IsZero() {
		return nil
	}
	var old CustomCommand
	if err := tx.Select("created_at").First(&old, c.ID).Error; err == nil {
		c.CreatedAt = old.CreatedAt
	}
	return nil
}
