package handler

import "github.com/askie/grix/backend/internal/pkg/keywordmatcher"

var openGroupTurnMatcher = keywordmatcher.Compile([]string{
	"大家",
	"各位",
	"你们",
	"有人",
	"谁有",
	"谁能",
	"谁来",
	"everyone",
	"anyone",
	"anybody",
	"all of you",
	"folks",
	"team",
})

func shouldSuppressGroupContinuation(content string) bool {
	return len(openGroupTurnMatcher.Match(content)) > 0
}
