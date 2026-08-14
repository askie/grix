package model

import "strings"

const (
	EggStatusDraft     = "draft"
	EggStatusPublished = "published"
	EggStatusBanned    = "banned"
)

const (
	EggCategoryStatusActive   = "active"
	EggCategoryStatusDisabled = "disabled"
)

const (
	EggPackageTypePersonaZip = "persona_zip"
	EggPackageTypeSkillZip   = "skill_zip"
)

// Legacy: kept for migration compatibility; new code should use HasPersonaZip / HasSkillZip.
const (
	EggTargetClientTypeOpenClaw = "openclaw"
	EggTargetClientTypeClaude   = "claude"
)

const (
	EggInstallStatusPending = "pending"
	EggInstallStatusRunning = "running"
	EggInstallStatusSuccess = "success"
	EggInstallStatusFailed  = "failed"
)

func NormalizeEggStatus(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func IsValidEggStatus(value string) bool {
	switch NormalizeEggStatus(value) {
	case EggStatusDraft, EggStatusPublished, EggStatusBanned:
		return true
	default:
		return false
	}
}

func NormalizeEggCategoryStatus(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func IsValidEggCategoryStatus(value string) bool {
	switch NormalizeEggCategoryStatus(value) {
	case EggCategoryStatusActive, EggCategoryStatusDisabled:
		return true
	default:
		return false
	}
}

func NormalizeEggPackageType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func IsValidEggPackageType(value string) bool {
	switch NormalizeEggPackageType(value) {
	case EggPackageTypePersonaZip, EggPackageTypeSkillZip:
		return true
	default:
		return false
	}
}

func NormalizeEggTargetClientType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func IsValidEggTargetClientType(value string) bool {
	switch NormalizeEggTargetClientType(value) {
	case EggTargetClientTypeOpenClaw, EggTargetClientTypeClaude:
		return true
	default:
		return false
	}
}

// IsValidSkillTargetType validates the target for skill_zip.
func IsValidSkillTargetType(value string) bool {
	switch NormalizeEggTargetClientType(value) {
	case EggTargetClientTypeOpenClaw, EggTargetClientTypeClaude:
		return true
	default:
		return false
	}
}
