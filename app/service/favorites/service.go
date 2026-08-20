// Package favorites provides CRUD operations for saved directory bookmarks
// (remote directories per connection, and local directories), persisted in
// SQLite through GORM.
package favorites

import (
	"errors"

	"changeme/app/model"
	"changeme/app/service/db"
)

// FavoriteService exposes favorite CRUD to the frontend.
type FavoriteService struct{}

// ServiceName implements application.ServiceName.
func (s *FavoriteService) ServiceName() string { return "FavoriteService" }

// List returns the favorites of a kind ("remote"/"local") for a connection
// (connectionID is 0 for local bookmarks), most recently added first.
func (s *FavoriteService) List(kind string, connectionID uint) ([]model.Favorite, error) {
	if kind != "local" {
		kind = "remote"
	}
	var list []model.Favorite
	query := db.GetDB()
	if kind == "local" {
		query = query.Where("kind = 'local'")
	} else {
		// 兼容旧数据（kind 为空视为 remote）
		query = query.Where("(kind = 'remote' OR kind = '') AND connection_id = ?", connectionID)
	}
	err := query.Order("created_at desc").Find(&list).Error
	return list, err
}

// Create persists a new favorite (duplicates are rejected).
func (s *FavoriteService) Create(fav model.Favorite) (model.Favorite, error) {
	if fav.Kind != "local" {
		fav.Kind = "remote"
	}
	if fav.Kind == "remote" && fav.ConnectionID == 0 {
		return fav, errors.New("请先选择连接")
	}
	if fav.Path == "" {
		return fav, errors.New("收藏路径不能为空")
	}
	var count int64
	if err := db.GetDB().Model(&model.Favorite{}).
		Where("kind = ? AND connection_id = ? AND path = ?", fav.Kind, fav.ConnectionID, fav.Path).
		Count(&count).Error; err != nil {
		return fav, err
	}
	if count > 0 {
		return fav, errors.New("该目录已在收藏中")
	}
	if err := db.GetDB().Create(&fav).Error; err != nil {
		return fav, err
	}
	return fav, nil
}

// Delete removes a favorite by id.
func (s *FavoriteService) Delete(id uint) error {
	return db.GetDB().Delete(&model.Favorite{}, id).Error
}
