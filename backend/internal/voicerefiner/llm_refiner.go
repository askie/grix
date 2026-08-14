package voicerefiner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/llmclient"
)

const refineSystemPrompt = `你是一个语音转写文本改写助手。
规则：
1. 去除口水词（嗯、啊、那个、就是等）
2. 添加标点符号
3. 不增加、不删除、不改变任何实质语义
4. 保持原始语言（中文保持中文，英文保持英文）
5. 直接输出改写结果，不要任何解释`

// LLMRefiner 使用 LLM Gateway 改写转写文本。
type LLMRefiner struct {
	client *llmclient.Client
	model  string
}

// NewLLMRefiner 创建 LLMRefiner。
func NewLLMRefiner(client *llmclient.Client, model string) *LLMRefiner {
	return &LLMRefiner{client: client, model: model}
}

func (r *LLMRefiner) Refine(ctx context.Context, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	// 限制 LLM 调用最长 5s，防止 goroutine 泄漏
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req := llmclient.JSONRequest{
		Model:        r.model,
		Instructions: refineSystemPrompt,
		Input:        raw,
	}

	resp, err := r.client.GenerateJSON(ctx, req, nil)
	if err != nil {
		// 降级：LLM 失败时返回原始文本，不阻断消息写入
		return raw, fmt.Errorf("refine llm: %w", err)
	}
	refined := strings.TrimSpace(resp.OutputText)
	if refined == "" {
		return raw, nil // 降级
	}
	return refined, nil
}

// NoopRefiner 直接返回原始文本（测试/降级用）。
type NoopRefiner struct{}

func (n *NoopRefiner) Refine(_ context.Context, raw string) (string, error) {
	return raw, nil
}
