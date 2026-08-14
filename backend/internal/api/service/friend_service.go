package service

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

const (
	friendIDSeqMask      = 0xFFF
	friendRemarkMaxRunes = 50

	friendRequestStatusPending  int8 = 0
	friendRequestStatusAccepted int8 = 1
	friendRequestStatusRejected int8 = 2
)

var friendIDSeq uint32

var hiddenFriendSearchUsernamePrefixes = []string{
	"delegate_owner_",
	"delegate_sender_",
}

func normalizeFriendRemarkName(raw string) (string, error) {
	remarkName := strings.TrimSpace(raw)
	if utf8.RuneCountInString(remarkName) > friendRemarkMaxRunes {
		return "", fmt.Errorf("remark_name too long: max %d characters", friendRemarkMaxRunes)
	}
	return remarkName, nil
}

func resolveFriendDisplayNickname(remarkName, nickname, username string) string {
	if name := strings.TrimSpace(remarkName); name != "" {
		return name
	}
	if name := strings.TrimSpace(nickname); name != "" {
		return name
	}
	return strings.TrimSpace(username)
}

func nextFriendID() int64 {
	seq := atomic.AddUint32(&friendIDSeq, 1) & friendIDSeqMask
	return (time.Now().UnixMilli() << 12) | int64(seq)
}

func applyFriendSearchVisibilityFilter(query *gorm.DB) *gorm.DB {
	for _, prefix := range hiddenFriendSearchUsernamePrefixes {
		query = query.Where("LOWER(username) NOT LIKE ?", prefix+"%")
	}
	return query
}

func isHiddenFriendSearchUsername(username string) bool {
	name := strings.ToLower(strings.TrimSpace(username))
	if name == "" {
		return false
	}
	for _, prefix := range hiddenFriendSearchUsernamePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
