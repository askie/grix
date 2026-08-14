package service

import (
	"errors"
	"net"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
)

func validateAgentAvatarURLInputProvided(avatarURL *string) *errcode.ErrCode {
	if avatarURL == nil {
		return nil
	}
	return &errAgentAvatarURLManagedOnly
}

func validateAgentAvatarURLValue(avatarURL string) *errcode.ErrCode {
	if strings.TrimSpace(avatarURL) == "" {
		return nil
	}
	return &errAgentAvatarURLManagedOnly
}

func validateLocalEndpoint(endpoint string) *errcode.ErrCode {
	if endpoint == "" {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return &errcode.ErrAgentEndpointBad
	}
	host := u.Hostname()
	if host == "" {
		return &errcode.ErrAgentEndpointBad
	}

	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return &errcode.ErrAgentEndpointBad
	}
	if !isPrivateIP(ip) {
		return &errcode.ErrAgentEndpointBad
	}
	return nil
}

func isValidProviderType(providerType int16) bool {
	return providerType == model.AgentProviderRemote ||
		providerType == model.AgentProviderLocal ||
		providerType == model.AgentProviderAPI ||
		providerType == model.AgentProviderVoice
}

// supportedVoiceProviders 是 voicebridge 端到端已实现的语音 provider 集合。
// 选用未实现的 provider 会被建/改 agent 接口直接拒绝（避免运行时通话静默失败）。
var supportedVoiceProviders = map[string]bool{
	"openai_realtime": true,
	"doubao_realtime": true,
}

// isSupportedVoiceProvider 判断 provider 是否已端到端支持。
func isSupportedVoiceProvider(p string) bool {
	return supportedVoiceProviders[strings.TrimSpace(p)]
}

// normalizeAndValidateVoiceCreate 裁剪并校验语音大模型(type=4)创建入参。
// provider/model/api_key 必填，无默认、无兜底；provider 必须在已支持集合内。
func normalizeAndValidateVoiceCreate(req *AgentCreateReq) *errcode.ErrCode {
	req.VoiceProvider = strings.TrimSpace(req.VoiceProvider)
	req.VoiceModel = strings.TrimSpace(req.VoiceModel)
	req.VoiceEndpoint = strings.TrimSpace(req.VoiceEndpoint)
	req.VoiceID = strings.TrimSpace(req.VoiceID)
	req.VoiceAPIKey = strings.TrimSpace(req.VoiceAPIKey)
	if req.VoiceProvider == "" || req.VoiceModel == "" || req.VoiceAPIKey == "" {
		return &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "语音大模型需填写 voice_provider / voice_model / voice_api_key",
		}
	}
	if !isSupportedVoiceProvider(req.VoiceProvider) {
		return &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "暂不支持的语音 provider（当前支持 openai_realtime / doubao_realtime）",
		}
	}
	if ec := validateVoiceEndpoint(req.VoiceEndpoint); ec != nil {
		return ec
	}
	return nil
}

// validateVoiceEndpoint 校验语音 BYOK 自定义 endpoint，防止 SSRF。
// 空值放行（使用 provider 官方默认地址）；非空则必须是 ws/wss，
// 且解析出的所有 IP 不得指向环回 / 私网 / 链路本地（含云元数据 169.254.169.254）
// / 未指定 / 组播地址。对域名会做一次 DNS 解析校验以降低 rebind 风险。
func validateVoiceEndpoint(endpoint string) *errcode.ErrCode {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil
	}
	bad := &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "voice_endpoint 非法或指向内网地址"}
	u, err := url.Parse(endpoint)
	if err != nil {
		return bad
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return bad
	}
	host := u.Hostname()
	if host == "" {
		return bad
	}
	var ips []net.IP
	if literal := net.ParseIP(host); literal != nil {
		ips = []net.IP{literal}
	} else {
		resolved, err := net.LookupIP(host)
		if err != nil || len(resolved) == 0 {
			return bad
		}
		ips = resolved
	}
	for _, ip := range ips {
		if isDisallowedSSRFIP(ip) {
			return bad
		}
	}
	return nil
}

// isDisallowedSSRFIP 判定 IP 是否落在禁止后端主动访问的保留范围内。
func isDisallowedSSRFIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}

func normalizeAgentClientTypeForProvider(providerType int16, raw string) (string, *errcode.ErrCode) {
	normalized := model.NormalizeAgentClientType(raw)
	if normalized == "" {
		return "", nil
	}
	if providerType != model.AgentProviderAPI {
		return "", &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "agent_client_type 仅支持 provider_type=3",
		}
	}
	if !model.IsValidAgentClientType(normalized) {
		return "", &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "agent_client_type 非法",
		}
	}
	return normalized, nil
}

func isPrivateIP(ip net.IP) bool {
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}
	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return true
		}
	}
	return strings.HasPrefix(ip.String(), "fd")
}

func normalizeAgentName(raw string) (string, *errcode.ErrCode) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "agent_name 不能为空",
		}
	}
	if utf8.RuneCountInString(name) > maxAgentNameRunes {
		return "", &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "agent_name 长度不能超过 100",
		}
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", &errcode.ErrCode{
				HTTPStatus: 400,
				BizCode:    10003,
				Msg:        "agent_name 包含非法控制字符",
			}
		}
	}
	return name, nil
}

func normalizeAgentIntroduction(raw string) (string, *errcode.ErrCode) {
	normalized, err := normalizeIntroductionText(raw, maxAgentIntroductionRunes)
	if err == nil {
		return normalized, nil
	}
	switch {
	case errors.Is(err, errIntroductionTooLong):
		return "", &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "introduction 长度不能超过 3072",
		}
	case errors.Is(err, errIntroductionInvalidControl):
		return "", &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "introduction 包含非法控制字符",
		}
	default:
		return "", &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "introduction 非法",
		}
	}
}
