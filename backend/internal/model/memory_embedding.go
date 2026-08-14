package model

import "time"

type MemoryEmbedding struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID      string    `gorm:"size:50;not null;index" json:"session_id"`
	MsgID          int64     `gorm:"not null" json:"msg_id"`
	ChunkIndex     int16     `gorm:"default:0" json:"chunk_index"`
	ContentText    string    `gorm:"type:text;not null" json:"content_text"`
	Embedding      []byte    `gorm:"type:bytea" json:"-"` // pgvector stored as bytea for GORM compatibility
	EmbeddingModel string    `gorm:"size:50;default:text-embedding-3-small" json:"embedding_model"`
	CreatedAt      time.Time `json:"created_at"`
}

func (MemoryEmbedding) TableName() string { return "memory_embeddings" }
