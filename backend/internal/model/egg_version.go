package model

import (
	"time"

	"gorm.io/datatypes"
)

type EggVersion struct {
	EggID                string         `gorm:"primaryKey;size:128" json:"egg_id"`
	Version              int            `gorm:"primaryKey" json:"version"`
	ZipURL               string         `gorm:"type:text;not null" json:"zip_url"`
	ZipSHA256            string         `gorm:"size:128;not null" json:"zip_sha256"`
	ZipSize              int64          `gorm:"not null" json:"zip_size"`
	PersonaZipURL        string         `gorm:"type:text;not null;default:''" json:"persona_zip_url"`
	PersonaZipSHA256     string         `gorm:"size:128;not null;default:''" json:"persona_zip_sha256"`
	PersonaZipSize       int64          `gorm:"not null;default:0" json:"persona_zip_size"`
	SkillZipURL          string         `gorm:"type:text;not null;default:''" json:"skill_zip_url"`
	SkillZipSHA256       string         `gorm:"size:128;not null;default:''" json:"skill_zip_sha256"`
	SkillZipSize         int64          `gorm:"not null;default:0" json:"skill_zip_size"`
	ArtifactManifestJSON datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"artifact_manifest_json"`
	PublishedAt          *time.Time     `json:"published_at,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

func (EggVersion) TableName() string { return "egg_versions" }

type EggVersionI18n struct {
	EggID       string    `gorm:"primaryKey;size:128" json:"egg_id"`
	Version     int       `gorm:"primaryKey" json:"version"`
	Locale      string    `gorm:"primaryKey;size:16" json:"locale"`
	VersionDesc string    `gorm:"type:text;not null;default:''" json:"version_desc"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (EggVersionI18n) TableName() string { return "egg_version_i18n" }
