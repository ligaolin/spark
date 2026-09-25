package model

import "time"

const (
	TableNameRestFolder      = "rest_folders"
	TableNameRestRequest     = "rest_requests"
	TableNameRestEnvironment = "rest_environments"
)

// RestFolder is a folder node in the REST request tree. Folders and child folders
// form a multi-level directory structure; leaf nodes are RestRequestModel entries
// that belong to a folder via FolderID.
type RestFolder struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	ParentID      uint      `gorm:"column:parent_id;index" json:"parentId"` // 0 = root
	Name          string    `gorm:"column:name" json:"name"`
	BaseURL       string    `gorm:"column:base_url" json:"baseUrl"`             // 覆盖该文件夹下所有请求的基础链接（为空则继承上级）
	CommonHeaders string    `gorm:"column:common_headers" json:"commonHeaders"` // JSON: 文件夹级公共请求头（合并上级，同 key 覆盖）
	Sort          int       `gorm:"column:sort" json:"sort"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (*RestFolder) TableName() string {
	return TableNameRestFolder
}

// RestRequestModel is a saved REST request. Each request lives under a folder
// (FolderID). The request body, headers, and params are stored as JSON text.
type RestRequestModel struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	FolderID  uint      `gorm:"column:folder_id;index" json:"folderId"` // 0 = root level (no folder)
	Name      string    `gorm:"column:name" json:"name"`
	Method    string    `gorm:"column:method" json:"method"`    // GET | POST | PUT | DELETE | PATCH | HEAD | OPTIONS
	URL       string    `gorm:"column:url" json:"url"`          // path relative to environment base URL, or absolute
	BaseURL   string    `gorm:"column:base_url" json:"baseUrl"` // 该请求专属的基础链接（覆盖文件夹和环境设置，为空则继承上级）
	Headers   string    `gorm:"column:headers" json:"headers"`  // JSON: [{"key":"...","value":"..."}]
	Params    string    `gorm:"column:params" json:"params"`    // JSON: [{"key":"...","value":"..."}]
	Body      string    `gorm:"column:body" json:"body"`
	Sort      int       `gorm:"column:sort" json:"sort"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (*RestRequestModel) TableName() string {
	return TableNameRestRequest
}

// RestEnvironment stores a named environment with a base URL and common headers.
// Environments can be switched globally when sending requests.
type RestEnvironment struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Name          string    `gorm:"column:name" json:"name"`
	BaseURL       string    `gorm:"column:base_url" json:"baseUrl"`
	CommonHeaders string    `gorm:"column:common_headers" json:"commonHeaders"` // JSON: [{"key":"...","value":"..."}]
	IsDefault     bool      `gorm:"column:is_default" json:"isDefault"`
	Sort          int       `gorm:"column:sort" json:"sort"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (*RestEnvironment) TableName() string {
	return TableNameRestEnvironment
}
