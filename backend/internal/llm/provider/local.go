package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
)

// LocalProvider implements the Provider interface for local LLM servers (Ollama-compatible).
type LocalProvider struct {
	Endpoint  string // e.g. "http://localhost:11434"
	ModelName string // e.g. "llama3"
}

func NewLocalProvider(endpoint, modelName string) *LocalProvider {
	return &LocalProvider{Endpoint: endpoint, ModelName: modelName}
}

func (p *LocalProvider) Name() string { return "local" }

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done            bool `json:"done"`
	PromptEvalCount int  `json:"prompt_eval_count"`
	EvalCount       int  `json:"eval_count"`
}

// localDisallowedIP 判定拨号目标 IP 是否落在禁止访问的保留范围
// （loopback/私网/链路本地/未指定/组播/CGNAT 100.64.0.0/10）。
// 与 agent_service_validation 的 isDisallowedSSRFIP 同口径，但作用于「实际连接的 IP」，
// 保存时的域名校验存在 DNS rebinding TOCTOU，必须在拨号后按连接对端再拦一次。
func localDisallowedIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() ||
		netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
}

// localHTTPTransport 在拨号后校验连接对端 IP（开关关闭即 SaaS 默认时），
// 攻击者即使让域名在保存时解析到公网、请求时 rebind 到 169.254.169.254/127.0.0.1，
// 也会在这里被拒。ResponseHeaderTimeout 只约束等首字节的时长，流式 body 的
// 生命周期交给调用方 ctx——不能用 http.Client.Timeout，它会覆盖整个流式读取，
// 把本地模型的长生成从中掐断。
var localHTTPTransport = &http.Transport{
	ResponseHeaderTimeout: 30 * time.Second,
	DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		d := &net.Dialer{Timeout: 10 * time.Second}
		conn, err := d.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		if !config.C.Security.AllowPrivateLocalEndpoint {
			if ta, ok := conn.RemoteAddr().(*net.TCPAddr); ok && localDisallowedIP(ta.AddrPort().Addr()) {
				conn.Close()
				return nil, fmt.Errorf("local LLM endpoint: dial to disallowed address %s", ta.AddrPort().Addr())
			}
		}
		return conn, nil
	},
}

// localHTTPClient 是 local provider 的专用 HTTP client。
// 重定向整体禁用（ErrUseLastResponse）：local LLM API 无合法跳转场景，
// 放行 302 会让攻击者把请求引到未校验的目标，绕过拨号 IP 校验。
var localHTTPClient = &http.Client{
	Transport: localHTTPTransport,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func (p *LocalProvider) StreamChat(ctx context.Context, req *Request, callback StreamCallback) error {
	modelName := req.Model
	if modelName == "" {
		modelName = p.ModelName
	}

	ollamaMsgs := make([]ollamaMessage, len(req.Messages))
	for i, m := range req.Messages {
		ollamaMsgs[i] = ollamaMessage{Role: m.Role, Content: m.Content}
	}

	body := ollamaChatRequest{
		Model:    modelName,
		Messages: ollamaMsgs,
		Stream:   req.Stream,
	}

	data, _ := json.Marshal(body)

	endpoint := strings.TrimRight(p.Endpoint, "/")
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := localHTTPClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("local LLM API error %d: %s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var chunk ollamaChatResponse
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}

		sc := StreamChunk{
			DeltaContent: chunk.Message.Content,
		}

		if chunk.Done {
			sc.IsFinish = true
			sc.PromptTokens = chunk.PromptEvalCount
			sc.CompletionTokens = chunk.EvalCount
		}

		callback(sc)

		if chunk.Done {
			return nil
		}
	}
	return scanner.Err()
}
