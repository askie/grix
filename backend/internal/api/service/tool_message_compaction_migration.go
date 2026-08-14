package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/toolcard"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	toolMessageCompactionMigrationName = "compact_tool_execution_messages_v1"
	toolMessageCompactionBatchSize     = 500
)

type toolMessageCompactionRow struct {
	MsgID     int64          `gorm:"column:msg_id"`
	SessionID string         `gorm:"column:session_id"`
	Content   string         `gorm:"column:content"`
	Extra     datatypes.JSON `gorm:"column:extra"`
}

type toolMessageCompactionStats struct {
	Scanned   int
	Updated   int
	Unchanged int
}

// RunToolMessageCompactionMigration removes historical provider payloads and
// duplicated tool card fields from messages. The migration is restart-safe:
// rows are compacted idempotently in bounded batches, and the completion
// marker is written only after every candidate row has been processed.
func RunToolMessageCompactionMigration(ctx context.Context) error {
	if store.DB == nil {
		return fmt.Errorf("tool message compaction migration: database is nil")
	}
	stats, applied, err := runToolMessageCompactionMigration(ctx, store.DB)
	if err != nil {
		return err
	}
	if logger.L != nil {
		if applied {
			logger.L.Infof(
				"tool message compaction migration applied: scanned=%d updated=%d unchanged=%d",
				stats.Scanned,
				stats.Updated,
				stats.Unchanged,
			)
		} else {
			logger.L.Infof("tool message compaction migration already applied: %s", toolMessageCompactionMigrationName)
		}
	}
	return nil
}

func runToolMessageCompactionMigration(
	ctx context.Context,
	db *gorm.DB,
) (toolMessageCompactionStats, bool, error) {
	var stats toolMessageCompactionStats
	if db == nil {
		return stats, false, fmt.Errorf("tool message compaction migration: database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ensureDataMigrationTable(ctx, db); err != nil {
		return stats, false, fmt.Errorf("ensure data migration table: %w", err)
	}
	applied, err := isDataMigrationApplied(ctx, db, toolMessageCompactionMigrationName)
	if err != nil {
		return stats, false, fmt.Errorf("check tool message compaction migration: %w", err)
	}
	if applied {
		return stats, false, nil
	}

	var lastMsgID int64
	var lastSessionID string
	for {
		var rows []toolMessageCompactionRow
		query := db.WithContext(ctx).
			Table("messages").
			Select("msg_id", "session_id", "content", "extra").
			Where("content LIKE ?", "%grix://card/tool_execution%")
		if lastMsgID != 0 || lastSessionID != "" {
			query = query.Where(
				"(msg_id > ?) OR (msg_id = ? AND session_id > ?)",
				lastMsgID,
				lastMsgID,
				lastSessionID,
			)
		}
		if err := query.
			Order("msg_id ASC").
			Order("session_id ASC").
			Limit(toolMessageCompactionBatchSize).
			Find(&rows).Error; err != nil {
			return stats, false, fmt.Errorf("load historical tool messages: %w", err)
		}
		if len(rows) == 0 {
			break
		}

		for _, row := range rows {
			stats.Scanned++
			lastMsgID = row.MsgID
			lastSessionID = row.SessionID

			compactContent, compactExtra, ok := toolcard.CompactHistoricalForStorage(
				row.Content,
				json.RawMessage(row.Extra),
			)
			if !ok {
				stats.Unchanged++
				continue
			}
			if compactContent == row.Content &&
				bytes.Equal(bytes.TrimSpace(compactExtra), bytes.TrimSpace(row.Extra)) {
				stats.Unchanged++
				continue
			}

			result := db.WithContext(ctx).
				Table("messages").
				Where("msg_id = ? AND session_id = ? AND content = ?", row.MsgID, row.SessionID, row.Content).
				Updates(map[string]any{
					"content": compactContent,
					"extra":   datatypes.JSON(compactExtra),
				})
			if result.Error != nil {
				return stats, false, fmt.Errorf(
					"compact historical tool message msg_id=%d session_id=%s: %w",
					row.MsgID,
					row.SessionID,
					result.Error,
				)
			}
			if result.RowsAffected != 1 {
				return stats, false, fmt.Errorf(
					"historical tool message changed during migration msg_id=%d session_id=%s",
					row.MsgID,
					row.SessionID,
				)
			}
			stats.Updated++
		}
	}

	if err := markDataMigrationApplied(ctx, db, toolMessageCompactionMigrationName); err != nil {
		return stats, false, fmt.Errorf("mark tool message compaction migration applied: %w", err)
	}
	return stats, true, nil
}
