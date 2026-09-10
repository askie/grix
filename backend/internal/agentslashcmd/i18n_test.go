package agentslashcmd

import (
	"regexp"
	"strings"
	"testing"
)

// slashDescriptionTokenPattern 抓取命令说明里"翻译时必须原样保留"的 token：
// 斜杠命令别名（如 /bg、/new-chat）与 Sprintf 占位符（如 %s）。用于完整性测试
// 断言译文没有把这些字面量也顺手"翻译"掉。
var slashDescriptionTokenPattern = regexp.MustCompile(`/[A-Za-z][A-Za-z0-9_-]*|%[a-zA-Z]`)

// appLanguages 与 backend/internal/pkg/userpref/language.go 的 supportedLanguages
// 一致，是 BuildInput.LanguageFull（userpref.Language 的返回值）可能取到的全部值。
var appLanguages = []string{"zh", "en", "ja", "ko", "de", "fr", "es", "pt", "ru", "ar", "hi"}

// TestDescriptionFor_CoversAllRegisteredCommandsIn11Languages 遍历全部已注册
// client_type 的斜杠命令 × 11 种 app 语言，断言 DescriptionFor 的解析结果：
//  1. 非空；
//  2. 非 zh 语言时不等于中文原文——否则说明该命令的翻译在 descriptionI18n 里
//     缺失，只是安静地回退成了看起来正常的中文，测试要能抓住这种缺口；
//  3. 中文原文里的斜杠别名 / Sprintf 占位符在译文中原样保留。
func TestDescriptionFor_CoversAllRegisteredCommandsIn11Languages(t *testing.T) {
	for clientType, cmds := range registry {
		for _, cmd := range cmds {
			zh := cmd.Description
			if strings.TrimSpace(zh) == "" {
				continue
			}
			wantTokens := slashDescriptionTokenPattern.FindAllString(zh, -1)
			for _, lang := range appLanguages {
				got := DescriptionFor(zh, lang)
				if strings.TrimSpace(got) == "" {
					t.Errorf("client_type=%s command=%s lang=%s: DescriptionFor returned empty", clientType, cmd.Name, lang)
					continue
				}
				if lang != "zh" && got == zh {
					t.Errorf("client_type=%s command=%s lang=%s: description not translated, still zh original %q", clientType, cmd.Name, lang, zh)
				}
				for _, token := range wantTokens {
					if !strings.Contains(got, token) {
						t.Errorf("client_type=%s command=%s lang=%s: translation dropped placeholder %q, got %q", clientType, cmd.Name, lang, token, got)
					}
				}
			}
		}
	}
}

// TestDescriptionFor_UnknownTextFallsBackUnchanged 覆盖主人自定义斜杠命令这类
// 不在 descriptionI18n 里的说明文本：解析任何语言都应原样返回，不做翻译，也不
// panic。这与 core.localizeSnapshot 里"自定义命令在 i18n 之后合并、不参与翻译"
// 的既有行为一致。
func TestDescriptionFor_UnknownTextFallsBackUnchanged(t *testing.T) {
	custom := "老郭自己写的斜杠命令说明"
	for _, lang := range appLanguages {
		if got := DescriptionFor(custom, lang); got != custom {
			t.Errorf("lang=%s: got %q, want unchanged %q", lang, got, custom)
		}
	}
}

// TestDescriptionFor_EmptyOrZhLanguageShortCircuits 覆盖 lang 为空或 "zh" 时
// 直接原样返回，不查字典——这是最常见的默认路径。
func TestDescriptionFor_EmptyOrZhLanguageShortCircuits(t *testing.T) {
	zh := "查看当前 Pi 会话状态"
	for _, lang := range []string{"", "zh", " ZH "} {
		if got := DescriptionFor(zh, lang); got != zh {
			t.Errorf("lang=%q: got %q, want %q", lang, got, zh)
		}
	}
}

// TestDescriptionI18n_NoOrphanedEntries 反向校验：翻译表里的每一条 zh key 都必须
// 对应某个当前仍在注册表里的命令说明。key 会变成孤儿的典型场景是某个命令后来
// 被删除或改了文案（如 reasonix 的 /restart 曾经存在、后被下线），但翻译表没
// 跟着清理——孤儿条目本身不会造成运行期错误（DescriptionFor 只是多存了一条永
// 远查不到的 key），但会不知不觉地在表里越堆越多，也可能掩盖"文案改了但翻译
// 没同步改"的真实遗漏。
func TestDescriptionI18n_NoOrphanedEntries(t *testing.T) {
	live := map[string]bool{}
	for _, cmds := range registry {
		for _, cmd := range cmds {
			if zh := strings.TrimSpace(cmd.Description); zh != "" {
				live[zh] = true
			}
		}
	}
	for zh := range descriptionI18n {
		if !live[zh] {
			t.Errorf("descriptionI18n has an orphaned entry not backed by any registered command: %q", zh)
		}
	}
}
