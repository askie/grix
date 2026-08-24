package service

import "testing"

func TestEggGetReturnsPackageURLs(t *testing.T) {
	testDB, userID := setupEggSearchTestDB(t)
	seedSearchCategory(t, testDB, "persona")
	seedSearchEgg(
		t,
		testDB,
		seedSearchEggParams{
			ID:          "lobster.persona_get",
			CategoryID:  "persona",
			Locale:      "zh",
			Name:        "测试虾蛋",
			Description: "用于验证详情返回下载地址。",
			SearchText:  "测试虾蛋",
		},
	)

	resp, ec := EggGet(userID, EggGetReq{ID: "lobster.persona_get", Locale: "zh"})
	if ec != nil {
		t.Fatalf("EggGet error: %#v", ec)
	}
	if resp.PersonaZipURL != "https://example.com/lobster.persona_get-v1.zip" {
		t.Fatalf("persona_zip_url=%q", resp.PersonaZipURL)
	}
	if resp.PersonaZipSHA256 != "abc123" {
		t.Fatalf("persona_zip_sha256=%q", resp.PersonaZipSHA256)
	}
	if resp.SkillZipURL != "" || resp.SkillZipSHA256 != "" {
		t.Fatalf("skill package should be empty, got url=%q sha=%q", resp.SkillZipURL, resp.SkillZipSHA256)
	}
	if !resp.CanCreateAgent {
		t.Fatalf("can_create_agent should be true when persona package exists")
	}
}
