// Package voicerefiner 实现通话转写文本的改写（Refiner）。
//
// 职责：接收原始转写文本（含口水词、无标点），
// 调用 LLM Gateway 做改写（去口水词 + 加标点 + 不增删语义），
// 返回改写后的文本。
package voicerefiner

import "context"

// TranscriptRefiner 改写转写文本的接口。
type TranscriptRefiner interface {
	// Refine 改写原始转写文本。
	// raw: 原始转写（如 "嗯 您好 那个 请问您是张先生吗"）
	// 返回改写后文本（如 "您好，请问您是张先生吗？"）
	Refine(ctx context.Context, raw string) (refined string, err error)
}
