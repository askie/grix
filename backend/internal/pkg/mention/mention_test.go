package mention

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseUserIDsStructuredPlusTextFallback(t *testing.T) {
	extra := json.RawMessage(`{"mention_user_ids":[1001,"1002",1001,-1,0,"bad"]}`)
	content := "hi @1003 and @1002 and @-1"

	got := ParseUserIDs(extra, content)
	want := []int64{1001, 1002, 1003}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseUserIDs() = %v, want %v", got, want)
	}
}

func TestParseUserIDsTextOnly(t *testing.T) {
	got := ParseUserIDs(nil, "hello @42 @42 world @100")
	want := []int64{42, 100}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseUserIDs() = %v, want %v", got, want)
	}
}

func TestNormalizeExtraInjectsCanonicalMentionIDs(t *testing.T) {
	raw := json.RawMessage(`{"reply_mode":"mention","mention_user_ids":["2001","2001"]}`)
	content := "@2002 hello @2001"
	normalized := NormalizeExtra(raw, content)

	var parsed map[string]any
	if err := json.Unmarshal(normalized, &parsed); err != nil {
		t.Fatalf("unmarshal normalized extra error: %v", err)
	}

	if parsed["reply_mode"] != "mention" {
		t.Fatalf("reply_mode lost after normalize, got=%v", parsed["reply_mode"])
	}

	rawMentions, ok := parsed["mention_user_ids"].([]any)
	if !ok {
		t.Fatalf("mention_user_ids should be array, got=%T", parsed["mention_user_ids"])
	}
	got := make([]int64, 0, len(rawMentions))
	for _, item := range rawMentions {
		id, ok := parseInt64FromAny(item)
		if !ok {
			t.Fatalf("mention_user_ids contains non-int value: %#v", item)
		}
		got = append(got, id)
	}

	want := []int64{2001, 2002}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized mention_user_ids = %v, want %v", got, want)
	}
}

func TestNormalizeExtraNoMentionKeepsOriginal(t *testing.T) {
	raw := json.RawMessage(`{"reply_mode":"plain"}`)
	normalized := NormalizeExtra(raw, "hello world")
	if string(normalized) != string(raw) {
		t.Fatalf("NormalizeExtra should keep original when no mentions, got=%s want=%s", normalized, raw)
	}
}

func TestParseUserIDsWithCandidatesResolveUsernameNickname(t *testing.T) {
	candidates := []Candidate{
		{UserID: 3001, Aliases: []string{"alice", "AliceCN"}},
		{UserID: 3002, Aliases: []string{"bob", "小明"}},
	}

	got := ParseUserIDsWithCandidates(nil, "hi @alice and @小明 and @3001", candidates)
	want := []int64{3001, 3002}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseUserIDsWithCandidates() = %v, want %v", got, want)
	}
}

func TestParseUserIDsWithCandidatesResolveDottedAndPlusUsername(t *testing.T) {
	candidates := []Candidate{
		{UserID: 3101, Aliases: []string{"john.doe"}},
		{UserID: 3102, Aliases: []string{"alice+work"}},
	}

	got := ParseUserIDsWithCandidates(nil, "hi @john.doe and @alice+work", candidates)
	want := []int64{3101, 3102}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseUserIDsWithCandidates() = %v, want %v", got, want)
	}
}

func TestParseUserIDsWithCandidatesAmbiguousAliasIgnored(t *testing.T) {
	candidates := []Candidate{
		{UserID: 4001, Aliases: []string{"dev"}},
		{UserID: 4002, Aliases: []string{"dev"}},
	}

	got := ParseUserIDsWithCandidates(nil, "ping @dev", candidates)
	if len(got) != 0 {
		t.Fatalf("ambiguous alias should be ignored, got=%v", got)
	}
}

func TestParseUserIDsWithCandidatesSkipsEmailLikeAt(t *testing.T) {
	candidates := []Candidate{
		{UserID: 5001, Aliases: []string{"example"}},
	}

	got := ParseUserIDsWithCandidates(nil, "mail me@example.com then @example", candidates)
	want := []int64{5001}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("email-like @ should be ignored, got=%v want=%v", got, want)
	}
}

func TestContainsMentionToken(t *testing.T) {
	testCases := []struct {
		name    string
		content string
		token   string
		want    bool
	}{
		{
			name:    "plain chinese mention",
			content: "@所有人 都在吗",
			token:   "所有人",
			want:    true,
		},
		{
			name:    "punctuation after token",
			content: "先叫一下 @所有人。",
			token:   "所有人",
			want:    true,
		},
		{
			name:    "email like at should not match",
			content: "mail a@所有人.com",
			token:   "所有人",
			want:    false,
		},
		{
			name:    "different token",
			content: "@所有人 都在吗",
			token:   "全员",
			want:    false,
		},
	}

	for _, tc := range testCases {
		if got := ContainsMentionToken(tc.content, tc.token); got != tc.want {
			t.Fatalf("%s: ContainsMentionToken() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNormalizeExtraWithCandidates(t *testing.T) {
	raw := json.RawMessage(`{"reply_mode":"mention"}`)
	candidates := []Candidate{
		{UserID: 6001, Aliases: []string{"alice"}},
	}
	normalized := NormalizeExtraWithCandidates(raw, "hello @alice", candidates)

	var parsed map[string]any
	if err := json.Unmarshal(normalized, &parsed); err != nil {
		t.Fatalf("unmarshal normalized extra error: %v", err)
	}
	rawMentions, ok := parsed["mention_user_ids"].([]any)
	if !ok || len(rawMentions) != 1 {
		t.Fatalf("mention_user_ids invalid: %#v", parsed["mention_user_ids"])
	}
	if id, _ := parseInt64FromAny(rawMentions[0]); id != 6001 {
		t.Fatalf("expected mention_user_ids=[6001], got=%#v", rawMentions)
	}
}

func TestParseUserIDsWithCandidatesSuppressesImplicitMentionWhenExplicitMentionExists(t *testing.T) {
	candidates := []Candidate{
		{UserID: 6051, Aliases: []string{"alice"}},
	}

	got := ParseUserIDsWithCandidates(nil, "hello @alice", candidates, 6052)
	want := []int64{6051}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseUserIDsWithCandidates() = %v, want %v", got, want)
	}
}

func TestNormalizeExtraWithCandidatesSkipsImplicitMentionWhenExplicitMentionExists(t *testing.T) {
	raw := json.RawMessage(`{"reply_mode":"mention","mention_user_ids":["6061"]}`)
	normalized := NormalizeExtraWithCandidates(raw, "hello world", nil, 6062)

	var parsed map[string]any
	if err := json.Unmarshal(normalized, &parsed); err != nil {
		t.Fatalf("unmarshal normalized extra error: %v", err)
	}
	rawMentions, ok := parsed["mention_user_ids"].([]any)
	if !ok || len(rawMentions) != 1 {
		t.Fatalf("mention_user_ids invalid: %#v", parsed["mention_user_ids"])
	}
	if id, _ := parseInt64FromAny(rawMentions[0]); id != 6061 {
		t.Fatalf("expected mention_user_ids=[6061], got=%#v", rawMentions)
	}
}

func TestNormalizeExtraWithCandidatesIncludesImplicitMention(t *testing.T) {
	raw := json.RawMessage(`{"reply_mode":"mention"}`)
	normalized := NormalizeExtraWithCandidates(raw, "hello world", nil, 6101)

	var parsed map[string]any
	if err := json.Unmarshal(normalized, &parsed); err != nil {
		t.Fatalf("unmarshal normalized extra error: %v", err)
	}
	rawMentions, ok := parsed["mention_user_ids"].([]any)
	if !ok || len(rawMentions) != 1 {
		t.Fatalf("mention_user_ids invalid: %#v", parsed["mention_user_ids"])
	}
	if id, _ := parseInt64FromAny(rawMentions[0]); id != 6101 {
		t.Fatalf("expected mention_user_ids=[6101], got=%#v", rawMentions)
	}
}

func TestShouldNormalize(t *testing.T) {
	if !ShouldNormalize(nil, "hello @1") {
		t.Fatalf("content with @ should require normalize")
	}
	if !ShouldNormalize(json.RawMessage(`{"mention_user_ids":[1]}`), "hello") {
		t.Fatalf("extra with mention_user_ids should require normalize")
	}
	if !ShouldNormalize(nil, "hello world", 6201) {
		t.Fatalf("implicit mention should require normalize")
	}
	if ShouldNormalize(nil, "hello world") {
		t.Fatalf("content without @ and extra mention should not require normalize")
	}
}

func TestRemoveMentionUserIDs(t *testing.T) {
	raw := json.RawMessage(`{"reply_mode":"plain","mention_user_ids":["2001","2002"],"foo":"bar"}`)
	cleaned := RemoveMentionUserIDs(raw)

	var parsed map[string]any
	if err := json.Unmarshal(cleaned, &parsed); err != nil {
		t.Fatalf("unmarshal cleaned extra error: %v", err)
	}
	if _, ok := parsed["mention_user_ids"]; ok {
		t.Fatalf("mention_user_ids should be removed, got=%#v", parsed["mention_user_ids"])
	}
	if parsed["reply_mode"] != "plain" || parsed["foo"] != "bar" {
		t.Fatalf("other fields should stay intact, got=%#v", parsed)
	}
}

func TestRemoveMentionUserIDsReturnsNilWhenOnlyMentionFieldExists(t *testing.T) {
	raw := json.RawMessage(`{"mention_user_ids":["2001"]}`)
	cleaned := RemoveMentionUserIDs(raw)
	if cleaned != nil {
		t.Fatalf("expected nil cleaned extra, got=%s", string(cleaned))
	}
}

func TestParseMentionIDsFromContentBoundary(t *testing.T) {
	testCases := []struct {
		name    string
		content string
		want    []int64
	}{
		{
			name:    "database connection string is not a mention",
			content: "postgres://eshop:eshop@100.64.0.7:15432/eshop?sslmode=disable",
			want:    nil,
		},
		{
			name:    "numeric id glued to a word is not a mention",
			content: "user@123",
			want:    nil,
		},
		{
			name:    "dotted numeric literal is not a mention",
			content: "a@1.2.3.4",
			want:    nil,
		},
		{
			name:    "leading ip literal is not a mention",
			content: "@1.2.3.4 down",
			want:    nil,
		},
		{
			name:    "mention followed by space",
			content: "@123 hi",
			want:    []int64{123},
		},
		{
			name:    "mention after chinese text",
			content: "你好@123",
			want:    []int64{123},
		},
		{
			name:    "mention at line start",
			content: "hello\n@123",
			want:    []int64{123},
		},
		{
			name:    "mention followed by fullwidth comma",
			content: "@123，在吗",
			want:    []int64{123},
		},
		{
			name:    "mention followed by ascii period",
			content: "ping @123.",
			want:    []int64{123},
		},
	}

	for _, tc := range testCases {
		got := parseMentionIDsFromContent(tc.content)
		if len(got) == 0 && len(tc.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: parseMentionIDsFromContent(%q) = %v, want %v", tc.name, tc.content, got, tc.want)
		}
	}
}

func TestParseUserIDsKeepsImplicitMentionWhenContentHasConnectionString(t *testing.T) {
	const quotedOwnerID = int64(2053600175380762624)
	content := "[dispatch-result] done\npostgres://eshop:eshop@100.64.0.7:15432/eshop?sslmode=disable"

	got := ParseUserIDs(nil, content, quotedOwnerID)
	want := []int64{quotedOwnerID}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseUserIDs() = %v, want %v", got, want)
	}
}
