package handler

type agentDeliveryFailureCopy struct {
	ackTimeout  string
	queueFull   string
	unavailable string
}

const defaultAgentDeliveryFailureLanguage = "zh"

var agentDeliveryFailureCopyByLanguage = map[string]agentDeliveryFailureCopy{
	"zh": {
		ackTimeout:  "智能体响应超时，请稍后重试。",
		queueFull:   "智能体消息队列已满，请稍后重试。",
		unavailable: "智能体暂时不可用，请稍后重试。",
	},
	"en": {
		ackTimeout:  "The agent response timed out. Please try again later.",
		queueFull:   "The agent's message queue is full. Please try again later.",
		unavailable: "The agent is temporarily unavailable. Please try again later.",
	},
	"ja": {
		ackTimeout:  "エージェントの応答がタイムアウトしました。しばらくしてからもう一度お試しください。",
		queueFull:   "エージェントのメッセージキューがいっぱいです。しばらくしてからもう一度お試しください。",
		unavailable: "エージェントは一時的に利用できません。しばらくしてからもう一度お試しください。",
	},
	"ko": {
		ackTimeout:  "에이전트 응답 시간이 초과되었습니다. 잠시 후 다시 시도해 주세요.",
		queueFull:   "에이전트의 메시지 대기열이 가득 찼습니다. 잠시 후 다시 시도해 주세요.",
		unavailable: "에이전트를 일시적으로 사용할 수 없습니다. 잠시 후 다시 시도해 주세요.",
	},
	"de": {
		ackTimeout:  "Zeitüberschreitung bei der Agent-Antwort. Bitte versuchen Sie es später erneut.",
		queueFull:   "Die Nachrichtenwarteschlange des Agenten ist voll. Bitte versuchen Sie es später erneut.",
		unavailable: "Der Agent ist vorübergehend nicht verfügbar. Bitte versuchen Sie es später erneut.",
	},
	"fr": {
		ackTimeout:  "La réponse de l’agent a expiré. Veuillez réessayer plus tard.",
		queueFull:   "La file d’attente des messages de l’agent est pleine. Veuillez réessayer plus tard.",
		unavailable: "L’agent est temporairement indisponible. Veuillez réessayer plus tard.",
	},
	"es": {
		ackTimeout:  "Se agotó el tiempo de respuesta del agente. Inténtalo de nuevo más tarde.",
		queueFull:   "La cola de mensajes del agente está llena. Inténtalo de nuevo más tarde.",
		unavailable: "El agente no está disponible temporalmente. Inténtalo de nuevo más tarde.",
	},
	"pt": {
		ackTimeout:  "O tempo de resposta do agente se esgotou. Tente novamente mais tarde.",
		queueFull:   "A fila de mensagens do agente está cheia. Tente novamente mais tarde.",
		unavailable: "O agente está temporariamente indisponível. Tente novamente mais tarde.",
	},
	"ru": {
		ackTimeout:  "Время ожидания ответа агента истекло. Повторите попытку позже.",
		queueFull:   "Очередь сообщений агента заполнена. Повторите попытку позже.",
		unavailable: "Агент временно недоступен. Повторите попытку позже.",
	},
	"ar": {
		ackTimeout:  "انتهت مهلة استجابة الوكيل. يُرجى المحاولة مرة أخرى لاحقًا.",
		queueFull:   "قائمة انتظار رسائل الوكيل ممتلئة. يُرجى المحاولة مرة أخرى لاحقًا.",
		unavailable: "الوكيل غير متاح مؤقتًا. يُرجى المحاولة مرة أخرى لاحقًا.",
	},
	"hi": {
		ackTimeout:  "एजेंट के जवाब का समय समाप्त हो गया। कृपया बाद में फिर से कोशिश करें।",
		queueFull:   "एजेंट की संदेश कतार भर गई है। कृपया बाद में फिर से कोशिश करें।",
		unavailable: "एजेंट अभी उपलब्ध नहीं है। कृपया बाद में फिर से कोशिश करें।",
	},
}

func agentDeliveryFailureCopyFor(language string) agentDeliveryFailureCopy {
	if copy, ok := agentDeliveryFailureCopyByLanguage[language]; ok {
		return copy
	}
	return agentDeliveryFailureCopyByLanguage[defaultAgentDeliveryFailureLanguage]
}
