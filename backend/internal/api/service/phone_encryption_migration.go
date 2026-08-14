package service

import (
	"context"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/store"
)

// RunPhoneEncryptionMigration 把存量明文手机号迁移为加密存储（配合 migration 083）。
// 做三件事，全部幂等、可重复跑：
//  1. 备份：把当前 users 明文手机号快照进 _backup_phone_083，便于回滚（验证无误后应手动 DROP）。
//  2. users：对仍有明文 phone_e164 且未生成盲索引的行，写入密文/末4/盲索引，并把 phone_e164 置 NULL。
//  3. user_identities：把 phone_sms_* 行里仍是明文(+开头)的 external_id 改成盲索引。
//
// 新注册/绑定走的是已加密的写路径，不在本迁移范围；本迁移只处理改造前的存量数据。
func RunPhoneEncryptionMigration(ctx context.Context) error {
	db := store.DB.WithContext(ctx)

	// 1. 备份明文（仅缺失时插入；保留真实号以便回滚，验证后请手动 DROP TABLE _backup_phone_083）
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS _backup_phone_083 (user_id BIGINT PRIMARY KEY, phone_e164 VARCHAR(20))`).Error; err != nil {
		return err
	}
	if err := db.Exec(`INSERT INTO _backup_phone_083 (user_id, phone_e164)
		SELECT id, phone_e164 FROM users
		WHERE phone_e164 IS NOT NULL AND phone_e164 <> ''
		ON CONFLICT (user_id) DO NOTHING`).Error; err != nil {
		return err
	}

	// 2. users 明文 → 密文/末4/盲索引
	usersMigrated := 0
	for {
		var rows []model.User
		if err := db.Select("id", "phone_e164").
			Where("phone_e164 IS NOT NULL AND phone_e164 <> '' AND (phone_blind IS NULL OR phone_blind = '')").
			Limit(500).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, u := range rows {
			cipher, err := secretcrypto.Encrypt(u.PhoneE164)
			if err != nil {
				return err
			}
			if err := db.Model(&model.User{}).Where("id = ?", u.ID).Updates(map[string]any{
				"phone_cipher": cipher,
				"phone_last4":  secretcrypto.Hint(u.PhoneE164),
				"phone_blind":  secretcrypto.BlindIndex(u.PhoneE164),
				"phone_e164":   nil,
			}).Error; err != nil {
				return err
			}
			usersMigrated++
		}
	}

	// 3. user_identities 明文 external_id → 盲索引
	identitiesMigrated := 0
	for {
		var rows []model.UserIdentity
		if err := db.Select("id", "external_id").
			Where("provider IN ? AND external_id LIKE ?",
				[]string{model.IdentityProviderPhoneSmsCN, model.IdentityProviderPhoneSmsGlobal}, "+%").
			Limit(500).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if !strings.HasPrefix(row.ExternalID, "+") {
				continue
			}
			if err := db.Model(&model.UserIdentity{}).Where("id = ?", row.ID).
				Update("external_id", secretcrypto.BlindIndex(row.ExternalID)).Error; err != nil {
				return err
			}
			identitiesMigrated++
		}
	}

	if usersMigrated > 0 || identitiesMigrated > 0 {
		logger.L.Infof("phone encryption migration done: users=%d identities=%d (备份表 _backup_phone_083 验证无误后请手动 DROP)",
			usersMigrated, identitiesMigrated)
	}
	return nil
}
