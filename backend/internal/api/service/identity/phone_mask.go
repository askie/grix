package identity

import "github.com/askie/grix/backend/internal/pkg/phonemask"

// PhoneMask 脱敏手机号用于日志/管理面板显示，复用 phonemask.Mask 单一真源。
//
//	+8613800138000 → +86138****8000（保留前 6 后 4）
//	+15551234567   → +15551****4567
//	+44712345678   → +44712****5678
//	+1234567       → ********（短号整段星号化，避免真实号泄露）
func PhoneMask(phone string) string {
	return phonemask.Mask(phone)
}
