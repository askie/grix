package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	eggInstallChatStatusRunning = "running"
	eggInstallChatStatusSuccess = "success"
	eggInstallChatStatusFailed  = "failed"

	eggInstallStatusCardType = "egg_install_status"

	eggInstallStepCompleted = "completed"

	eggInstallErrorChatResultInvalid   = "chat_result_invalid"
	eggInstallErrorTargetMissing       = "install_target_missing"
	eggInstallErrorTargetMismatch      = "install_target_mismatch"
	eggInstallErrorTargetUnavailable   = "install_target_unavailable"
	eggInstallErrorTargetOwnerMismatch = "install_target_owner_mismatch"
)

var standaloneGrixCardMarkdownLinkPattern = regexp.MustCompile(`^\s*\[(.*)\]\((grix://card/[^)\s]+)\)\s*$`)

type eggInstallChatStatusSignal struct {
	InstallID     string
	Status        string
	Step          string
	Summary       string
	DetailText    string
	TargetAgentID *int64
	ErrorCode     string
	ErrorMsg      string
}

func ReconcileEggInstallChatStatus(sessionID string, senderID int64, senderType int16, content string) error {
	if store.DB == nil || senderType != 2 || senderID <= 0 {
		return nil
	}

	signal, ok := parseEggInstallChatStatusSignal(content)
	if !ok {
		return nil
	}

	return store.DB.Transaction(func(tx *gorm.DB) error {
		return reconcileEggInstallChatStatusTx(tx, strings.TrimSpace(sessionID), senderID, signal)
	})
}

func parseEggInstallChatStatusSignal(content string) (*eggInstallChatStatusSignal, bool) {
	href, ok := extractStandaloneGrixCardHref(content)
	if !ok {
		return nil, false
	}

	parsed, err := url.Parse(href)
	if err != nil || parsed == nil {
		return nil, false
	}
	if parsed.Scheme != "grix" || parsed.Host != "card" {
		return nil, false
	}
	if strings.Trim(strings.TrimSpace(parsed.Path), "/") != eggInstallStatusCardType {
		return nil, false
	}

	params := parsed.Query()
	status := normalizeEggInstallChatStatus(params.Get("status"))
	if status == "" {
		return nil, false
	}

	installID := strings.TrimSpace(params.Get("install_id"))
	if installID == "" {
		return nil, false
	}

	targetAgentID, _ := optionalInt64Value(params.Get("target_agent_id"))
	signal := &eggInstallChatStatusSignal{
		InstallID:     installID,
		Status:        status,
		Step:          strings.TrimSpace(params.Get("step")),
		Summary:       strings.TrimSpace(params.Get("summary")),
		DetailText:    strings.TrimSpace(params.Get("detail_text")),
		TargetAgentID: targetAgentID,
		ErrorCode:     strings.TrimSpace(params.Get("error_code")),
		ErrorMsg:      strings.TrimSpace(params.Get("error_msg")),
	}
	return signal, true
}

func extractStandaloneGrixCardHref(content string) (string, bool) {
	normalized := strings.TrimSpace(content)
	if normalized == "" {
		return "", false
	}
	if strings.HasPrefix(normalized, "grix://card/") {
		return normalized, true
	}

	match := standaloneGrixCardMarkdownLinkPattern.FindStringSubmatch(normalized)
	if len(match) != 3 {
		return "", false
	}

	href := strings.TrimSpace(match[2])
	if href == "" {
		return "", false
	}
	return href, true
}

func normalizeEggInstallChatStatus(value any) string {
	switch strings.ToLower(strings.TrimSpace(stringValue(value))) {
	case eggInstallChatStatusRunning:
		return eggInstallChatStatusRunning
	case eggInstallChatStatusSuccess:
		return eggInstallChatStatusSuccess
	case eggInstallChatStatusFailed:
		return eggInstallChatStatusFailed
	default:
		return ""
	}
}

func reconcileEggInstallChatStatusTx(tx *gorm.DB, sessionID string, senderID int64, signal *eggInstallChatStatusSignal) error {
	if tx == nil || signal == nil || sessionID == "" {
		return nil
	}

	var install model.EggInstall
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("install_id = ?", signal.InstallID).
		First(&install).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	if install.SessionID != sessionID {
		return nil
	}
	if install.ExecutorAgentID == nil || *install.ExecutorAgentID != senderID {
		return nil
	}
	if install.Status == model.EggInstallStatusFailed {
		return nil
	}

	switch signal.Status {
	case eggInstallChatStatusRunning:
		return updateEggInstallRunningTx(tx, install, signal)
	case eggInstallChatStatusFailed:
		return updateEggInstallFailedTx(tx, install, signal)
	case eggInstallChatStatusSuccess:
		return updateEggInstallSuccessTx(tx, install, signal)
	default:
		return nil
	}
}

func updateEggInstallRunningTx(tx *gorm.DB, install model.EggInstall, signal *eggInstallChatStatusSignal) error {
	step := strings.TrimSpace(signal.Step)
	if step == "" {
		step = strings.TrimSpace(install.Step)
	}
	if step == "" {
		step = eggInstallStepChatReady
	}

	return tx.Model(&model.EggInstall{}).
		Where("install_id = ?", install.InstallID).
		Updates(map[string]any{
			"status":     model.EggInstallStatusRunning,
			"step":       step,
			"error_code": "",
			"error_msg":  "",
		}).Error
}

func updateEggInstallFailedTx(tx *gorm.DB, install model.EggInstall, signal *eggInstallChatStatusSignal) error {
	step := strings.TrimSpace(signal.Step)
	if step == "" {
		step = strings.TrimSpace(install.Step)
	}
	if step == "" {
		step = "failed"
	}

	errorCode := strings.TrimSpace(signal.ErrorCode)
	if errorCode == "" {
		errorCode = eggInstallErrorChatResultInvalid
	}
	errorMsg := strings.TrimSpace(signal.ErrorMsg)
	if errorMsg == "" {
		errorMsg = strings.TrimSpace(signal.Summary)
	}
	if errorMsg == "" {
		errorMsg = strings.TrimSpace(signal.DetailText)
	}
	if errorMsg == "" {
		errorMsg = "主 agent 回报安装失败"
	}

	return tx.Model(&model.EggInstall{}).
		Where("install_id = ?", install.InstallID).
		Updates(map[string]any{
			"status":     model.EggInstallStatusFailed,
			"step":       step,
			"error_code": errorCode,
			"error_msg":  errorMsg,
		}).Error
}

func updateEggInstallSuccessTx(tx *gorm.DB, install model.EggInstall, signal *eggInstallChatStatusSignal) error {
	if install.Status == model.EggInstallStatusSuccess && install.CounterApplied {
		return nil
	}

	targetAgentID, errorCode, errorMsg, err := resolveEggInstallSuccessTargetTx(tx, install, signal)
	if err != nil {
		return err
	}
	if errorCode != "" {
		return updateEggInstallFailedTx(tx, install, &eggInstallChatStatusSignal{
			InstallID: install.InstallID,
			Status:    eggInstallChatStatusFailed,
			Step:      signal.Step,
			ErrorCode: errorCode,
			ErrorMsg:  errorMsg,
		})
	}

	step := strings.TrimSpace(signal.Step)
	if step == "" {
		step = eggInstallStepCompleted
	}

	updates := map[string]any{
		"status":     model.EggInstallStatusSuccess,
		"step":       step,
		"error_code": "",
		"error_msg":  "",
	}
	if targetAgentID != nil && *targetAgentID > 0 {
		updates["target_agent_id"] = *targetAgentID
	}

	if err := tx.Model(&model.EggInstall{}).
		Where("install_id = ?", install.InstallID).
		Updates(updates).Error; err != nil {
		return err
	}

	if install.CounterApplied {
		return nil
	}
	if err := tx.Model(&model.Egg{}).
		Where("id = ?", install.EggID).
		Update("install_count", gorm.Expr("install_count + 1")).Error; err != nil {
		return err
	}
	return tx.Model(&model.EggInstall{}).
		Where("install_id = ? AND counter_applied = ?", install.InstallID, false).
		Update("counter_applied", true).Error
}

func resolveEggInstallSuccessTargetTx(
	tx *gorm.DB,
	install model.EggInstall,
	signal *eggInstallChatStatusSignal,
) (*int64, string, string, error) {
	candidate := install.TargetAgentID
	if signal.TargetAgentID != nil && *signal.TargetAgentID > 0 {
		if candidate != nil && *candidate > 0 && *candidate != *signal.TargetAgentID {
			return nil, eggInstallErrorTargetMismatch, "主 agent 回报的目标 agent 与安装单不一致", nil
		}
		candidate = signal.TargetAgentID
	}

	if candidate == nil || *candidate <= 0 {
		return nil, eggInstallErrorTargetMissing, "主 agent 没有回报最终目标 agent", nil
	}

	var agent model.Agent
	if err := tx.Select("id", "owner_id", "provider_type", "agent_client_type", "status").
		Where("id = ?", *candidate).
		First(&agent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, eggInstallErrorTargetUnavailable, "安装完成后目标 agent 不存在", nil
		}
		return nil, "", "", err
	}
	if agent.OwnerID != install.UserID {
		return nil, eggInstallErrorTargetOwnerMismatch, "安装完成后目标 agent 不属于当前用户", nil
	}
	if agent.ProviderType != model.AgentProviderAPI || agent.Status != model.AgentStatusActive {
		return nil, eggInstallErrorTargetUnavailable, "安装完成后目标 agent 不可用", nil
	}

	var version model.EggVersion
	if err := tx.Select("egg_id", "version", "persona_zip_url", "skill_zip_url").
		Where("egg_id = ? AND version = ?", install.EggID, install.Version).
		First(&version).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, eggInstallErrorChatResultInvalid, "安装对应的 egg 版本已不存在", nil
		}
		return nil, "", "", err
	}
	if !supportsEggInstallClient(version, agent.AgentClientType) {
		return nil, eggInstallErrorTargetMismatch, "安装完成后的目标 agent 类型不匹配", nil
	}

	resolved := agent.ID
	return &resolved, "", "", nil
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case float64:
		if math.Trunc(typed) == typed {
			return fmt.Sprintf("%.0f", typed)
		}
		return fmt.Sprintf("%v", typed)
	case float32:
		if math.Trunc(float64(typed)) == float64(typed) {
			return fmt.Sprintf("%.0f", typed)
		}
		return fmt.Sprintf("%v", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case int32:
		return fmt.Sprintf("%d", typed)
	case uint64:
		return fmt.Sprintf("%d", typed)
	case uint32:
		return fmt.Sprintf("%d", typed)
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func optionalInt64Value(value any) (*int64, bool) {
	normalized := strings.TrimSpace(stringValue(value))
	if normalized == "" || normalized == "<nil>" {
		return nil, false
	}

	parsed, err := strconv.ParseInt(normalized, 10, 64)
	if err != nil {
		return nil, false
	}
	if parsed <= 0 {
		return nil, false
	}
	return &parsed, true
}
