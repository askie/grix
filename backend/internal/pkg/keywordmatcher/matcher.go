package keywordmatcher

import (
	"sort"
	"strings"
)

type node struct {
	next    map[rune]int
	fail    int
	outputs []string
}

type Matcher struct {
	nodes []node
}

func Compile(keywords []string) *Matcher {
	normalized := normalizeKeywords(keywords)
	matcher := &Matcher{
		nodes: []node{{next: make(map[rune]int)}},
	}
	if len(normalized) == 0 {
		return matcher
	}

	for _, keyword := range normalized {
		state := 0
		for _, r := range keyword {
			next, ok := matcher.nodes[state].next[r]
			if !ok {
				next = len(matcher.nodes)
				matcher.nodes[state].next[r] = next
				matcher.nodes = append(matcher.nodes, node{next: make(map[rune]int)})
			}
			state = next
		}
		matcher.nodes[state].outputs = appendUniqueStrings(matcher.nodes[state].outputs, keyword)
	}

	queue := make([]int, 0, len(matcher.nodes))
	for _, next := range matcher.nodes[0].next {
		queue = append(queue, next)
	}

	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]

		for r, next := range matcher.nodes[state].next {
			queue = append(queue, next)

			fail := matcher.nodes[state].fail
			for fail != 0 {
				if fallback, ok := matcher.nodes[fail].next[r]; ok {
					matcher.nodes[next].fail = fallback
					break
				}
				fail = matcher.nodes[fail].fail
			}
			if matcher.nodes[next].fail == 0 {
				if fallback, ok := matcher.nodes[0].next[r]; ok && fallback != next {
					matcher.nodes[next].fail = fallback
				}
			}
			matcher.nodes[next].outputs = appendUniqueStrings(
				matcher.nodes[next].outputs,
				matcher.nodes[matcher.nodes[next].fail].outputs...,
			)
		}
	}

	return matcher
}

func (m *Matcher) Match(text string) []string {
	if m == nil || len(m.nodes) == 0 {
		return nil
	}

	normalized := normalizeKeyword(text)
	if normalized == "" {
		return nil
	}

	state := 0
	matches := make([]string, 0, 4)
	seen := make(map[string]struct{})
	for _, r := range normalized {
		for state != 0 {
			if _, ok := m.nodes[state].next[r]; ok {
				break
			}
			state = m.nodes[state].fail
		}
		if next, ok := m.nodes[state].next[r]; ok {
			state = next
		}

		for _, output := range m.nodes[state].outputs {
			if _, ok := seen[output]; ok {
				continue
			}
			seen[output] = struct{}{}
			matches = append(matches, output)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.Strings(matches)
	return matches
}

func normalizeKeywords(keywords []string) []string {
	if len(keywords) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(keywords))
	normalized := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		value := normalizeKeyword(keyword)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	if len(normalized) == 0 {
		return nil
	}
	sort.Strings(normalized)
	return normalized
}

func normalizeKeyword(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func appendUniqueStrings(base []string, values ...string) []string {
	if len(values) == 0 {
		return base
	}

	seen := make(map[string]struct{}, len(base)+len(values))
	for _, item := range base {
		seen[item] = struct{}{}
	}
	for _, item := range values {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		base = append(base, item)
	}
	return base
}
