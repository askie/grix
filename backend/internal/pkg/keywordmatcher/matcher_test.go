package keywordmatcher

import (
	"reflect"
	"testing"
)

func TestMatcherMatchReturnsUniqueSortedKeywords(t *testing.T) {
	matcher := Compile([]string{" Forbidden ", "敏感词", "forbidden", "危词"})

	got := matcher.Match("这条消息包含敏感词，也包含FORBIDDEN，还重复 forbidden。")
	want := []string{"forbidden", "敏感词"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Match() = %#v, want %#v", got, want)
	}
}

func TestMatcherMatchReturnsNilWhenNoKeywordMatched(t *testing.T) {
	matcher := Compile([]string{"敏感词", "forbidden"})

	got := matcher.Match("ordinary message")
	if got != nil {
		t.Fatalf("Match() = %#v, want nil", got)
	}
}
