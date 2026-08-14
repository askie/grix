package model

import "time"

type Egg struct {
	ID               string     `gorm:"primaryKey;size:128" json:"id"`
	CategoryID       string     `gorm:"size:64;not null;index:idx_eggs_category_status,priority:1" json:"category_id"`
	PackageType      string     `gorm:"size:32;not null" json:"package_type"`
	TargetClientType string     `gorm:"size:32;not null" json:"target_client_type"`
	HasPersonaZip    bool       `gorm:"not null;default:false" json:"has_persona_zip"`
	HasSkillZip      bool       `gorm:"not null;default:false" json:"has_skill_zip"`
	SkillTargetType  string     `gorm:"size:32;not null;default:''" json:"skill_target_type"`
	DefaultColor     string     `gorm:"size:16;not null;default:#D97706" json:"default_color"`
	DefaultEmoji     string     `gorm:"size:16;not null;default:🌍" json:"default_emoji"`
	Status           string     `gorm:"size:16;not null;default:draft;index:idx_eggs_category_status,priority:2" json:"status"`
	InstallCount     int64      `gorm:"not null;default:0" json:"install_count"`
	PinnedAt         *time.Time `gorm:"index:idx_eggs_pinned_at" json:"pinned_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (Egg) TableName() string { return "eggs" }

type EggI18n struct {
	EggID                string    `gorm:"primaryKey;size:128;index:idx_egg_i18n_locale_egg,priority:2" json:"egg_id"`
	Locale               string    `gorm:"primaryKey;size:16;index:idx_egg_i18n_locale_egg,priority:1" json:"locale"`
	Name                 string    `gorm:"size:128;not null" json:"name"`
	Description          string    `gorm:"type:text;not null;default:''" json:"description"`
	Vibe                 string    `gorm:"size:128;not null;default:''" json:"vibe"`
	SearchTextNormalized string    `gorm:"type:text;not null;default:''" json:"search_text_normalized"`
	SearchTSV            string    `gorm:"type:tsvector" json:"search_tsv"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (EggI18n) TableName() string { return "egg_i18n" }
