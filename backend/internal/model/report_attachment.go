package model

import "time"

type ReportAttachment struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id,string"`
	ReportID  int64     `gorm:"not null;index" json:"report_id,string"`
	SlotNo    int16     `gorm:"not null" json:"slot_no"`
	ObjectKey string    `gorm:"size:512;not null" json:"object_key"`
	MimeType  string    `gorm:"size:64;not null" json:"mime_type"`
	SizeBytes int64     `gorm:"not null;default:0" json:"size_bytes"`
	SHA256    string    `gorm:"size:64;not null;default:''" json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
}

func (ReportAttachment) TableName() string { return "report_attachments" }
