package model

import "time"

const TableNameAIProvider = "ai_providers"

// AIProvider is one saved OpenAI-compatible endpoint. The active provider's
// fields are mirrored into the ai.* settings keys, so every request path keeps
// reading a single config and switching just re-applies a row.
type AIProvider struct {
	ID           uint    `gorm:"primaryKey" json:"id"`
	Name         string  `gorm:"column:name" json:"name"`
	BaseURL      string  `gorm:"column:base_url" json:"baseUrl"`
	Model        string  `gorm:"column:model" json:"model"`
	Temperature  float64 `gorm:"column:temperature" json:"temperature"`
	MaxTokens    int     `gorm:"column:max_tokens" json:"maxTokens"`
	SystemPrompt string  `gorm:"column:system_prompt" json:"systemPrompt"`
	// APIKey 为 AES-GCM 密文，绝不返回给前端
	APIKey string `gorm:"column:api_key" json:"-"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*AIProvider) TableName() string {
	return TableNameAIProvider
}
