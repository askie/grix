package notification

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/pkg/userpref"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// i18n.go renders all user-facing push copy in the recipient's preferred
// language. The ws-side hooks publish raw facts only (event key + raw reason /
// agent content); this file is the single place push copy is composed.

const defaultPushLang = "zh"

// Copy keys. Titles mirror the App's agent notification settings page wording
// (frontend/assets/i18n/*) so push and in-app terminology stay consistent.
const (
	copyTitleApproval  = "title_approval"
	copyTitleQuestion  = "title_question"
	copyTitleCompleted = "title_completed"
	copyTitleFailed    = "title_failed"
	copyTitleStopped   = "title_stopped"
	copyTitleStarted   = "title_started"
	copyTitleUnknown   = "title_unknown"
	copyTitleDefault   = "title_default"
	copyBodyApproval   = "body_approval"
	copyBodyQuestion   = "body_question"
	copyBodyStarted    = "body_started"
	copyBodyCompleted  = "body_completed"
	copyBodyStopped    = "body_stopped"
	copyBodyFailed     = "body_failed"
	copyBodyUnknown    = "body_unknown"
	copyFailedPrefix   = "failed_prefix"
	// Agent presence (online/offline) copy. "one" templates take the agent name
	// (%s); "many" templates take the agent count (%d).
	copyTitleAgentOnline     = "title_agent_online"
	copyTitleAgentOffline    = "title_agent_offline"
	copyBodyAgentOnlineOne   = "body_agent_online_one"
	copyBodyAgentOnlineMany  = "body_agent_online_many"
	copyBodyAgentOfflineOne  = "body_agent_offline_one"
	copyBodyAgentOfflineMany = "body_agent_offline_many"
)

// unknownStatusNotifyReasons render a task_failed event as "status unknown"
// copy instead of "failed". An ack timeout only proves the event was not
// confirmed within seconds — in production half the cases were real delivery
// failures and half were a momentarily stuck agent that finished fine.
// "Unknown, go check" is the only claim true for both; "failed" is wrong for
// the second half. The event still rides the task_failed key so user
// preference toggles keep working and no new event type is introduced.
var unknownStatusNotifyReasons = map[string]struct{}{
	protocol.AgentDeliveryCodeAckTimeout: {},
}

var pushCopy = map[string]map[string]string{
	"zh": {
		copyTitleApproval:        "审批请求",
		copyTitleQuestion:        "Agent 提问",
		copyTitleCompleted:       "任务完成",
		copyTitleFailed:          "任务失败",
		copyTitleStopped:         "任务意外停止",
		copyTitleStarted:         "任务开始",
		copyTitleDefault:         "Agent 通知",
		copyBodyStarted:          "任务开始执行",
		copyBodyApproval:         "有任务需要审批",
		copyBodyQuestion:         "Agent 向你提问",
		copyBodyCompleted:        "任务已完成",
		copyBodyStopped:          "任务意外停止，请打开会话查看",
		copyBodyFailed:           "任务失败，请打开会话查看原因",
		copyTitleUnknown:         "任务状态未知",
		copyBodyUnknown:          "Agent 未确认收到任务，请打开会话查看",
		copyFailedPrefix:         "任务失败：",
		copyTitleAgentOnline:     "Agent 上线",
		copyTitleAgentOffline:    "Agent 离线",
		copyBodyAgentOnlineOne:   "%s 已上线",
		copyBodyAgentOnlineMany:  "%d 个 Agent 已上线",
		copyBodyAgentOfflineOne:  "%s 已离线",
		copyBodyAgentOfflineMany: "%d 个 Agent 已离线",
	},
	"en": {
		copyTitleApproval:        "Approval request",
		copyTitleQuestion:        "Agent question",
		copyTitleCompleted:       "Task completed",
		copyTitleFailed:          "Task failed",
		copyTitleStopped:         "Task stopped unexpectedly",
		copyTitleStarted:         "Task started",
		copyTitleDefault:         "Agent notification",
		copyBodyStarted:          "The task has started",
		copyBodyApproval:         "A task needs your approval",
		copyBodyQuestion:         "The agent has a question for you",
		copyBodyCompleted:        "The task has completed",
		copyBodyStopped:          "The task stopped unexpectedly",
		copyBodyFailed:           "The task failed. Open the chat to see why",
		copyTitleUnknown:         "Task status unknown",
		copyBodyUnknown:          "The agent did not confirm receiving the task. Open the chat to check",
		copyFailedPrefix:         "Task failed: ",
		copyTitleAgentOnline:     "Agent online",
		copyTitleAgentOffline:    "Agent offline",
		copyBodyAgentOnlineOne:   "%s came online",
		copyBodyAgentOnlineMany:  "%d agents came online",
		copyBodyAgentOfflineOne:  "%s went offline",
		copyBodyAgentOfflineMany: "%d agents went offline",
	},
	"ja": {
		copyTitleApproval:        "承認リクエスト",
		copyTitleQuestion:        "エージェントの質問",
		copyTitleCompleted:       "タスク完了",
		copyTitleFailed:          "タスク失敗",
		copyTitleStopped:         "タスクが予期せず停止",
		copyTitleStarted:         "タスク開始",
		copyTitleDefault:         "エージェント通知",
		copyBodyStarted:          "タスクの実行を開始しました",
		copyBodyApproval:         "承認が必要なタスクがあります",
		copyBodyQuestion:         "エージェントから質問があります",
		copyBodyCompleted:        "タスクが完了しました",
		copyBodyStopped:          "タスクが予期せず停止しました",
		copyBodyFailed:           "タスクが失敗しました。チャットを開いて原因をご確認ください",
		copyTitleUnknown:         "タスクの状態が不明",
		copyBodyUnknown:          "エージェントがタスクの受信を確認しませんでした。チャットを開いてご確認ください",
		copyFailedPrefix:         "タスク失敗：",
		copyTitleAgentOnline:     "エージェントがオンライン",
		copyTitleAgentOffline:    "エージェントがオフライン",
		copyBodyAgentOnlineOne:   "%s がオンラインになりました",
		copyBodyAgentOnlineMany:  "%d 個のエージェントがオンラインになりました",
		copyBodyAgentOfflineOne:  "%s がオフラインになりました",
		copyBodyAgentOfflineMany: "%d 個のエージェントがオフラインになりました",
	},
	"ko": {
		copyTitleApproval:        "승인 요청",
		copyTitleQuestion:        "에이전트 질문",
		copyTitleCompleted:       "작업 완료",
		copyTitleFailed:          "작업 실패",
		copyTitleStopped:         "작업이 예기치 않게 중지됨",
		copyTitleStarted:         "작업 시작",
		copyTitleDefault:         "에이전트 알림",
		copyBodyStarted:          "작업 실행이 시작되었습니다",
		copyBodyApproval:         "승인이 필요한 작업이 있습니다",
		copyBodyQuestion:         "에이전트가 질문했습니다",
		copyBodyCompleted:        "작업이 완료되었습니다",
		copyBodyStopped:          "작업이 예기치 않게 중지되었습니다",
		copyBodyFailed:           "작업이 실패했습니다. 대화를 열어 원인을 확인해 주세요",
		copyTitleUnknown:         "작업 상태를 알 수 없음",
		copyBodyUnknown:          "에이전트가 작업 수신을 확인하지 않았습니다. 대화를 열어 확인해 주세요",
		copyFailedPrefix:         "작업 실패: ",
		copyTitleAgentOnline:     "에이전트 온라인",
		copyTitleAgentOffline:    "에이전트 오프라인",
		copyBodyAgentOnlineOne:   "%s 온라인 상태가 되었습니다",
		copyBodyAgentOnlineMany:  "에이전트 %d개가 온라인 상태가 되었습니다",
		copyBodyAgentOfflineOne:  "%s 오프라인 상태가 되었습니다",
		copyBodyAgentOfflineMany: "에이전트 %d개가 오프라인 상태가 되었습니다",
	},
	"de": {
		copyTitleApproval:        "Freigabeanfrage",
		copyTitleQuestion:        "Agent-Frage",
		copyTitleCompleted:       "Aufgabe abgeschlossen",
		copyTitleFailed:          "Aufgabe fehlgeschlagen",
		copyTitleStopped:         "Aufgabe unerwartet gestoppt",
		copyTitleStarted:         "Aufgabe gestartet",
		copyTitleDefault:         "Agent-Benachrichtigung",
		copyBodyStarted:          "Die Aufgabe wurde gestartet",
		copyBodyApproval:         "Eine Aufgabe benötigt Ihre Freigabe",
		copyBodyQuestion:         "Der Agent hat eine Frage an Sie",
		copyBodyCompleted:        "Die Aufgabe wurde abgeschlossen",
		copyBodyStopped:          "Die Aufgabe wurde unerwartet gestoppt",
		copyBodyFailed:           "Die Aufgabe ist fehlgeschlagen. Öffnen Sie den Chat, um die Ursache zu sehen",
		copyTitleUnknown:         "Aufgabenstatus unbekannt",
		copyBodyUnknown:          "Der Agent hat den Empfang der Aufgabe nicht bestätigt. Öffnen Sie den Chat, um nachzusehen",
		copyFailedPrefix:         "Aufgabe fehlgeschlagen: ",
		copyTitleAgentOnline:     "Agent online",
		copyTitleAgentOffline:    "Agent offline",
		copyBodyAgentOnlineOne:   "%s ist online",
		copyBodyAgentOnlineMany:  "%d Agenten sind online",
		copyBodyAgentOfflineOne:  "%s ist offline",
		copyBodyAgentOfflineMany: "%d Agenten sind offline",
	},
	"fr": {
		copyTitleApproval:        "Demande d'approbation",
		copyTitleQuestion:        "Question de l'agent",
		copyTitleCompleted:       "Tâche terminée",
		copyTitleFailed:          "Échec de la tâche",
		copyTitleStopped:         "Tâche arrêtée de façon inattendue",
		copyTitleStarted:         "Tâche démarrée",
		copyTitleDefault:         "Notification de l'agent",
		copyBodyStarted:          "La tâche a démarré",
		copyBodyApproval:         "Une tâche nécessite votre approbation",
		copyBodyQuestion:         "L'agent vous pose une question",
		copyBodyCompleted:        "La tâche est terminée",
		copyBodyStopped:          "La tâche s'est arrêtée de façon inattendue",
		copyBodyFailed:           "La tâche a échoué. Ouvrez la conversation pour en voir la raison",
		copyTitleUnknown:         "Statut de la tâche inconnu",
		copyBodyUnknown:          "L'agent n'a pas confirmé la réception de la tâche. Ouvrez la conversation pour vérifier",
		copyFailedPrefix:         "Échec de la tâche : ",
		copyTitleAgentOnline:     "Agent en ligne",
		copyTitleAgentOffline:    "Agent hors ligne",
		copyBodyAgentOnlineOne:   "%s est en ligne",
		copyBodyAgentOnlineMany:  "%d agents sont en ligne",
		copyBodyAgentOfflineOne:  "%s est hors ligne",
		copyBodyAgentOfflineMany: "%d agents sont hors ligne",
	},
	"es": {
		copyTitleApproval:        "Solicitud de aprobación",
		copyTitleQuestion:        "Pregunta del agente",
		copyTitleCompleted:       "Tarea completada",
		copyTitleFailed:          "Tarea fallida",
		copyTitleStopped:         "Tarea detenida inesperadamente",
		copyTitleStarted:         "Tarea iniciada",
		copyTitleDefault:         "Notificación del agente",
		copyBodyStarted:          "La tarea ha comenzado",
		copyBodyApproval:         "Una tarea necesita tu aprobación",
		copyBodyQuestion:         "El agente tiene una pregunta para ti",
		copyBodyCompleted:        "La tarea se ha completado",
		copyBodyStopped:          "La tarea se detuvo inesperadamente",
		copyBodyFailed:           "La tarea ha fallado. Abre la conversación para ver el motivo",
		copyTitleUnknown:         "Estado de la tarea desconocido",
		copyBodyUnknown:          "El agente no confirmó la recepción de la tarea; abre la conversación para comprobarlo",
		copyFailedPrefix:         "Tarea fallida: ",
		copyTitleAgentOnline:     "Agente en línea",
		copyTitleAgentOffline:    "Agente sin conexión",
		copyBodyAgentOnlineOne:   "%s está en línea",
		copyBodyAgentOnlineMany:  "%d agentes están en línea",
		copyBodyAgentOfflineOne:  "%s se desconectó",
		copyBodyAgentOfflineMany: "%d agentes se desconectaron",
	},
	"pt": {
		copyTitleApproval:        "Solicitação de aprovação",
		copyTitleQuestion:        "Pergunta do agente",
		copyTitleCompleted:       "Tarefa concluída",
		copyTitleFailed:          "Tarefa falhou",
		copyTitleStopped:         "Tarefa interrompida inesperadamente",
		copyTitleStarted:         "Tarefa iniciada",
		copyTitleDefault:         "Notificação do agente",
		copyBodyStarted:          "A tarefa foi iniciada",
		copyBodyApproval:         "Uma tarefa precisa da sua aprovação",
		copyBodyQuestion:         "O agente tem uma pergunta para você",
		copyBodyCompleted:        "A tarefa foi concluída",
		copyBodyStopped:          "A tarefa foi interrompida inesperadamente",
		copyBodyFailed:           "A tarefa falhou. Abra a conversa para ver o motivo",
		copyTitleUnknown:         "Status da tarefa desconhecido",
		copyBodyUnknown:          "O agente não confirmou o recebimento da tarefa. Abra a conversa para verificar",
		copyFailedPrefix:         "Tarefa falhou: ",
		copyTitleAgentOnline:     "Agente on-line",
		copyTitleAgentOffline:    "Agente off-line",
		copyBodyAgentOnlineOne:   "%s ficou on-line",
		copyBodyAgentOnlineMany:  "%d agentes ficaram on-line",
		copyBodyAgentOfflineOne:  "%s ficou off-line",
		copyBodyAgentOfflineMany: "%d agentes ficaram off-line",
	},
	"ru": {
		copyTitleApproval:        "Запрос на одобрение",
		copyTitleQuestion:        "Вопрос агента",
		copyTitleCompleted:       "Задача выполнена",
		copyTitleFailed:          "Задача не выполнена",
		copyTitleStopped:         "Задача неожиданно остановлена",
		copyTitleStarted:         "Задача запущена",
		copyTitleDefault:         "Уведомление агента",
		copyBodyStarted:          "Выполнение задачи началось",
		copyBodyApproval:         "Задача требует вашего подтверждения",
		copyBodyQuestion:         "Агент задал вам вопрос",
		copyBodyCompleted:        "Задача успешно завершена",
		copyBodyStopped:          "Задача неожиданно остановилась",
		copyBodyFailed:           "Задача завершилась с ошибкой. Откройте чат, чтобы узнать причину",
		copyTitleUnknown:         "Статус задачи неизвестен",
		copyBodyUnknown:          "Агент не подтвердил получение задачи. Откройте чат, чтобы проверить",
		copyFailedPrefix:         "Задача не выполнена: ",
		copyTitleAgentOnline:     "Агент в сети",
		copyTitleAgentOffline:    "Агент не в сети",
		copyBodyAgentOnlineOne:   "%s в сети",
		copyBodyAgentOnlineMany:  "%d агентов в сети",
		copyBodyAgentOfflineOne:  "%s не в сети",
		copyBodyAgentOfflineMany: "%d агентов не в сети",
	},
	"ar": {
		copyTitleApproval:        "طلب موافقة",
		copyTitleQuestion:        "سؤال الوكيل",
		copyTitleCompleted:       "اكتملت المهمة",
		copyTitleFailed:          "فشلت المهمة",
		copyTitleStopped:         "توقفت المهمة بشكل غير متوقع",
		copyTitleStarted:         "بدأت المهمة",
		copyTitleDefault:         "إشعار الوكيل",
		copyBodyStarted:          "بدأ تنفيذ المهمة",
		copyBodyApproval:         "هناك مهمة تحتاج إلى موافقتك",
		copyBodyQuestion:         "الوكيل يطرح عليك سؤالاً",
		copyBodyCompleted:        "اكتملت المهمة بنجاح",
		copyBodyStopped:          "توقفت المهمة بشكل غير متوقع. افتح المحادثة للتحقق",
		copyBodyFailed:           "فشلت المهمة. افتح المحادثة لمعرفة السبب",
		copyTitleUnknown:         "حالة المهمة غير معروفة",
		copyBodyUnknown:          "لم يؤكد الوكيل استلام المهمة. افتح المحادثة للتحقق",
		copyFailedPrefix:         "فشلت المهمة: ",
		copyTitleAgentOnline:     "الوكيل متصل",
		copyTitleAgentOffline:    "الوكيل غير متصل",
		copyBodyAgentOnlineOne:   "%s أصبح متصلاً",
		copyBodyAgentOnlineMany:  "أصبح %d وكيلاً متصلاً",
		copyBodyAgentOfflineOne:  "%s أصبح غير متصل",
		copyBodyAgentOfflineMany: "أصبح %d وكيلاً غير متصل",
	},
	"hi": {
		copyTitleApproval:        "स्वीकृति अनुरोध",
		copyTitleQuestion:        "एजेंट का प्रश्न",
		copyTitleCompleted:       "कार्य पूर्ण",
		copyTitleFailed:          "कार्य विफल",
		copyTitleStopped:         "कार्य अप्रत्याशित रूप से रुका",
		copyTitleStarted:         "कार्य आरंभ",
		copyTitleDefault:         "एजेंट सूचना",
		copyBodyStarted:          "कार्य शुरू हो गया है",
		copyBodyApproval:         "एक कार्य को आपकी स्वीकृति चाहिए",
		copyBodyQuestion:         "एजेंट ने आपसे एक प्रश्न पूछा है",
		copyBodyCompleted:        "कार्य पूरा हो गया है",
		copyBodyStopped:          "कार्य अप्रत्याशित रूप से रुक गया",
		copyBodyFailed:           "कार्य विफल हो गया। कारण देखने के लिए चैट खोलें",
		copyTitleUnknown:         "कार्य की स्थिति अज्ञात",
		copyBodyUnknown:          "एजेंट ने कार्य प्राप्ति की पुष्टि नहीं की। जांचने के लिए चैट खोलें",
		copyFailedPrefix:         "कार्य विफल: ",
		copyTitleAgentOnline:     "एजेंट ऑनलाइन",
		copyTitleAgentOffline:    "एजेंट ऑफ़लाइन",
		copyBodyAgentOnlineOne:   "%s ऑनलाइन हो गया",
		copyBodyAgentOnlineMany:  "%d एजेंट ऑनलाइन हो गए",
		copyBodyAgentOfflineOne:  "%s ऑफ़लाइन हो गया",
		copyBodyAgentOfflineMany: "%d एजेंट ऑफ़लाइन हो गए",
	},
}

// Failure-reason copy keys. The ws-side hooks carry the raw machine stop-reason
// code in Summary (e.g. "agent_api_event_result_timeout"); rendering it verbatim
// would show internal English codes to users. Known codes map to localized
// phrases here; unknown or free-text reasons fall back to the generic body.
const (
	reasonChannelUnavailable = "reason_channel_unavailable"
	reasonAckTimeout         = "reason_ack_timeout"
	reasonResultTimeout      = "reason_result_timeout"
	reasonProcessingFailed   = "reason_processing_failed"
	reasonNoReply            = "reason_no_reply"
	reasonStopFailed         = "reason_stop_failed"
	reasonInvalidCwd         = "reason_invalid_cwd"
	reasonIdleTimeout        = "reason_idle_timeout"
	reasonHardTimeout        = "reason_hard_timeout"
	reasonInterrupted        = "reason_interrupted"
	reasonStale              = "reason_stale"
)

// stopReasonCopyKey maps every raw stop-reason code emitted by the ws service
// or agent connectors to its copy key. Protocol codes are referenced as
// constants; the connector-side codes below have no Go constant and are a
// literal contract with grix-connector — if it renames one, localizedFailReason
// logs a warning rather than silently degrading to the generic body.
//
// AgentDeliveryCodeCanceled is deliberately absent: user-initiated stops are
// filtered by isUserInitiatedStopReason before any task_failed event is
// published, so they never reach this table.
var stopReasonCopyKey = map[string]string{
	protocol.AgentDeliveryCodeChannelUnavailable: reasonChannelUnavailable,
	protocol.AgentDeliveryCodeAckTimeout:         reasonAckTimeout,
	protocol.AgentDeliveryCodeResultTimeout:      reasonResultTimeout,
	protocol.AgentDeliveryCodeProcessingFailed:   reasonProcessingFailed,
	protocol.AgentDeliveryCodeAgentStopFailure:   reasonNoReply,
	"event_stop_failed":                          reasonStopFailed,
	"session_invalid_cwd":                        reasonInvalidCwd,
	"agent_idle_timeout":                         reasonIdleTimeout,
	"agent_hard_timeout":                         reasonHardTimeout,
	"worker_interrupted":                         reasonInterrupted,
	protocol.AgentDeliveryCodeEventStale:         reasonStale,
}

var failReasonCopy = map[string]map[string]string{
	"zh": {
		reasonChannelUnavailable: "Agent 连接不可用",
		reasonAckTimeout:         "Agent 未响应",
		reasonResultTimeout:      "长时间未返回结果",
		reasonProcessingFailed:   "消息处理出错",
		reasonNoReply:            "Agent 中断，未能完成回复",
		reasonStopFailed:         "停止请求失败",
		reasonInvalidCwd:         "会话工作目录无效",
		reasonIdleTimeout:        "任务长时间无活动",
		reasonHardTimeout:        "任务运行超时",
		reasonInterrupted:        "任务被中断",
		reasonStale:              "任务已过期",
	},
	"en": {
		reasonChannelUnavailable: "the agent connection is unavailable",
		reasonAckTimeout:         "the agent did not respond",
		reasonResultTimeout:      "no result was returned in time",
		reasonProcessingFailed:   "an error occurred while processing the message",
		reasonNoReply:            "the agent was interrupted before completing its reply",
		reasonStopFailed:         "the stop request failed",
		reasonInvalidCwd:         "the session working directory is invalid",
		reasonIdleTimeout:        "the task was idle for too long",
		reasonHardTimeout:        "the task ran too long",
		reasonInterrupted:        "the task was interrupted",
		reasonStale:              "the task has expired",
	},
	"ja": {
		reasonChannelUnavailable: "エージェント接続が利用できません",
		reasonAckTimeout:         "エージェントが応答しませんでした",
		reasonResultTimeout:      "結果が時間内に返されませんでした",
		reasonProcessingFailed:   "メッセージ処理中にエラーが発生しました",
		reasonNoReply:            "エージェントが中断され、返信を完了できませんでした",
		reasonStopFailed:         "停止リクエストが失敗しました",
		reasonInvalidCwd:         "セッションの作業ディレクトリが無効です",
		reasonIdleTimeout:        "タスクが長時間アイドル状態でした",
		reasonHardTimeout:        "タスクの実行時間が長すぎました",
		reasonInterrupted:        "タスクが中断されました",
		reasonStale:              "タスクの有効期限が切れました",
	},
	"ko": {
		reasonChannelUnavailable: "에이전트 연결을 사용할 수 없습니다",
		reasonAckTimeout:         "에이전트가 응답하지 않았습니다",
		reasonResultTimeout:      "제시간에 결과가 반환되지 않았습니다",
		reasonProcessingFailed:   "메시지 처리 중 오류가 발생했습니다",
		reasonNoReply:            "에이전트가 중단되어 응답을 완료하지 못했습니다",
		reasonStopFailed:         "중지 요청이 실패했습니다",
		reasonInvalidCwd:         "세션 작업 디렉터리가 유효하지 않습니다",
		reasonIdleTimeout:        "작업이 오랫동안 유휴 상태였습니다",
		reasonHardTimeout:        "작업 실행 시간이 초과되었습니다",
		reasonInterrupted:        "작업이 중단되었습니다",
		reasonStale:              "작업이 만료되었습니다",
	},
	"de": {
		reasonChannelUnavailable: "Die Agent-Verbindung ist nicht verfügbar",
		reasonAckTimeout:         "Der Agent hat nicht reagiert",
		reasonResultTimeout:      "Es wurde nicht rechtzeitig ein Ergebnis zurückgegeben",
		reasonProcessingFailed:   "Bei der Verarbeitung der Nachricht ist ein Fehler aufgetreten",
		reasonNoReply:            "Der Agent wurde unterbrochen, bevor die Antwort abgeschlossen war",
		reasonStopFailed:         "Die Stopp-Anfrage ist fehlgeschlagen",
		reasonInvalidCwd:         "Das Arbeitsverzeichnis der Sitzung ist ungültig",
		reasonIdleTimeout:        "Die Aufgabe war zu lange inaktiv",
		reasonHardTimeout:        "Die Aufgabe lief zu lange",
		reasonInterrupted:        "Die Aufgabe wurde unterbrochen",
		reasonStale:              "Die Aufgabe ist abgelaufen",
	},
	"fr": {
		reasonChannelUnavailable: "la connexion de l'agent est indisponible",
		reasonAckTimeout:         "l'agent n'a pas répondu",
		reasonResultTimeout:      "aucun résultat n'a été renvoyé à temps",
		reasonProcessingFailed:   "une erreur s'est produite lors du traitement du message",
		reasonNoReply:            "l'agent a été interrompu avant de terminer sa réponse",
		reasonStopFailed:         "la demande d'arrêt a échoué",
		reasonInvalidCwd:         "le répertoire de travail de la session est invalide",
		reasonIdleTimeout:        "la tâche est restée inactive trop longtemps",
		reasonHardTimeout:        "la tâche a duré trop longtemps",
		reasonInterrupted:        "la tâche a été interrompue",
		reasonStale:              "la tâche a expiré",
	},
	"es": {
		reasonChannelUnavailable: "la conexión del agente no está disponible",
		reasonAckTimeout:         "el agente no respondió",
		reasonResultTimeout:      "no se devolvió ningún resultado a tiempo",
		reasonProcessingFailed:   "ocurrió un error al procesar el mensaje",
		reasonNoReply:            "el agente fue interrumpido antes de completar su respuesta",
		reasonStopFailed:         "la solicitud de detención falló",
		reasonInvalidCwd:         "el directorio de trabajo de la sesión no es válido",
		reasonIdleTimeout:        "la tarea estuvo inactiva demasiado tiempo",
		reasonHardTimeout:        "la tarea se ejecutó demasiado tiempo",
		reasonInterrupted:        "la tarea fue interrumpida",
		reasonStale:              "la tarea ha caducado",
	},
	"pt": {
		reasonChannelUnavailable: "a conexão do agente está indisponível",
		reasonAckTimeout:         "o agente não respondeu",
		reasonResultTimeout:      "nenhum resultado foi retornado a tempo",
		reasonProcessingFailed:   "ocorreu um erro ao processar a mensagem",
		reasonNoReply:            "o agente foi interrompido antes de concluir a resposta",
		reasonStopFailed:         "a solicitação de parada falhou",
		reasonInvalidCwd:         "o diretório de trabalho da sessão é inválido",
		reasonIdleTimeout:        "a tarefa ficou ociosa por muito tempo",
		reasonHardTimeout:        "a tarefa demorou demais",
		reasonInterrupted:        "a tarefa foi interrompida",
		reasonStale:              "a tarefa expirou",
	},
	"ru": {
		reasonChannelUnavailable: "соединение с агентом недоступно",
		reasonAckTimeout:         "агент не ответил",
		reasonResultTimeout:      "результат не был возвращён вовремя",
		reasonProcessingFailed:   "при обработке сообщения произошла ошибка",
		reasonNoReply:            "агент был прерван, не завершив ответ",
		reasonStopFailed:         "запрос на остановку не удался",
		reasonInvalidCwd:         "рабочий каталог сессии недействителен",
		reasonIdleTimeout:        "задача слишком долго бездействовала",
		reasonHardTimeout:        "задача выполнялась слишком долго",
		reasonInterrupted:        "задача была прервана",
		reasonStale:              "задача устарела",
	},
	"ar": {
		reasonChannelUnavailable: "اتصال الوكيل غير متاح",
		reasonAckTimeout:         "لم يستجب الوكيل",
		reasonResultTimeout:      "لم يتم إرجاع نتيجة في الوقت المحدد",
		reasonProcessingFailed:   "حدث خطأ أثناء معالجة الرسالة",
		reasonNoReply:            "تمت مقاطعة الوكيل قبل إكمال رده",
		reasonStopFailed:         "فشل طلب الإيقاف",
		reasonInvalidCwd:         "دليل عمل الجلسة غير صالح",
		reasonIdleTimeout:        "ظلت المهمة خاملة لفترة طويلة",
		reasonHardTimeout:        "استغرقت المهمة وقتاً طويلاً",
		reasonInterrupted:        "تمت مقاطعة المهمة",
		reasonStale:              "انتهت صلاحية المهمة",
	},
	"hi": {
		reasonChannelUnavailable: "एजेंट कनेक्शन उपलब्ध नहीं है",
		reasonAckTimeout:         "एजेंट ने प्रतिक्रिया नहीं दी",
		reasonResultTimeout:      "समय पर कोई परिणाम नहीं मिला",
		reasonProcessingFailed:   "संदेश संसाधित करते समय त्रुटि हुई",
		reasonNoReply:            "उत्तर पूरा करने से पहले एजेंट बाधित हो गया",
		reasonStopFailed:         "रोकने का अनुरोध विफल रहा",
		reasonInvalidCwd:         "सत्र की कार्य निर्देशिका अमान्य है",
		reasonIdleTimeout:        "कार्य बहुत देर तक निष्क्रिय रहा",
		reasonHardTimeout:        "कार्य बहुत देर तक चला",
		reasonInterrupted:        "कार्य बाधित हो गया",
		reasonStale:              "कार्य की अवधि समाप्त हो गई",
	},
}

// localizedFailReason renders a known stop-reason code in the given language.
// Returns "" for unknown codes and free-text reasons — the caller then uses the
// generic failure body instead of leaking internal English strings to users.
//
// A non-empty reason that misses the table is logged: it means either a new code
// was added without copy, or a connector renamed one. Both silently degrade the
// push to the generic body, so the log is the only signal.
//
// The defaultPushLang fallback assumes the zh table covers every key — an
// invariant enforced by TestFailReasonCopyComplete.
// AgentDeliveryFailReason returns the localized human copy for a delivery
// failure code, or "" when the code has no mapped copy. Unlike
// localizedFailReason it never logs: callers use it as a best-effort fallback.
func AgentDeliveryFailReason(code, lang string) string {
	key, ok := stopReasonCopyKey[code]
	if !ok {
		return ""
	}
	if table, ok := failReasonCopy[lang]; ok {
		if s := table[key]; s != "" {
			return s
		}
	}
	return failReasonCopy[defaultPushLang][key]
}

func localizedFailReason(reason, lang string) string {
	key, ok := stopReasonCopyKey[reason]
	if !ok {
		if looksLikeStopReasonCode(reason) && logger.L != nil {
			logger.L.Warnf("notification: unmapped task_failed stop reason %q, falling back to generic body", reason)
		}
		return ""
	}
	if table, ok := failReasonCopy[lang]; ok {
		if s := table[key]; s != "" {
			return s
		}
	}
	return failReasonCopy[defaultPushLang][key]
}

// failDetailMaxRunes bounds the free-text detail rendered into a push body.
// The ws side already truncates the stop reason to 80 runes; this keeps the
// bound local so the copy layer never emits an unbounded body.
const failDetailMaxRunes = 80

// looksLikeStopReasonCode reports whether a reason is a machine identifier
// (e.g. "agent_api_event_processing_failed") rather than a human sentence.
// Codes never contain whitespace; connector and third-party agent messages
// always do.
func looksLikeStopReasonCode(reason string) bool {
	reason = strings.TrimSpace(reason)
	return reason != "" && !strings.ContainsAny(reason, " \t\n\u3000")
}

// freeTextFailDetail renders an unmapped stop reason as push detail when it is
// a human-readable message. Connectors and third-party agents report their real
// failure only in this free text (e.g. "Hermes finished without producing a
// reply"); dropping it left the body identical to the title, which told the
// user nothing. Unmapped machine codes are still withheld — they would leak
// internal identifiers without explaining anything.
func freeTextFailDetail(reason string) string {
	if looksLikeStopReasonCode(reason) {
		return ""
	}
	// Same shape as the in-session failure notice: first non-empty line, one
	// line of collapsed whitespace. A multi-line stack trace is unreadable in a
	// notification and would push the useful first line off screen.
	for _, line := range strings.Split(reason, "\n") {
		line = strings.TrimSpace(failDetailWhitespace.ReplaceAllString(line, " "))
		if line == "" {
			continue
		}
		return textutil.TruncateRunes(line, failDetailMaxRunes)
	}
	return ""
}

var failDetailWhitespace = regexp.MustCompile(`\s+`)

// userPreferredLanguage returns the recipient's normalized language code via
// the unified userpref.Language entry. Falls back to zh when the push copy
// table does not cover that language.
func userPreferredLanguage(userID int64) string {
	lang := userpref.Language(context.Background(), userID)
	if _, ok := pushCopy[lang]; !ok {
		return defaultPushLang
	}
	return lang
}

// presenceTitle renders the agent online/offline notification title.
func presenceTitle(kind, lang string) string {
	if kind == presenceKindOffline {
		return copyFor(lang, copyTitleAgentOffline)
	}
	return copyFor(lang, copyTitleAgentOnline)
}

// presenceBody renders the body: a single agent by name, or a merged count.
func presenceBody(kind, lang string, names []string) string {
	one, many := copyBodyAgentOnlineOne, copyBodyAgentOnlineMany
	if kind == presenceKindOffline {
		one, many = copyBodyAgentOfflineOne, copyBodyAgentOfflineMany
	}
	if len(names) == 1 {
		return fmt.Sprintf(copyFor(lang, one), names[0])
	}
	return fmt.Sprintf(copyFor(lang, many), len(names))
}

func copyFor(lang, key string) string {
	if table, ok := pushCopy[lang]; ok {
		if s, ok := table[key]; ok {
			return s
		}
	}
	return pushCopy[defaultPushLang][key]
}

// pushTitle renders the notification title for an event in the given language.
// task_failed events carrying an unknown-status reason (ack timeout) are
// retitled "status unknown" — see unknownStatusNotifyReasons.
func pushTitle(evt *AgentNotificationEvent, lang string) string {
	switch evt.EventKey {
	case EventApprovalRequested:
		return copyFor(lang, copyTitleApproval)
	case EventAgentQuestion:
		return copyFor(lang, copyTitleQuestion)
	case EventTaskCompleted:
		return copyFor(lang, copyTitleCompleted)
	case EventTaskFailed:
		if _, ok := unknownStatusNotifyReasons[evt.Summary]; ok {
			return copyFor(lang, copyTitleUnknown)
		}
		return copyFor(lang, copyTitleFailed)
	case EventTaskStoppedUnexpected:
		return copyFor(lang, copyTitleStopped)
	case EventTaskStarted:
		return copyFor(lang, copyTitleStarted)
	default:
		return copyFor(lang, copyTitleDefault)
	}
}

// pushBody renders the notification body. Lifecycle events get fixed localized
// copy; callbackable events pass the agent's own content through untranslated.
//
// task_failed detail precedence: unknown-status reasons render the dedicated
// unknown body, then the agent's free-text Detail is passed through, then the
// stop-reason code in Summary is mapped to a localized phrase, then the generic
// failure body. Detail wins over the mapped phrase for the same reason the
// in-session failure notice prefers it (see buildAgentDeliveryFailureMessageContent):
// the code is usually the generic catch-all the backend filled in, whose phrase
// ("an error occurred while processing the message") says nothing, while Detail
// is the agent's actual verdict. The cost is that Detail is in whatever language
// the agent emitted; the user reads the same text in-session either way. An
// unmapped machine code is never rendered — internal identifiers explain nothing.
func pushBody(evt *AgentNotificationEvent, lang string) string {
	switch evt.EventKey {
	case EventApprovalRequested:
		// Summary carries the command awaiting approval; empty means the card
		// payload was unparseable, so fall back to generic copy rather than
		// pushing an empty body.
		if evt.Summary == "" {
			return copyFor(lang, copyBodyApproval)
		}
		return evt.Summary
	case EventAgentQuestion:
		if evt.Summary == "" {
			return copyFor(lang, copyBodyQuestion)
		}
		return evt.Summary
	case EventTaskStarted:
		return copyFor(lang, copyBodyStarted)
	case EventTaskCompleted:
		return copyFor(lang, copyBodyCompleted)
	case EventTaskStoppedUnexpected:
		return copyFor(lang, copyBodyStopped)
	case EventTaskFailed:
		if _, ok := unknownStatusNotifyReasons[evt.Summary]; ok {
			return copyFor(lang, copyBodyUnknown)
		}
		if detail := freeTextFailDetail(evt.Detail); detail != "" {
			return copyFor(lang, copyFailedPrefix) + detail
		}
		if reason := localizedFailReason(evt.Summary, lang); reason != "" {
			return copyFor(lang, copyFailedPrefix) + reason
		}
		// Summary itself may still hold free text: a ws node publishing the
		// pre-Detail payload shape during a rolling deploy, or a replayed
		// JetStream message from before it.
		if detail := freeTextFailDetail(evt.Summary); detail != "" {
			return copyFor(lang, copyFailedPrefix) + detail
		}
		return copyFor(lang, copyBodyFailed)
	default:
		return evt.Summary
	}
}
