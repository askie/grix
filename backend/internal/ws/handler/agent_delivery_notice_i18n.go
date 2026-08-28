package handler

type agentDeliveryFailureCopy struct {
	ackTimeout    string
	queueFull     string
	offlineQueued string
}

const defaultAgentDeliveryFailureLanguage = "zh"

var agentDeliveryFailureCopyByLanguage = map[string]agentDeliveryFailureCopy{
	"zh": {
		ackTimeout:    "智能体响应超时，请稍后重试。",
		queueFull:     "智能体消息队列已满，请稍后重试。",
		offlineQueued: "智能体当前不在线，消息已保存，请检查并启动该智能体的连接程序，它上线后会自动处理。",
	},
	"en": {
		ackTimeout:    "The agent response timed out. Please try again later.",
		queueFull:     "The agent's message queue is full. Please try again later.",
		offlineQueued: "The agent is not connected right now. Your message is saved and will be processed once you start its connector.",
	},
	"ja": {
		ackTimeout:    "エージェントの応答がタイムアウトしました。しばらくしてからもう一度お試しください。",
		queueFull:     "エージェントのメッセージキューがいっぱいです。しばらくしてからもう一度お試しください。",
		offlineQueued: "エージェントは現在接続されていません。メッセージは保存されており、コネクターを起動すると自動的に処理されます。",
	},
	"ko": {
		ackTimeout:    "에이전트 응답 시간이 초과되었습니다. 잠시 후 다시 시도해 주세요.",
		queueFull:     "에이전트의 메시지 대기열이 가득 찼습니다. 잠시 후 다시 시도해 주세요.",
		offlineQueued: "에이전트가 현재 연결되어 있지 않습니다. 메시지는 저장되었으며 커넥터를 실행하면 자동으로 처리됩니다.",
	},
	"de": {
		ackTimeout:    "Zeitüberschreitung bei der Agent-Antwort. Bitte versuchen Sie es später erneut.",
		queueFull:     "Die Nachrichtenwarteschlange des Agenten ist voll. Bitte versuchen Sie es später erneut.",
		offlineQueued: "Der Agent ist derzeit nicht verbunden. Ihre Nachricht wurde gespeichert und wird verarbeitet, sobald Sie den Connector starten.",
	},
	"fr": {
		ackTimeout:    "La réponse de l’agent a expiré. Veuillez réessayer plus tard.",
		queueFull:     "La file d’attente des messages de l’agent est pleine. Veuillez réessayer plus tard.",
		offlineQueued: "L’agent n’est pas connecté pour le moment. Votre message est enregistré et sera traité dès que vous démarrerez son connecteur.",
	},
	"es": {
		ackTimeout:    "Se agotó el tiempo de respuesta del agente. Inténtalo de nuevo más tarde.",
		queueFull:     "La cola de mensajes del agente está llena. Inténtalo de nuevo más tarde.",
		offlineQueued: "El agente no está conectado en este momento. Tu mensaje se guardó y se procesará en cuanto inicies su conector.",
	},
	"pt": {
		ackTimeout:    "O tempo de resposta do agente se esgotou. Tente novamente mais tarde.",
		queueFull:     "A fila de mensagens do agente está cheia. Tente novamente mais tarde.",
		offlineQueued: "O agente não está conectado no momento. Sua mensagem foi salva e será processada assim que você iniciar o conector.",
	},
	"ru": {
		ackTimeout:    "Время ожидания ответа агента истекло. Повторите попытку позже.",
		queueFull:     "Очередь сообщений агента заполнена. Повторите попытку позже.",
		offlineQueued: "Агент сейчас не подключён. Сообщение сохранено и будет обработано, как только вы запустите коннектор.",
	},
	"ar": {
		ackTimeout:    "انتهت مهلة استجابة الوكيل. يُرجى المحاولة مرة أخرى لاحقًا.",
		queueFull:     "قائمة انتظار رسائل الوكيل ممتلئة. يُرجى المحاولة مرة أخرى لاحقًا.",
		offlineQueued: "الوكيل غير متصل حاليًا. تم حفظ رسالتك وستتم معالجتها بمجرد تشغيل موصل الوكيل.",
	},
	"hi": {
		ackTimeout:    "एजेंट के जवाब का समय समाप्त हो गया। कृपया बाद में फिर से कोशिश करें।",
		queueFull:     "एजेंट की संदेश कतार भर गई है। कृपया बाद में फिर से कोशिश करें।",
		offlineQueued: "एजेंट अभी कनेक्ट नहीं है। आपका संदेश सहेज लिया गया है और कनेक्टर शुरू करते ही संसाधित हो जाएगा।",
	},
}

func agentDeliveryFailureCopyFor(language string) agentDeliveryFailureCopy {
	if copy, ok := agentDeliveryFailureCopyByLanguage[language]; ok {
		return copy
	}
	return agentDeliveryFailureCopyByLanguage[defaultAgentDeliveryFailureLanguage]
}
