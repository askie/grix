package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestUserMarshalJSON_MasksPhone 确认 User JSON 出站时 phone_e164 只输出末 4 位脱敏（****8000），
// 真实手机号、密文、盲索引都永不出现。空值通过 omitempty 省略字段。
func TestUserMarshalJSON_MasksPhone(t *testing.T) {
	cases := []struct {
		name         string
		user         User
		wantContains string
		wantMissing  []string
		wantHasField bool
	}{
		{
			name: "encrypted-user-shows-last4",
			user: User{
				ID: 1, Username: "alice",
				PhoneLast4:  "8000",
				PhoneCipher: "Y2lwaGVydGV4dC1ub3QtbGVha2Vk",
				PhoneBlind:  "deadbeefblindindexvalue",
			},
			wantContains: `"phone_e164":"****8000"`,
			// 真实号片段、密文、盲索引都不得出现
			wantMissing:  []string{"13800138000", "Y2lwaGVydGV4dC", "deadbeefblindindex", "phone_cipher", "phone_blind", "phone_last4"},
			wantHasField: true,
		},
		{
			name:         "legacy-plaintext-fallback-last4",
			user:         User{ID: 2, Username: "carol", PhoneE164: "+8613800138000"},
			wantContains: `"phone_e164":"****8000"`,
			wantMissing:  []string{"13800138000", "+8613800138000"},
			wantHasField: true,
		},
		{
			name:         "no-phone-omitted",
			user:         User{ID: 3, Username: "dave"},
			wantContains: "",
			wantMissing:  []string{`"phone_e164"`},
			wantHasField: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := json.Marshal(c.user)
			if err != nil {
				t.Fatalf("Marshal err: %v", err)
			}
			s := string(raw)
			if c.wantHasField && !strings.Contains(s, c.wantContains) {
				t.Fatalf("expected %q in %s", c.wantContains, s)
			}
			for _, leak := range c.wantMissing {
				if strings.Contains(s, leak) {
					t.Fatalf("leaked substring %q found in %s", leak, s)
				}
			}
		})
	}
}

// TestUserMarshalJSON_RoundTripIntoMap 通过 map 反解确认客户端拿到的就是末 4 位脱敏值。
func TestUserMarshalJSON_RoundTripIntoMap(t *testing.T) {
	u := User{ID: 42, Username: "bob", PhoneLast4: "8000", PhoneCipher: "secretcipher", PhoneBlind: "blindx"}
	raw, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got, _ := m["phone_e164"].(string); got != "****8000" {
		t.Fatalf("phone_e164 not masked to last4: %q", got)
	}
	if _, ok := m["phone_cipher"]; ok {
		t.Fatalf("phone_cipher must not be serialized")
	}
	if _, ok := m["phone_blind"]; ok {
		t.Fatalf("phone_blind must not be serialized")
	}
}

// TestUserMarshalJSON_PointerSliceAlsoMasked 嵌入 User 的切片 / 指针场景也应脱敏，
// 防止登录返回结构 LoginResp 内的 User 字段绕过 MarshalJSON。
func TestUserMarshalJSON_PointerSliceAlsoMasked(t *testing.T) {
	type wrap struct {
		User  User   `json:"user"`
		UserP *User  `json:"user_p"`
		Users []User `json:"users"`
	}
	u := User{ID: 7, Username: "carol", PhoneLast4: "8000", PhoneCipher: "cipherval", PhoneBlind: "blindval"}
	w := wrap{User: u, UserP: &u, Users: []User{u}}
	raw, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(raw)
	if strings.Contains(s, "cipherval") || strings.Contains(s, "blindval") {
		t.Fatalf("cipher/blind leaked in wrapper: %s", s)
	}
	if strings.Count(s, "****8000") != 3 {
		t.Fatalf("expected 3 masked occurrences in %s", s)
	}
}
