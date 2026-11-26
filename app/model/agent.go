package model

import "time"

const TableNameAgentConversation = "agent_conversations"

// AgentConversation persists one AI assistant conversation for a server.
// ServerKey is "user@host:port" so history survives reconnects (new session
// ids) and stays grouped per server. LLMMessages is the protocol history
// (JSON []ai.Message) owned by the backend; Transcript is the UI entry list
// (JSON, written by the frontend) so restoring a conversation looks exactly
// like the live one.
type AgentConversation struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	ServerKey string `gorm:"column:server_key;index" json:"serverKey"`
	Title     string `gorm:"column:title" json:"title"`

	LLMMessages string `gorm:"column:llm_messages" json:"-"`
	Transcript  string `gorm:"column:transcript" json:"transcript"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*AgentConversation) TableName() string {
	return TableNameAgentConversation
}
