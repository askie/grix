package rag

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/askie/grix/backend/internal/store"
)

// RetrievalResult represents a retrieved memory chunk.
type RetrievalResult struct {
	ContentText string
	MsgID       int64
	Similarity  float64
}

// Retrieve searches memory_embeddings for relevant context.
func Retrieve(
	ctx context.Context,
	sessionID string,
	userID int64,
	queryText string,
	topK int,
) ([]RetrievalResult, error) {
	// Generate query embedding
	embResult, err := GenerateEmbedding(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("generate query embedding: %w", err)
	}

	// Convert float32 slice to pgvector format string
	vecStr := float32SliceToVectorString(embResult.Embedding)

	var results []RetrievalResult
	query := `
		SELECT me.content_text, me.msg_id, 1 - (me.embedding <=> ?::vector) AS similarity
		FROM memory_embeddings me
		JOIN messages m ON m.msg_id = me.msg_id AND m.session_id = me.session_id
		LEFT JOIN session_history_resets shr ON shr.session_id = me.session_id AND shr.user_id = ?
		WHERE me.session_id = ?
		  AND (shr.deleted_before IS NULL OR m.created_at > shr.deleted_before)
		ORDER BY me.embedding <=> ?::vector
		LIMIT ?
	`
	rows, err := store.DB.Raw(query, vecStr, userID, sessionID, vecStr, topK).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var r RetrievalResult
		if err := rows.Scan(&r.ContentText, &r.MsgID, &r.Similarity); err != nil {
			continue
		}
		results = append(results, r)
	}

	return results, nil
}

// RetrieveKnowledge searches knowledge_docs for an agent.
func RetrieveKnowledge(ctx context.Context, agentID int64, queryText string, topK int) ([]RetrievalResult, error) {
	embResult, err := GenerateEmbedding(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("generate query embedding: %w", err)
	}

	vecStr := float32SliceToVectorString(embResult.Embedding)

	var results []RetrievalResult
	rows, err := store.DB.Raw(`
		SELECT chunk_text, id, 1 - (embedding <=> ?::vector) AS similarity
		FROM knowledge_docs
		WHERE agent_id = ?
		ORDER BY embedding <=> ?::vector
		LIMIT ?
	`, vecStr, agentID, vecStr, topK).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var r RetrievalResult
		if err := rows.Scan(&r.ContentText, &r.MsgID, &r.Similarity); err != nil {
			continue
		}
		results = append(results, r)
	}

	return results, nil
}

func float32SliceToVectorString(v []float32) string {
	s := "["
	for i, f := range v {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf("%g", f)
	}
	s += "]"
	return s
}

// Float32ToBytes converts a float32 slice to bytes for storage.
func Float32ToBytes(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}
