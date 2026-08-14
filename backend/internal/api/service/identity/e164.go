package identity

import (
	"errors"
	"regexp"
	"strings"
)

// E.164 校验：以 + 开头 + 7~15 位数字。
var e164Re = regexp.MustCompile(`^\+\d{7,15}$`)

// 已知 country code prefix → ISO 区域映射。一期只区分 cn / global，
// 二期可以接入 libphonenumber 做更细的 country/region 拆分。
var knownCountryPrefixes = []string{
	"+86", // 中国大陆
	"+1",
	"+44",
	"+49",
	"+33",
	"+81",
	"+82",
	"+852",
	"+853",
	"+886",
	"+65",
	"+60",
	"+66",
	"+62",
	"+63",
	"+61",
	"+64",
	"+91",
	"+92",
	"+971",
	"+966",
	"+972",
	"+90",
	"+7",
	"+34",
	"+39",
	"+31",
	"+46",
	"+47",
	"+48",
	"+55",
	"+52",
	"+54",
	"+27",
	"+20",
	"+234",
}

// SanitizePhoneE164 把用户输入的手机号标准化为 E.164 格式：
//   - 去除所有空格、横线、括号、点号
//   - 去除 "(0)" 这类干扰段
//   - 必须以 + 开头
//   - 校验最终格式
func SanitizePhoneE164(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("手机号不能为空")
	}
	s := strings.TrimSpace(raw)
	// 替换全角加号
	s = strings.ReplaceAll(s, "＋", "+")
	// 去除 "(0)" 国际拨号本地段
	s = strings.ReplaceAll(s, "(0)", "")
	// 去除其它分隔符
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '+' && i == 0:
			b.WriteRune('+')
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
			// skip
		default:
			return "", errors.New("手机号格式不合法")
		}
	}
	out := b.String()
	if !e164Re.MatchString(out) {
		return "", errors.New("手机号格式不合法")
	}
	return out, nil
}

// ParseCountryCode 返回 E.164 号码的国家区号（含 +），如 "+86"。
// 解析规则：找匹配前缀的最长 known prefix；都没匹配返回 "" 与 error。
func ParseCountryCode(phoneE164 string) (string, error) {
	if !e164Re.MatchString(phoneE164) {
		return "", errors.New("手机号格式不合法")
	}
	best := ""
	for _, p := range knownCountryPrefixes {
		if strings.HasPrefix(phoneE164, p) && len(p) > len(best) {
			best = p
		}
	}
	if best == "" {
		return "", errors.New("不支持的国家区号")
	}
	return best, nil
}

// RegionForCountry 返回 cn / global，决定走哪条短信通道、归属哪个区。
func RegionForCountry(countryCode string) string {
	if countryCode == "+86" {
		return "cn"
	}
	return "global"
}
