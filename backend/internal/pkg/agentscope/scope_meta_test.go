package agentscope

import "testing"

func TestAllowedScopeItemsCoverAllowedScopes(t *testing.T) {
	scopes := AllowedScopes()
	items := AllowedScopeItems("zh")
	if len(items) != len(scopes) {
		t.Fatalf("AllowedScopeItems len=%d want=%d", len(items), len(scopes))
	}
	for i, scope := range scopes {
		item := items[i]
		if item.Scope != scope {
			t.Fatalf("item[%d].Scope=%q want=%q", i, item.Scope, scope)
		}
		if item.Label == "" || item.Description == "" {
			t.Fatalf("item[%d] has empty text: %#v", i, item)
		}
		if item.Label == scope || item.Description == scope {
			t.Fatalf("item[%d] falls back to raw scope key: %#v", i, item)
		}
	}
}

func TestAllowedScopeItemsLocalizesZhAndEnglish(t *testing.T) {
	zhItems := AllowedScopeItems("zh")
	zhCNItems := AllowedScopeItems("zh-CN")
	enItems := AllowedScopeItems("en")
	if len(zhItems) == 0 || len(enItems) == 0 {
		t.Fatal("expected non-empty scope items")
	}
	if zhItems[0].Label != "创建 API Agent" {
		t.Fatalf("zh label=%q want 创建 API Agent", zhItems[0].Label)
	}
	if zhCNItems[0].Label != "创建 API Agent" {
		t.Fatalf("zh-CN label=%q want 创建 API Agent", zhCNItems[0].Label)
	}
	if enItems[0].Label != "Create API Agent" {
		t.Fatalf("en label=%q want Create API Agent", enItems[0].Label)
	}
}
