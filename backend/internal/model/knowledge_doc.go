package model

import "time"

type KnowledgeDoc struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentID        int64     `gorm:"not null;index" json:"agent_id"`
	DocTitle       string    `gorm:"size:255" json:"doc_title"`
	ChunkText      string    `gorm:"type:text;not null" json:"chunk_text"`
	Embedding      []byte    `gorm:"type:bytea" json:"-"`
	EmbeddingModel string    `gorm:"size:50;default:text-embedding-3-small" json:"embedding_model"`
	SourceURL      string    `gorm:"size:500" json:"source_url"`
	CreatedAt      time.Time `json:"created_at"`
}

func (KnowledgeDoc) TableName() string { return "knowledge_docs" }
