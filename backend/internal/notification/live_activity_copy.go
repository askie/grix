package notification

import (
	"strings"

	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// liveActivityDetailMaxRunes 限制卡片副标题长度。灵动岛和锁屏横幅都只有一行，
// 超长文本会被系统截断成一串省略号，不如后端先切干净。
const liveActivityDetailMaxRunes = 60

// LiveActivityPhaseCopy 渲染实时活动卡片的文案：卡片副标题 detail，以及转入
// 「等着主人」阶段时那条 alert 的标题与正文（其余阶段 alertTitle 为空表示不响铃）。
//
// 放在 i18n.go 旁边是因为这里同样是"后端只出事实、文案只在一处组装"的那处：
// ws 侧钩子传的是原始 phase 与 agent 自己的一句话摘要，翻译只发生在这里。
func LiveActivityPhaseCopy(userID int64, phase, summary string) (alertTitle, alertBody, detail string) {
	lang := userPreferredLanguage(userID)
	summary = textutil.TruncateRunes(strings.TrimSpace(summary), liveActivityDetailMaxRunes)

	switch phase {
	case protocol.LiveActivityPhaseWaitingApproval:
		alertTitle = copyFor(lang, copyTitleApproval)
	case protocol.LiveActivityPhaseWaitingQuestion:
		alertTitle = copyFor(lang, copyTitleQuestion)
	case protocol.LiveActivityPhaseCompleted:
		return "", "", copyFor(lang, copyBodyCompleted)
	case protocol.LiveActivityPhaseFailed:
		if summary != "" {
			if reason := localizedFailReason(summary, lang); reason != "" {
				return "", "", copyFor(lang, copyFailedPrefix) + reason
			}
			if free := freeTextFailDetail(summary); free != "" {
				return "", "", copyFor(lang, copyFailedPrefix) + free
			}
		}
		return "", "", copyFor(lang, copyBodyFailed)
	case protocol.LiveActivityPhaseStopped:
		return "", "", copyFor(lang, copyTitleStopped)
	default:
		// running：卡片只显示任务标题，副标题留空。
		return "", "", summary
	}

	alertBody = summary
	if alertBody == "" {
		alertBody = alertTitle
	}
	return alertTitle, alertBody, alertBody
}

// LiveActivityPushEnabled 判断是否给这个用户下发实时活动卡片。不新增偏好项：
// 只要主人还把「推送」留在任一条 run 生命周期通知的通道里，卡片就发；把这些
// 事件全部改成只走 TTS / 全部关掉的用户，锁屏上也不该冒出卡片。
//
// 只在卡片开卡（start）时判定一次。卡已经在锁屏上之后，update / end 一律照发：
// 半截不更新、或者留一张永远停在 running 的僵尸卡，比多推一条更糟。
func LiveActivityPushEnabled(userID int64) bool {
	// 库没接上时不开卡。卡片上的每一个字都要从库里取，连偏好都读不到的时候
	// 推出去的只会是一张空卡；而且 ResolvePref 拿 nil 库会直接 panic，
	// 绝不能让一张锦上添花的卡片把 ws 节点带崩。
	if store.DB == nil {
		return false
	}
	for _, key := range liveActivityGateEventKeys {
		pref, err := ResolvePref(userID, key)
		if err != nil {
			// 读不到偏好时按放行处理，与 dispatchPush 一侧遇到设置抖动的取舍一致。
			return true
		}
		if pref.Enabled && pref.HasChannel(ChannelPush) {
			return true
		}
	}
	return false
}

// liveActivityGateEventKeys 是一次 run 生命周期会触达主人的全部事件。
// 上线 / 离线属于 agent 自身状态，与某一次 run 无关，不参与判定。
var liveActivityGateEventKeys = []string{
	EventApprovalRequested,
	EventAgentQuestion,
	EventTaskCompleted,
	EventTaskFailed,
	EventTaskStoppedUnexpected,
}
