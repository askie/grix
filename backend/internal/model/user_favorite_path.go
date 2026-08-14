package model

import "time"

type UserFavoritePath struct {
	ID          int64     `gorm:"primaryKey;autoIncrement:false" json:"id,string"`
	UserID      int64     `gorm:"index;not null" json:"user_id,string"`
	Path        string    `gorm:"not null" json:"path"`
	Name        string    `gorm:"size:255;not null" json:"name"`
	IsDirectory bool      `gorm:"not null;default:false" json:"is_directory"`
	// MachineName is the host/machine the favorited path lives on. Empty for
	// legacy rows created before machine tagging; the client groups those as
	// "unknown machine".
	MachineName string    `gorm:"size:255;not null;default:''" json:"machine_name"`
	CreatedAt   time.Time `json:"created_at"`
}

func (UserFavoritePath) TableName() string { return "user_favorite_paths" }
