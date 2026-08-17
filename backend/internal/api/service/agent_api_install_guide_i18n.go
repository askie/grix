package service

// Language matrix plumbing for the agent-api install-guide catalog.
//
// zh and en are the reference texts and live next to the catalog definition in
// agent_api_install_guide_service.go; the other nine app languages (ja ko de fr
// es pt ru ar hi — keep in sync with frontend/assets/i18n) live in the
// agent_api_install_guide_task_*_i18n.go files. pickGuideText resolves
// lang → en → zh, so a language missing from any map silently reads English.

// cliPhrase renders the human-readable CLI reference embedded in connector
// tasks, e.g. zh "Claude Code CLI（claude）" / en "the Claude Code CLI (claude)".
// Languages other than zh/en use the neutral article-free form so one phrase
// fits every sentence that embeds it.
func cliPhrase(lang, display, binZh, binEn, binNeutral string) string {
	switch lang {
	case "zh":
		return display + " CLI（" + binZh + "）"
	case "en":
		return "the " + display + " CLI (" + binEn + ")"
	default:
		return display + " CLI (" + binNeutral + ")"
	}
}

// connectorIntroPatterns: one-line intro shown in the type picker; %s is the
// product name (Claude Code, Codex, ...). Also used for Kimi.
var connectorIntroPatterns = map[string]string{
	"zh": "通过 grix-connector 接入 %s",
	"en": "Connect %s through grix-connector",
	"ja": "grix-connector 経由で %s を接続",
	"ko": "grix-connector를 통해 %s 연결",
	"de": "%s über grix-connector verbinden",
	"fr": "Connecter %s via grix-connector",
	"es": "Conectar %s mediante grix-connector",
	"pt": "Conectar %s via grix-connector",
	"ru": "Подключить %s через grix-connector",
	"ar": "ربط %s عبر grix-connector",
	"hi": "grix-connector के ज़रिए %s कनेक्ट करें",
}

var openclawIntros = localizedGuideText{
	"zh": "作为 OpenClaw 插件接入，凭据写进 OpenClaw 自己的配置",
	"en": "Connect as an OpenClaw plugin; credentials live in OpenClaw's own config",
	"ja": "OpenClaw プラグインとして接続。資格情報は OpenClaw 自身の設定に保存",
	"ko": "OpenClaw 플러그인으로 연결하며, 자격 증명은 OpenClaw 자체 설정에 저장됩니다",
	"de": "Als OpenClaw-Plugin verbinden; die Zugangsdaten liegen in OpenClaws eigener Konfiguration",
	"fr": "Connexion en tant que plugin OpenClaw ; les identifiants vivent dans la config d'OpenClaw",
	"es": "Se conecta como plugin de OpenClaw; las credenciales viven en la config de OpenClaw",
	"pt": "Conecta como plugin do OpenClaw; as credenciais ficam na config do próprio OpenClaw",
	"ru": "Подключение как плагин OpenClaw; учётные данные хранятся в конфиге самого OpenClaw",
	"ar": "الاتصال كإضافة OpenClaw؛ تُحفظ بيانات الاعتماد في إعدادات OpenClaw نفسها",
	"hi": "OpenClaw प्लगइन के रूप में कनेक्ट; क्रेडेंशियल OpenClaw की अपनी कॉन्फ़िग में रहते हैं",
}

var hermesIntros = localizedGuideText{
	"zh": "作为 Hermes 插件接入，凭据写进 profile 的 .env",
	"en": "Connect as a Hermes plugin; credentials live in the profile's .env",
	"ja": "Hermes プラグインとして接続。資格情報はプロファイルの .env に保存",
	"ko": "Hermes 플러그인으로 연결하며, 자격 증명은 프로필의 .env에 저장됩니다",
	"de": "Als Hermes-Plugin verbinden; die Zugangsdaten liegen in der .env des Profils",
	"fr": "Connexion en tant que plugin Hermes ; les identifiants vivent dans le .env du profil",
	"es": "Se conecta como plugin de Hermes; las credenciales viven en el .env del perfil",
	"pt": "Conecta como plugin do Hermes; as credenciais ficam no .env do perfil",
	"ru": "Подключение как плагин Hermes; учётные данные хранятся в .env профиля",
	"ar": "الاتصال كإضافة Hermes؛ تُحفظ بيانات الاعتماد في ملف ‎.env الخاص بالملف الشخصي",
	"hi": "Hermes प्लगइन के रूप में कनेक्ट; क्रेडेंशियल प्रोफ़ाइल की .env में रहते हैं",
}

// connectorTasks: the generic grix-connector task, four %s slots
// (cli phrase, install command, agents.json entry, cli phrase).
var connectorTasks = map[string]string{
	"zh": connectorTaskZh,
	"en": connectorTaskEn,
	"ja": connectorTaskJa,
	"ko": connectorTaskKo,
	"de": connectorTaskDe,
	"fr": connectorTaskFr,
	"es": connectorTaskEs,
	"pt": connectorTaskPt,
	"ru": connectorTaskRu,
	"ar": connectorTaskAr,
	"hi": connectorTaskHi,
}

// kimiConnectorTasks: the Kimi variant with its extra step 0, two %s slots
// (install command, agents.json entry).
var kimiConnectorTasks = map[string]string{
	"zh": kimiConnectorTaskZh,
	"en": kimiConnectorTaskEn,
	"ja": kimiConnectorTaskJa,
	"ko": kimiConnectorTaskKo,
	"de": kimiConnectorTaskDe,
	"fr": kimiConnectorTaskFr,
	"es": kimiConnectorTaskEs,
	"pt": kimiConnectorTaskPt,
	"ru": kimiConnectorTaskRu,
	"ar": kimiConnectorTaskAr,
	"hi": kimiConnectorTaskHi,
}

// deepseekConnectorTasks: the DeepSeek variant with step 0 installing pnpm
// then the official npm CLI. Two %s slots: install command, agents.json entry.
var deepseekConnectorTasks = map[string]string{
	"zh": deepseekConnectorTaskZh,
	"en": deepseekConnectorTaskEn,
	"ja": deepseekConnectorTaskJa,
	"ko": deepseekConnectorTaskKo,
	"de": deepseekConnectorTaskDe,
	"fr": deepseekConnectorTaskFr,
	"es": deepseekConnectorTaskEs,
	"pt": deepseekConnectorTaskPt,
	"ru": deepseekConnectorTaskRu,
	"ar": deepseekConnectorTaskAr,
	"hi": deepseekConnectorTaskHi,
}

// openclawTasks / hermesTasks carry no %s slots — the {{...}} placeholders are
// embedded directly and substituted by the client.
var openclawTasks = localizedGuideText{
	"zh": openclawTaskZh,
	"en": openclawTaskEn,
	"ja": openclawTaskJa,
	"ko": openclawTaskKo,
	"de": openclawTaskDe,
	"fr": openclawTaskFr,
	"es": openclawTaskEs,
	"pt": openclawTaskPt,
	"ru": openclawTaskRu,
	"ar": openclawTaskAr,
	"hi": openclawTaskHi,
}

var hermesTasks = localizedGuideText{
	"zh": hermesTaskZh,
	"en": hermesTaskEn,
	"ja": hermesTaskJa,
	"ko": hermesTaskKo,
	"de": hermesTaskDe,
	"fr": hermesTaskFr,
	"es": hermesTaskEs,
	"pt": hermesTaskPt,
	"ru": hermesTaskRu,
	"ar": hermesTaskAr,
	"hi": hermesTaskHi,
}
