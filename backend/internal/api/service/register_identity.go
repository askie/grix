package service

import (
	crand "crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

const (
	registerUsernameMaxLen  = 100
	registerNicknameMaxLen  = 50
	registerMaxSuffixDigits = 6
)

func buildRegisterIdentity(email string) (string, string, error) {
	base, err := extractEmailLocalPart(email)
	if err != nil {
		return "", "", err
	}

	username, err := findAvailableUserFieldValue("username", base, registerUsernameMaxLen)
	if err != nil {
		return "", "", err
	}
	nickname, err := findAvailableUserFieldValue("nickname", base, registerNicknameMaxLen)
	if err != nil {
		return "", "", err
	}
	return username, nickname, nil
}

func extractEmailLocalPart(email string) (string, error) {
	addr := strings.TrimSpace(email)
	parts := strings.SplitN(addr, "@", 2)
	if len(parts) != 2 {
		return "", errors.New("邮箱格式不合法")
	}
	local := strings.TrimSpace(parts[0])
	if local == "" {
		return "", errors.New("邮箱格式不合法")
	}
	return local, nil
}

func findAvailableUserFieldValue(field, base string, maxLen int) (string, error) {
	normalized := strings.TrimSpace(base)
	if normalized == "" {
		return "", errors.New("邮箱格式不合法")
	}
	if maxLen <= 1 {
		return "", errors.New("注册失败: 账号字段长度配置错误")
	}

	candidate := trimBaseForSuffix(normalized, maxLen, 0)
	exists, err := userFieldValueExists(field, candidate)
	if err != nil {
		return "", err
	}
	if !exists {
		return candidate, nil
	}

	for digits := 1; digits <= registerMaxSuffixDigits; digits++ {
		prefix := trimBaseForSuffix(normalized, maxLen, digits)
		if prefix == "" {
			continue
		}

		total := pow10(digits)
		start, err := secureRandInt(total)
		if err != nil {
			return "", errors.New("注册失败: 随机后缀生成失败")
		}

		for i := 0; i < total; i++ {
			suffixNum := (start + i) % total
			next := fmt.Sprintf("%s%0*d", prefix, digits, suffixNum)
			used, checkErr := userFieldValueExists(field, next)
			if checkErr != nil {
				return "", checkErr
			}
			if !used {
				return next, nil
			}
		}
	}

	return "", fmt.Errorf("注册失败: %s 可用名已耗尽", field)
}

func trimBaseForSuffix(base string, maxLen, suffixDigits int) string {
	available := maxLen - suffixDigits
	if available <= 0 {
		return ""
	}
	if len(base) <= available {
		return base
	}
	return base[:available]
}

func userFieldValueExists(field, value string) (bool, error) {
	switch field {
	case "username", "nickname":
	default:
		return false, errors.New("注册失败: 非法字段查询")
	}

	var count int64
	if err := store.DB.Model(&model.User{}).Where(field+" = ?", value).Count(&count).Error; err != nil {
		return false, errors.New("注册失败: 检查重名失败")
	}
	return count > 0, nil
}

func pow10(n int) int {
	v := 1
	for i := 0; i < n; i++ {
		v *= 10
	}
	return v
}

func secureRandInt(max int) (int, error) {
	if max <= 0 {
		return 0, errors.New("invalid max")
	}
	n, err := crand.Int(crand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}

func isDuplicatedEmailErr(err error) bool {
	return isDuplicatedConstraintErr(err, "uni_users_email", "users.email")
}

func isDuplicatedUsernameErr(err error) bool {
	return isDuplicatedConstraintErr(err, "uni_users_username", "users.username")
}

func isDuplicatedConstraintErr(err error, keys ...string) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "duplicate") && !strings.Contains(msg, "unique") {
		return false
	}
	for _, key := range keys {
		if strings.Contains(msg, strings.ToLower(key)) {
			return true
		}
	}
	return false
}
