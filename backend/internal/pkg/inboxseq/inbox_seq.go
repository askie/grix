package inboxseq

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var nextUserInboxSeqScript = redis.NewScript(`
local key = KEYS[1]
local floor = tonumber(ARGV[1]) or 0
local current = tonumber(redis.call("GET", key) or "0")
if current < floor then
  redis.call("SET", key, floor)
end
return redis.call("INCR", key)
`)

type userInboxSeqSnapshot struct {
	UserID      int64 `gorm:"column:user_id"`
	MaxInboxSeq int64 `gorm:"column:max_inbox_seq"`
}

func normalizePositiveUserIDs(userIDs []int64) []int64 {
	if len(userIDs) == 0 {
		return nil
	}

	normalized := make([]int64, 0, len(userIDs))
	seen := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		normalized = append(normalized, userID)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i] < normalized[j]
	})
	return normalized
}

func lockOwnersTx(
	ctx context.Context,
	tx *gorm.DB,
	userIDs []int64,
) error {
	if tx == nil {
		return errors.New("inbox sequence create failed")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	normalizedUserIDs := normalizePositiveUserIDs(userIDs)
	if len(normalizedUserIDs) == 0 {
		return errors.New("inbox sequence create failed")
	}

	if tx.Dialector != nil && tx.Dialector.Name() == "postgres" {
		for _, userID := range normalizedUserIDs {
			if err := tx.WithContext(ctx).Exec(
				"SELECT pg_advisory_xact_lock(?)",
				userID,
			).Error; err != nil {
				return err
			}
		}
		return nil
	}

	var users []model.User
	return tx.WithContext(ctx).
		Model(&model.User{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").
		Where("id IN ?", normalizedUserIDs).
		Order("id ASC").
		Find(&users).Error
}

func loadCurrentMaxByUserTx(
	ctx context.Context,
	tx *gorm.DB,
	userIDs []int64,
) (map[int64]int64, error) {
	if tx == nil || len(userIDs) == 0 {
		return nil, errors.New("inbox sequence create failed")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	result := make(map[int64]int64, len(userIDs))
	for _, userID := range userIDs {
		if userID > 0 {
			result[userID] = 0
		}
	}

	var rows []userInboxSeqSnapshot
	if err := tx.WithContext(ctx).
		Model(&model.UserInbox{}).
		Select("user_id, COALESCE(MAX(inbox_seq), 0) AS max_inbox_seq").
		Where("user_id IN ?", userIDs).
		Group("user_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	for _, row := range rows {
		if row.UserID <= 0 {
			continue
		}
		result[row.UserID] = row.MaxInboxSeq
	}
	return result, nil
}

func allocateFromDBTx(
	ctx context.Context,
	tx *gorm.DB,
	userIDs []int64,
	extraFloorByUser map[int64]int64,
) (map[int64]int64, error) {
	if tx == nil {
		return nil, errors.New("inbox sequence create failed")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	normalizedUserIDs := normalizePositiveUserIDs(userIDs)
	if len(normalizedUserIDs) == 0 {
		return nil, errors.New("inbox sequence create failed")
	}
	if err := lockOwnersTx(ctx, tx, normalizedUserIDs); err != nil {
		return nil, err
	}

	currentMaxByUser, err := loadCurrentMaxByUserTx(ctx, tx, normalizedUserIDs)
	if err != nil {
		return nil, err
	}

	// 有效 floor = max(DB 当前最大序号, 调用方传入的额外下限)。额外下限用于
	// 撤回 tombstone 必须严格大于原投递序号的语义；Redis 不可用降级到本路径时
	// 也要保证该语义成立。
	result := make(map[int64]int64, len(normalizedUserIDs))
	for _, userID := range normalizedUserIDs {
		floor := currentMaxByUser[userID]
		if extra := extraFloorByUser[userID]; extra > floor {
			floor = extra
		}
		result[userID] = floor + 1
	}
	return result, nil
}

func allocateFromRedisTx(
	ctx context.Context,
	tx *gorm.DB,
	userIDs []int64,
	currentMaxByUser map[int64]int64,
) (map[int64]int64, error) {
	if tx == nil || len(userIDs) == 0 {
		return nil, errors.New("inbox sequence create failed")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	normalizedUserIDs := normalizePositiveUserIDs(userIDs)
	if len(normalizedUserIDs) == 0 {
		return nil, errors.New("inbox sequence create failed")
	}

	result := make(map[int64]int64, len(normalizedUserIDs))
	if store.RDB == nil {
		for _, userID := range normalizedUserIDs {
			result[userID] = currentMaxByUser[userID] + 1
		}
		return result, nil
	}

	pipe := store.RDB.Pipeline()
	cmds := make(map[int64]*redis.Cmd, len(normalizedUserIDs))
	for _, userID := range normalizedUserIDs {
		cmds[userID] = nextUserInboxSeqScript.Eval(
			ctx,
			pipe,
			[]string{fmt.Sprintf("im:inbox_seq:%d", userID)},
			currentMaxByUser[userID],
		)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}

	for _, userID := range normalizedUserIDs {
		seq, err := cmds[userID].Int64()
		if err != nil {
			return nil, err
		}
		if seq <= currentMaxByUser[userID] {
			return nil, fmt.Errorf(
				"inbox sequence regression user=%d current_max=%d next=%d",
				userID,
				currentMaxByUser[userID],
				seq,
			)
		}
		result[userID] = seq
	}
	return result, nil
}

func AllocateNextBatchTx(
	ctx context.Context,
	tx *gorm.DB,
	userIDs []int64,
) (map[int64]int64, error) {
	return AllocateNextBatchWithFloorTx(ctx, tx, userIDs, nil)
}

// AllocateNextBatchWithFloorTx 批量为多个用户分配下一个 inbox_seq，并保证每个用户
// 分配到的序号严格大于 extraFloorByUser[user]（传 nil 表示无额外下限）。
//
// 全系统所有 inbox_seq 发号都应经由此入口（及其包装 AllocateNextBatchTx / NextTx），
// 统一走 Redis 单一原子计数器，物理上消除跨路径撞号；DB 仅用于计算 floor 与 Redis
// 不可用时的降级兜底（此时由 advisory lock 串行化）。extraFloorByUser 用于撤回
// tombstone 等场景：墓碑序号必须大于该消息原投递序号，即使原投递行已被删除导致
// DB 当前最大值回退，也由该下限兜住。
func AllocateNextBatchWithFloorTx(
	ctx context.Context,
	tx *gorm.DB,
	userIDs []int64,
	extraFloorByUser map[int64]int64,
) (map[int64]int64, error) {
	if tx == nil {
		return nil, errors.New("inbox sequence create failed")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	normalizedUserIDs := normalizePositiveUserIDs(userIDs)
	if len(normalizedUserIDs) == 0 {
		return nil, errors.New("inbox sequence create failed")
	}
	if store.RDB == nil {
		return allocateFromDBTx(ctx, tx, normalizedUserIDs, extraFloorByUser)
	}

	currentMaxByUser, err := loadCurrentMaxByUserTx(ctx, tx, normalizedUserIDs)
	if err == nil {
		// Redis 脚本以 floor 为下限再 INCR；传入 max(DB 当前最大, 额外下限)。
		effectiveFloorByUser := mergeInboxSeqFloors(currentMaxByUser, extraFloorByUser)
		nextSeqByUser, nextErr := allocateFromRedisTx(
			ctx,
			tx,
			normalizedUserIDs,
			effectiveFloorByUser,
		)
		if nextErr == nil {
			return nextSeqByUser, nil
		}
		logWarnf(
			"user inbox seq redis allocation failed, fallback to db user_ids=%v: %v",
			normalizedUserIDs,
			nextErr,
		)
	} else {
		logWarnf(
			"user inbox seq pg snapshot failed before redis allocation user_ids=%v: %v",
			normalizedUserIDs,
			err,
		)
	}

	return allocateFromDBTx(ctx, tx, normalizedUserIDs, extraFloorByUser)
}

// mergeInboxSeqFloors 返回每个用户的有效 floor = max(DB 当前最大序号, 额外下限)。
func mergeInboxSeqFloors(currentMaxByUser, extraFloorByUser map[int64]int64) map[int64]int64 {
	merged := make(map[int64]int64, len(currentMaxByUser))
	for userID, maxSeq := range currentMaxByUser {
		merged[userID] = maxSeq
	}
	for userID, floor := range extraFloorByUser {
		if floor > merged[userID] {
			merged[userID] = floor
		}
	}
	return merged
}

func NextTx(ctx context.Context, tx *gorm.DB, userID int64) (int64, error) {
	nextSeqByUser, err := AllocateNextBatchTx(ctx, tx, []int64{userID})
	if err != nil {
		return 0, err
	}
	return nextSeqByUser[userID], nil
}

func logWarnf(template string, args ...any) {
	if logger.L != nil {
		logger.L.Warnf(template, args...)
	}
}
