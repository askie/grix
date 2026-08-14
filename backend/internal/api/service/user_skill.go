package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// 自定义技能多机器同步的业务逻辑（docs/architecture/38）。
// 平台只做存储与分发：按 owner 维度增删改查技能包，供 connector 拉取同步。
// 不解析技能内容、不校验语义、不参与调用。

const (
	skillNameMaxRunes  = 100
	skillContentMaxLen = 256 * 1024 // 256KB，技能包全文上限
)

// SkillSummary 是技能列表项（不含正文，供 connector 比对版本/摘要做增量拉取）。
type SkillSummary struct {
	ID        int64  `json:"id,string"`
	OwnerID   int64  `json:"owner_id,string"`
	Name      string `json:"name"`
	Version   int64  `json:"version,string"`
	Digest    string `json:"digest"`
	UpdatedAt int64  `json:"updated_at"` // 毫秒时间戳
}

// isSkillDuplicateKeyErr 判断落库错误是否为 (owner_id, name) 唯一索引冲突。
// gorm.Open 未启用 TranslateError（全局开启影响面大），Postgres 下原始错误是
// *pgconn.PgError(23505)，不会是 gorm.ErrDuplicatedKey；这里对 gorm 翻译错误、
// Postgres 原生错误、SQLite（测试环境）三种形态都识别。
func isSkillDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func skillDigest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// notifySkillLibraryChanged 在技能库落库成功后广播变更（docs/architecture/38 §6.2）：
// 全体 ws 节点收到后向该 owner 的在线主连接下发 skill_sync，connector 立即拉取同步。
// 广播失败只影响时效（connector 定时轮询兜底），不影响落库结果，故不回传错误。
func notifySkillLibraryChanged(ownerID int64, name string, version int64) {
	publishBroadcastEvent(protocol.RedisCmdSkillLibraryChanged, protocol.SkillLibraryChangedPayload{
		OwnerID: ownerID,
		Name:    name,
		Version: version,
	})
}

// skillNameSafe 拒绝会污染文件路径的技能名。平台是技能扇出到 owner 所有机器的
// 唯一入口，name 在各机器上会变成 grix/skills/<name> 目录名，必须在此堵死路径穿越
//（connector 侧 SkillSyncer 另有净化，此处是纵深防御的第一道）。
func skillNameSafe(name string) bool {
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// validateSkillInput 校验并归一化技能名与内容（平台不校验内容语义，只做基本边界）。
func validateSkillInput(name, content string) (string, string, *errcode.ErrCode) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", &errcode.ErrSkillNameRequired
	}
	if len([]rune(name)) > skillNameMaxRunes {
		return "", "", &errcode.ErrSkillNameTooLong
	}
	if !skillNameSafe(name) {
		return "", "", &errcode.ErrSkillNameInvalid
	}
	// grix- 前缀为平台内置技能保留前缀，任何用户/connector 都不可上传同名技能，
	// 防止把 connector/hermes 内置技能误传进用户库（docs/architecture/39 §8 风险②）。
	if strings.HasPrefix(strings.ToLower(name), "grix-") {
		return "", "", &errcode.ErrSkillNameReserved
	}
	if strings.TrimSpace(content) == "" {
		return "", "", &errcode.ErrSkillContentEmpty
	}
	if len(content) > skillContentMaxLen {
		return "", "", &errcode.ErrSkillContentTooLarge
	}
	return name, content, nil
}

// ListUserSkills 返回本 owner 的技能 + 系统内置技能（owner_id=0）摘要，按名排序。
// 同名时 owner 自有技能遮蔽（shadow）系统内置：列表按名去重、owner 优先——
// connector 按 name 落 grix/skills/<name> 目录，同名双条会让写盘顺序决定内容。
func ListUserSkills(ownerID int64) ([]SkillSummary, *errcode.ErrCode) {
	var skills []model.UserSkill
	if err := store.DB.
		Select("id", "owner_id", "name", "version", "digest", "updated_at").
		Where("owner_id IN (?, 0)", ownerID).
		Order("owner_id DESC, name ASC").
		Find(&skills).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	seen := make(map[string]bool, len(skills))
	out := make([]SkillSummary, 0, len(skills))
	for _, s := range skills {
		if seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		out = append(out, SkillSummary{
			ID:        s.ID,
			OwnerID:   s.OwnerID,
			Name:      s.Name,
			Version:   s.Version,
			Digest:    s.Digest,
			UpdatedAt: s.UpdatedAt.UnixMilli(),
		})
	}
	return out, nil
}

// GetUserSkillContent 读取单个技能全文（含正文）；系统内置技能对所有 owner 可读。
func GetUserSkillContent(ownerID, id int64) (*model.UserSkill, *errcode.ErrCode) {
	var skill model.UserSkill
	if err := store.DB.
		Where("id = ? AND owner_id IN (?, 0)", id, ownerID).
		First(&skill).Error; err != nil {
		return nil, &errcode.ErrSkillNotFound
	}
	return &skill, nil
}

// GetUserSkillByName 按名读取本 owner（或系统内置）技能全文；用于对话式 skill_get。
func GetUserSkillByName(ownerID int64, name string) (*model.UserSkill, *errcode.ErrCode) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, &errcode.ErrSkillNameRequired
	}
	var skill model.UserSkill
	if err := store.DB.
		Where("owner_id IN (?, 0) AND name = ?", ownerID, name).
		Order("owner_id DESC").
		First(&skill).Error; err != nil {
		return nil, &errcode.ErrSkillNotFound
	}
	return &skill, nil
}

// CreateUserSkill 新建本 owner 的技能；同名已存在报错。
func CreateUserSkill(ownerID int64, name, content string) (*model.UserSkill, *errcode.ErrCode) {
	name, content, ec := validateSkillInput(name, content)
	if ec != nil {
		return nil, ec
	}
	var count int64
	store.DB.Model(&model.UserSkill{}).Where("owner_id = ? AND name = ?", ownerID, name).Count(&count)
	if count > 0 {
		return nil, &errcode.ErrSkillNameExists
	}
	skill := model.UserSkill{
		ID:      snowflake.GenID(),
		OwnerID: ownerID,
		Name:    name,
		Content: content,
		Version: 1,
		Digest:  skillDigest(content),
	}
	if err := store.DB.Create(&skill).Error; err != nil {
		// check-then-insert 与并发插入间的竞态由唯一索引兜底，映射回同名冲突而非 500。
		if isSkillDuplicateKeyErr(err) {
			return nil, &errcode.ErrSkillNameExists
		}
		return nil, &errcode.ErrInternal
	}
	notifySkillLibraryChanged(ownerID, skill.Name, skill.Version)
	return &skill, nil
}

// UpdateUserSkill 更新本 owner 的技能（按 id）；系统内置不可改，版本自增。
func UpdateUserSkill(ownerID, id int64, name, content string) (*model.UserSkill, *errcode.ErrCode) {
	name, content, ec := validateSkillInput(name, content)
	if ec != nil {
		return nil, ec
	}
	var skill model.UserSkill
	if err := store.DB.Where("id = ?", id).First(&skill).Error; err != nil {
		return nil, &errcode.ErrSkillNotFound
	}
	if skill.OwnerID == 0 {
		return nil, &errcode.ErrSkillSystemReadonly
	}
	if skill.OwnerID != ownerID {
		return nil, &errcode.ErrSkillNotFound
	}
	// 幂等：名字与内容都没变则直接返回，不 bump 版本、不重写——避免 connector
	// 重复上载同一技能导致 version 无谓膨胀（审查建议）。
	if name == skill.Name && skill.Digest == skillDigest(content) {
		return &skill, nil
	}
	// 改名撞其它同名技能则拒绝。
	if name != skill.Name {
		var count int64
		store.DB.Model(&model.UserSkill{}).
			Where("owner_id = ? AND name = ? AND id <> ?", ownerID, name, id).Count(&count)
		if count > 0 {
			return nil, &errcode.ErrSkillNameExists
		}
	}
	skill.Name = name
	skill.Content = content
	skill.Digest = skillDigest(content)
	skill.Version++
	if err := store.DB.Model(&model.UserSkill{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"name":    skill.Name,
			"content": skill.Content,
			"digest":  skill.Digest,
			"version": skill.Version,
		}).Error; err != nil {
		// 改名的 check-then-update 竞态同样由唯一索引兜底。
		if isSkillDuplicateKeyErr(err) {
			return nil, &errcode.ErrSkillNameExists
		}
		return nil, &errcode.ErrInternal
	}
	notifySkillLibraryChanged(ownerID, skill.Name, skill.Version)
	return &skill, nil
}

// DeleteUserSkill 删除本 owner 的技能（按 id）；系统内置不可删。
func DeleteUserSkill(ownerID, id int64) *errcode.ErrCode {
	var skill model.UserSkill
	if err := store.DB.Where("id = ?", id).First(&skill).Error; err != nil {
		return &errcode.ErrSkillNotFound
	}
	if skill.OwnerID == 0 {
		return &errcode.ErrSkillSystemReadonly
	}
	if skill.OwnerID != ownerID {
		return &errcode.ErrSkillNotFound
	}
	if err := store.DB.Delete(&model.UserSkill{}, "id = ?", id).Error; err != nil {
		return &errcode.ErrInternal
	}
	notifySkillLibraryChanged(ownerID, skill.Name, skill.Version)
	return nil
}

// UpsertUserSkillByName 按名新建或更新本 owner 的技能（幂等）。
// 用于 connector 上载本地技能、以及对话式配置 skill_set——同名即更新、版本自增。
func UpsertUserSkillByName(ownerID int64, name, content string) (*model.UserSkill, *errcode.ErrCode) {
	name, content, ec := validateSkillInput(name, content)
	if ec != nil {
		return nil, ec
	}
	var skill model.UserSkill
	err := store.DB.Where("owner_id = ? AND name = ?", ownerID, name).First(&skill).Error
	if err == gorm.ErrRecordNotFound {
		return CreateUserSkill(ownerID, name, content)
	}
	if err != nil {
		return nil, &errcode.ErrInternal
	}
	return UpdateUserSkill(ownerID, skill.ID, name, content)
}

// DeleteUserSkillByName 按名删除本 owner 的技能；不存在视为已删除（幂等）。
func DeleteUserSkillByName(ownerID int64, name string) *errcode.ErrCode {
	name = strings.TrimSpace(name)
	if name == "" {
		return &errcode.ErrSkillNameRequired
	}
	res := store.DB.
		Where("owner_id = ? AND name = ?", ownerID, name).
		Delete(&model.UserSkill{})
	if res.Error != nil {
		return &errcode.ErrInternal
	}
	// 幂等删除只在真的删掉了东西时才广播，重复删不打扰各机器。
	if res.RowsAffected > 0 {
		notifySkillLibraryChanged(ownerID, name, 0)
	}
	return nil
}
