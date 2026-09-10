package agentslashcmd

import "strings"

// descriptionI18n 是斜杠命令说明文案的翻译表，key 为 zh 参考文案（各 client_type
// 文件里 Register 时写的 Description 原文），value 按语言码存翻译，覆盖除 zh 外
// 的 10 种 app 语言（en ja ko de fr es pt ru ar hi）。解析顺序 lang → en → zh，
// 与安装指南 11 语（api/service/agent_api_install_guide_i18n.go 的
// localizedGuideText/pickGuideText）口径一致。
//
// 用一张按文案去重的表而不是把翻译内嵌进每条 SlashCommand，是因为同一句中文
// 说明在多个 client_type 之间大量原样重复（如"压缩当前会话上下文"），内嵌会把
// 同一份译文复制 N 份，改一处要同步 N 处；集中存放只需改一处，也方便写"全量
// 遍历 registry × 11 语言"的完整性测试。
//
// 分散在 i18n_data_*.go 几个文件里（每个 init 往同一张表追加一段），纯粹是为了
// 单文件行数不至于失控，没有语义上的分组。
var descriptionI18n = map[string]map[string]string{}

// registerDescriptionI18n 供 i18n_data_*.go 的 init() 调用，追加一批翻译条目。
// 重复的 zh key 会 panic：同一条命令说明只应该在一处维护译文。
func registerDescriptionI18n(entries map[string]map[string]string) {
	for zh, translations := range entries {
		if _, exists := descriptionI18n[zh]; exists {
			panic("agentslashcmd: duplicate description translation for " + zh)
		}
		descriptionI18n[zh] = translations
	}
}

// DescriptionFor 按 lang 解析斜杠命令说明的翻译。lang 传入 app 完整语言码
// （zh/en/ja/ko/de/fr/es/pt/ru/ar/hi，即 userpref.Language 的返回值，未收窄到
// 工具栏历史上只支持的 zh/en 二选一）。zh 或空值原样返回 zhDescription；翻译表
// 未命中该 zh 文案（如主人自定义斜杠命令的说明）时也原样返回 zhDescription，
// 与"查不到就不翻译"的既有行为一致。
func DescriptionFor(zhDescription, lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" || lang == "zh" {
		return zhDescription
	}
	translations, ok := descriptionI18n[zhDescription]
	if !ok {
		return zhDescription
	}
	if v := strings.TrimSpace(translations[lang]); v != "" {
		return v
	}
	if v := strings.TrimSpace(translations["en"]); v != "" {
		return v
	}
	return zhDescription
}
