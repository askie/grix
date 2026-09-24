package agentapi

import (
	"testing"
)

// custom_toolbar 必须在 nullable 名单里：agent 撤掉全部自定义项时上报空数组，
// 若按「有值才覆盖」处理，旧项会永远留在工具栏上撤不掉。
func TestMergeToolbarMeta_CustomToolbarEmptyArrayClearsItems(t *testing.T) {
	dst := map[string]any{
		"custom_toolbar": []any{map[string]any{"id": "env"}},
	}
	merged := mergeToolbarMeta(dst, map[string]any{"custom_toolbar": []any{}})

	value, ok := merged["custom_toolbar"]
	if !ok {
		t.Fatal("custom_toolbar key dropped instead of cleared")
	}
	list, ok := value.([]any)
	if !ok {
		t.Fatalf("custom_toolbar = %T, want []any", value)
	}
	if len(list) != 0 {
		t.Fatalf("custom_toolbar = %v, want empty", list)
	}
}

// 非空上报正常覆盖。
func TestMergeToolbarMeta_CustomToolbarReplacesItems(t *testing.T) {
	dst := map[string]any{
		"custom_toolbar": []any{map[string]any{"id": "old"}},
	}
	merged := mergeToolbarMeta(dst, map[string]any{
		"custom_toolbar": []any{map[string]any{"id": "new"}},
	})
	list, _ := merged["custom_toolbar"].([]any)
	if len(list) != 1 {
		t.Fatalf("custom_toolbar = %v, want a single item", list)
	}
	item, _ := list[0].(map[string]any)
	if item["id"] != "new" {
		t.Fatalf("custom_toolbar item = %v, want the newly reported one", item)
	}
}

// 没带 custom_toolbar 的上报不能把已有的自定义工具栏清掉：
// 其余上报路径（切模型、切模式、绑定变化）都不带这个键。
func TestMergeToolbarMeta_CustomToolbarSurvivesUnrelatedUpdates(t *testing.T) {
	dst := map[string]any{
		"custom_toolbar": []any{map[string]any{"id": "env"}},
	}
	merged := mergeToolbarMeta(dst, map[string]any{"model_id": "m-a"})
	list, _ := merged["custom_toolbar"].([]any)
	if len(list) != 1 {
		t.Fatalf("custom_toolbar = %v, want it preserved", list)
	}
}
