package chatmarkdown

import (
	"regexp"
	"strings"

	extast "github.com/yuin/goldmark/extension/ast"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	gmtext "github.com/yuin/goldmark/text"
)

var orderedListMissingSpacePattern = regexp.MustCompile(
	`^([ \t]{0,3}\d{1,9}[.)])([^\s\d].*)$`,
)

type FinalRepairResult struct {
	Input                 string
	Output                string
	Changed               bool
	ParsedWithGoldmark    bool
	HasStructuredMarkdown bool
}

func RepairFinal(input string) FinalRepairResult {
	normalized := normalizeIngress(input)
	repaired := normalizeOrderedListSpacing(normalized)
	repaired = closeUnclosedFences(repaired)
	repaired = strings.TrimRight(repaired, "\n\r")

	summary := summarizeMarkdown(repaired)

	return FinalRepairResult{
		Input:                 input,
		Output:                repaired,
		Changed:               repaired != input,
		ParsedWithGoldmark:    summary.parsed,
		HasStructuredMarkdown: summary.hasStructuredMarkdown,
	}
}

func normalizeIngress(input string) string {
	normalized := strings.TrimLeft(input, " \t\n\r")
	if strings.HasPrefix(normalized, "\uFEFF") {
		normalized = strings.TrimPrefix(normalized, "\uFEFF")
	}
	return strings.ReplaceAll(
		strings.ReplaceAll(normalized, "\r\n", "\n"),
		"\r",
		"\n",
	)
}

func normalizeOrderedListSpacing(input string) string {
	if input == "" {
		return input
	}

	lines := strings.Split(input, "\n")
	activeFenceChar := byte(0)
	activeFenceLength := 0

	for i, line := range lines {
		fence, ok := parseFenceLine(line)
		if ok {
			if activeFenceChar == 0 {
				activeFenceChar = fence.markerChar
				activeFenceLength = fence.markerLength
			} else if fence.markerChar == activeFenceChar &&
				fence.markerLength >= activeFenceLength &&
				strings.TrimSpace(fence.tail) == "" {
				activeFenceChar = 0
				activeFenceLength = 0
			}
			continue
		}
		if activeFenceChar != 0 {
			continue
		}
		lines[i] = orderedListMissingSpacePattern.ReplaceAllString(line, "$1 $2")
	}

	return strings.Join(lines, "\n")
}

func closeUnclosedFences(input string) string {
	if input == "" {
		return input
	}

	lines := strings.Split(input, "\n")
	activeFenceChar := byte(0)
	activeFenceLength := 0

	for _, line := range lines {
		fence, ok := parseFenceLine(line)
		if !ok {
			continue
		}
		if activeFenceChar == 0 {
			activeFenceChar = fence.markerChar
			activeFenceLength = fence.markerLength
			continue
		}
		if fence.markerChar != activeFenceChar ||
			fence.markerLength < activeFenceLength ||
			strings.TrimSpace(fence.tail) != "" {
			continue
		}
		activeFenceChar = 0
		activeFenceLength = 0
	}

	if activeFenceChar == 0 {
		return input
	}

	closingFence := strings.Repeat(string(rune(activeFenceChar)), activeFenceLength)
	if strings.HasSuffix(input, "\n") {
		return input + closingFence
	}
	return input + "\n" + closingFence
}

type fenceLine struct {
	markerChar   byte
	markerLength int
	tail         string
}

func parseFenceLine(line string) (fenceLine, bool) {
	trimmedLeft := strings.TrimLeft(line, " \t")
	indentWidth := len(line) - len(trimmedLeft)
	if indentWidth > 3 || trimmedLeft == "" {
		return fenceLine{}, false
	}

	markerChar := trimmedLeft[0]
	if markerChar != '`' && markerChar != '~' {
		return fenceLine{}, false
	}

	markerLength := 0
	for markerLength < len(trimmedLeft) && trimmedLeft[markerLength] == markerChar {
		markerLength += 1
	}
	if markerLength < 3 {
		return fenceLine{}, false
	}

	tail := trimmedLeft[markerLength:]
	// Ignore malformed single-line fences so we don't accidentally append a
	// second closing fence at the end of the message.
	if strings.Contains(tail, strings.Repeat(string(rune(markerChar)), 3)) {
		return fenceLine{}, false
	}

	return fenceLine{
		markerChar:   markerChar,
		markerLength: markerLength,
		tail:         tail,
	}, true
}

type parseSummary struct {
	parsed                bool
	hasStructuredMarkdown bool
}

func summarizeMarkdown(input string) (summary parseSummary) {
	summary.parsed = true
	defer func() {
		if recover() != nil {
			summary = parseSummary{}
		}
	}()

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
	)
	doc := md.Parser().Parse(gmtext.NewReader([]byte(input)))
	if doc == nil {
		return parseSummary{}
	}

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if isStructuredMarkdownNode(node) {
			summary.hasStructuredMarkdown = true
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})

	return summary
}

func isStructuredMarkdownNode(node ast.Node) bool {
	switch node.(type) {
	case *ast.Heading,
		*ast.Blockquote,
		*ast.List,
		*ast.ListItem,
		*ast.CodeSpan,
		*ast.FencedCodeBlock,
		*ast.CodeBlock,
		*ast.Link,
		*ast.AutoLink,
		*ast.Image,
		*ast.Emphasis,
		*ast.ThematicBreak,
		*ast.RawHTML,
		*ast.HTMLBlock,
		*extast.Table,
		*extast.TableHeader,
		*extast.TableRow,
		*extast.TableCell,
		*extast.Strikethrough,
		*extast.TaskCheckBox:
		return true
	default:
		return false
	}
}
