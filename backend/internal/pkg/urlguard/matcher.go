package urlguard

import (
	"regexp"
	"strings"
)

// RuleKind 规则类型。
type RuleKind string

const (
	RuleExactDomain RuleKind = "domain"   // 精确域：evil.com（含子域、子路径自动通过 host 后缀匹配）
	RuleWildcard    RuleKind = "wildcard" // 父域通配：*.evil.com（语义等价于精确域 evil.com）
	RuleRegex       RuleKind = "regex"    // URL 整体正则
	RuleKeyword     RuleKind = "keyword"  // URL 中包含的关键词
)

// Severity 风险等级。
type Severity string

const (
	SeverityMalicious  Severity = "malicious"
	SeveritySuspicious Severity = "suspicious"
)

// Rule 单条黑名单规则。
type Rule struct {
	ID       int64
	Kind     RuleKind
	Value    string
	Severity Severity
	Source   string
}

// Verdict 匹配判定结果。
type Verdict struct {
	Hit           bool
	URL           string
	CanonicalHost string
	Rule          Rule
	Severity      Severity
}

// Matcher 编译后的黑名单索引。按 kind 分桶。
type Matcher struct {
	exact    map[string]Rule // host 后缀精确命中
	regexes  []compiledRegex
	keywords []Rule
}

type compiledRegex struct {
	re   *regexp.Regexp
	rule Rule
}

// Compile 把规则集编译为匹配器。无效规则（如非法正则）会被跳过。
func Compile(rules []Rule) *Matcher {
	m := &Matcher{exact: make(map[string]Rule)}
	for _, r := range rules {
		switch r.Kind {
		case RuleExactDomain:
			k := normalizeDomainKey(r.Value)
			if k != "" {
				m.exact[k] = r
			}
		case RuleWildcard:
			k := normalizeDomainKey(strings.TrimPrefix(r.Value, "*."))
			if k != "" {
				m.exact[k] = r
			}
		case RuleRegex:
			if re, err := regexp.Compile(r.Value); err == nil {
				m.regexes = append(m.regexes, compiledRegex{re: re, rule: r})
			}
		case RuleKeyword:
			v := strings.ToLower(strings.TrimSpace(r.Value))
			if v != "" {
				rr := r
				rr.Value = v
				m.keywords = append(m.keywords, rr)
			}
		}
	}
	return m
}

// Match 对单条原始 URL 判定，返回命中结果（Hit=false 表示干净）。
// 命中优先级：malicious > suspicious；同级别取首个命中规则。
func (m *Matcher) Match(rawURL string) Verdict {
	if m == nil {
		return Verdict{}
	}
	canon, err := Canonicalize(rawURL)
	if err != nil {
		return Verdict{}
	}

	// 1) host 后缀 / 通配精确命中
	for _, h := range canon.HostSuffixes() {
		if r, ok := m.exact[h]; ok {
			return Verdict{Hit: true, URL: rawURL, CanonicalHost: canon.Host, Rule: r, Severity: r.Severity}
		}
	}

	// 拼一个用于正则 / 关键词匹配的全 URL（不带 scheme，跨规则更稳定）
	full := canon.Host + canon.Path
	if canon.Query != "" {
		full += "?" + canon.Query
	}

	// 2) URL 正则
	for _, cr := range m.regexes {
		if cr.re.MatchString(full) {
			return Verdict{Hit: true, URL: rawURL, CanonicalHost: canon.Host, Rule: cr.rule, Severity: cr.rule.Severity}
		}
	}

	// 3) 关键词
	lowerFull := strings.ToLower(full)
	for _, kw := range m.keywords {
		if strings.Contains(lowerFull, kw.Value) {
			return Verdict{Hit: true, URL: rawURL, CanonicalHost: canon.Host, Rule: kw, Severity: kw.Severity}
		}
	}

	return Verdict{}
}

// MatchText 从一段文本中提取所有 URL 并取风险最高的命中。
// 若有 malicious 命中即立即返回（短路）；否则保留首个 suspicious。
func (m *Matcher) MatchText(text string) Verdict {
	var worst Verdict
	for _, raw := range Extract(text) {
		v := m.Match(raw)
		if !v.Hit {
			continue
		}
		if v.Severity == SeverityMalicious {
			return v
		}
		if !worst.Hit {
			worst = v
		}
	}
	return worst
}

// normalizeDomainKey 把规则值规范化成 exact 桶的查表 key。
// 与 Canonicalize 的 host 输出保持口径一致。
func normalizeDomainKey(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.TrimPrefix(v, "*.")
	v = strings.Trim(v, ".")
	v = collapseDots(v)
	return v
}
