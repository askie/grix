package model

const (
	AgentProviderRemote int16 = 1
	AgentProviderLocal  int16 = 2
	AgentProviderAPI    int16 = 3
	AgentProviderVoice  int16 = 4 // 语音大模型（BYOK，走 voiceadapter + Voice Bridge）
)
