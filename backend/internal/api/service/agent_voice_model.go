package service

import "github.com/askie/grix/backend/internal/systemsetting"

// VoicePresetResp 是 C 端可选的一条预定义音色。
type VoicePresetResp struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// VoiceModelOptionResp 是 C 端创建语音(type=4) agent 时可选的一条语音模型。
// 用户只按 Label 选择，Provider/Model/Endpoint 随选项一并带回提交，无需手填。
type VoiceModelOptionResp struct {
	ID       string            `json:"id"`
	Label    string            `json:"label"`
	Provider string            `json:"provider"`
	Model    string            `json:"model"`
	Endpoint string            `json:"endpoint"`
	Voices   []VoicePresetResp `json:"voices"`
}

// VoiceModelCatalogResp 是语音模型清单响应。
type VoiceModelCatalogResp struct {
	List []VoiceModelOptionResp `json:"list"`
}

// AgentVoiceModelCatalog 返回塘主已启用的语音模型清单（按 Sort 升序）。
func AgentVoiceModelCatalog() (VoiceModelCatalogResp, error) {
	options, err := systemsetting.EnabledVoiceModelOptions()
	if err != nil {
		return VoiceModelCatalogResp{}, err
	}
	list := make([]VoiceModelOptionResp, 0, len(options))
	for _, opt := range options {
		voices := make([]VoicePresetResp, 0, len(opt.Voices))
		for _, v := range opt.Voices {
			voices = append(voices, VoicePresetResp{ID: v.ID, Label: v.Label})
		}
		list = append(list, VoiceModelOptionResp{
			ID:       opt.ID,
			Label:    opt.Label,
			Provider: opt.Provider,
			Model:    opt.Model,
			Endpoint: opt.Endpoint,
			Voices:   voices,
		})
	}
	return VoiceModelCatalogResp{List: list}, nil
}
