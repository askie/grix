package model

import "time"

type UserSessionFavorite struct {
	ID        int64     `gorm:"primaryKey;autoIncrement:false" json:"id,string"`
	UserID    int64     `gorm:"not null;uniqueIndex:uq_user_session_favorite,priority:1" json:"user_id,string"`
	SessionID string    `gorm:"size:50;not null;uniqueIndex:uq_user_session_favorite,priority:2" json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (UserSessionFavorite) TableName() string { return "user_session_favorites" }
