package model

import "time"

type EggCategory struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	Code      string    `gorm:"size:64;not null;uniqueIndex" json:"code"`
	Status    string    `gorm:"size:16;not null;default:active" json:"status"`
	SortOrder int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (EggCategory) TableName() string { return "egg_categories" }

type EggCategoryI18n struct {
	CategoryID  string    `gorm:"primaryKey;size:64" json:"category_id"`
	Locale      string    `gorm:"primaryKey;size:16" json:"locale"`
	Name        string    `gorm:"size:128;not null" json:"name"`
	Description string    `gorm:"type:text;not null;default:''" json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (EggCategoryI18n) TableName() string { return "egg_category_i18n" }
