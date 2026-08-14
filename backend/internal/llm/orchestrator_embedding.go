package llm

import (
	"context"
	"encoding/json"
	"time"

	"github.com/askie/grix/backend/internal/llm/rag"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/nats-io/nats.go"
)

func (o *Orchestrator) publishEmbeddingTask(msgID int64, sessionID, content string) {
	data, _ := json.Marshal(map[string]interface{}{
		"msg_id":     msgID,
		"session_id": sessionID,
		"content":    content,
	})
	if store.JS != nil {
		store.JS.Publish("ai.embedding.generate", data)
	}
}

// EmbeddingWorker processes embedding generation tasks from NATS.
func (o *Orchestrator) StartEmbeddingWorker(ctx context.Context) {
	handler := func(msg *nats.Msg) {
		done := make(chan struct{})
		go func() {
			ticker := time.NewTicker(embeddingInProgressInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := msg.InProgress(); err != nil {
						logger.L.Warnf("embedding in-progress ack error: %v", err)
					}
				case <-done:
					return
				}
			}
		}()
		defer close(done)

		var task struct {
			MsgID     int64  `json:"msg_id"`
			SessionID string `json:"session_id"`
			Content   string `json:"content"`
		}
		if err := json.Unmarshal(msg.Data, &task); err != nil {
			logger.L.Errorf("embedding task unmarshal error: %v", err)
			if err := msg.Nak(); err != nil {
				logger.L.Warnf("embedding task nak error (unmarshal): %v", err)
			}
			return
		}

		var count int64
		store.DB.Raw("SELECT COUNT(*) FROM memory_embeddings WHERE msg_id = ?", task.MsgID).Scan(&count)
		if count > 0 {
			if err := msg.Ack(); err != nil {
				logger.L.Warnf("embedding task ack error (already processed): %v", err)
			}
			return
		}

		chunks := rag.ChunkText(task.Content, 400, 50)
		for _, chunk := range chunks {
			embResult, err := rag.GenerateEmbedding(ctx, chunk.Text)
			if err != nil {
				logger.L.Errorf("embedding generation error for msg %d: %v", task.MsgID, err)
				if err := msg.Nak(); err != nil {
					logger.L.Warnf("embedding task nak error: %v", err)
				}
				return
			}

			embedding := model.MemoryEmbedding{
				SessionID:   task.SessionID,
				MsgID:       task.MsgID,
				ChunkIndex:  int16(chunk.Index),
				ContentText: chunk.Text,
				Embedding:   rag.Float32ToBytes(embResult.Embedding),
			}
			store.DB.Create(&embedding)
		}

		if err := msg.Ack(); err != nil {
			logger.L.Warnf("embedding task ack error: %v", err)
		}
	}

	sub, err := store.JS.QueueSubscribe("ai.embedding.generate", embeddingQueueGroup, handler,
		nats.ManualAck(),
		nats.Durable(embeddingDurable),
		nats.DeliverNew(),
		nats.AckWait(embeddingAckWait),
		nats.MaxDeliver(5),
	)
	if err != nil {
		logger.L.Errorf("failed to subscribe embedding worker: %v", err)
		return
	}
	if info, infoErr := sub.ConsumerInfo(); infoErr != nil {
		logger.L.Warnf("load embedding consumer info error: %v", infoErr)
	} else {
		logger.L.Infof(
			"embedding consumer ready stream=%s durable=%s deliver_policy=%s ack_wait=%s pending=%d ack_pending=%d redelivered=%d",
			info.Stream,
			info.Name,
			info.Config.DeliverPolicy,
			info.Config.AckWait,
			info.NumPending,
			info.NumAckPending,
			info.NumRedelivered,
		)
	}

	<-ctx.Done()
	if err := sub.Unsubscribe(); err != nil {
		logger.L.Warnf("embedding consumer unsubscribe error: %v", err)
	}
}
