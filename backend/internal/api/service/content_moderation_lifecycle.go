package service

import (
	"context"
	"sync"
	"time"
)

// 内容审核 worker 的生命周期。
//
// 这些 worker 会一直读写全局的 DB / Redis / logger。原来它们用 sync.Once 起在
// context.Background() 上——起来就再也停不下来：进程活着它们就活着，服务优雅关停
// 时正在处理的审核任务会被硬生生打断，测试里则会活过整个用例，与下一个用例重置
// 全局变量抢跑（-race 必红）。
//
// 现在统一由 Start/Stop 管：Stop 取消 ctx 并等它们真正退出。
// 生产侧在 API 服务关停时调用，测试侧在收尾（关库之前）调用。
const contentModerationStopWait = 15 * time.Second

var contentModerationWorkers struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	// active 统计在跑的 worker 与一次性任务协程。
	// ⛔ 不能用 sync.WaitGroup：关停期间派发仍可能拉起新 worker，
	// WaitGroup 在 Wait 期间计数从 0 涨到 1 会 panic
	// （sync: WaitGroup is reused before previous Wait has returned）。
	active  int
	running bool
	// stopping 表示 Stop 正在等待 worker 退出。这期间不许再 Start——
	// 否则新拉起的 worker 会让计数永远不归零，Stop 就一直等到上限（活锁）。
	stopping bool
}

// contentModerationWorkerBegin 登记一个在跑的协程。
// 调用方必须已持有 contentModerationWorkers.mu——登记与「读 ctx / 判 running」
// 必须在同一个临界区里完成，否则 Stop 可能在中间插进来，协程就漏在等待之外了。
func contentModerationWorkerBeginLocked() {
	contentModerationWorkers.active++
}

// contentModerationWorkerEnd 注销一个已退出的协程；自行加锁。
func contentModerationWorkerEnd() {
	contentModerationWorkers.mu.Lock()
	contentModerationWorkers.active--
	contentModerationWorkers.mu.Unlock()
}

// StartContentModerationWorkers 启动内容审核 worker（幂等）。
// 已在运行时直接返回，不会重复起一批。
func StartContentModerationWorkers(parent context.Context) {
	if parent == nil {
		parent = context.Background()
	}

	contentModerationWorkers.mu.Lock()
	defer contentModerationWorkers.mu.Unlock()
	if contentModerationWorkers.running || contentModerationWorkers.stopping {
		return
	}

	ctx, cancel := context.WithCancel(parent)
	contentModerationWorkers.ctx = ctx
	contentModerationWorkers.cancel = cancel
	contentModerationWorkers.running = true

	for worker := 0; worker < contentModerationWorkerCount; worker++ {
		contentModerationWorkerBeginLocked()
		go func() {
			defer contentModerationWorkerEnd()
			runContentModerationWorker(ctx)
		}()
	}
	contentModerationWorkerBeginLocked()
	go func() {
		defer contentModerationWorkerEnd()
		runContentModerationRecoveryLoop(ctx)
	}()
}

// StopContentModerationWorkers 停止 worker 并等它们退出（幂等）。
// 超过 contentModerationStopWait 仍未退出就不再干等，只打日志——
// 关停不能被一个卡住的任务拖死。
func StopContentModerationWorkers() {
	contentModerationWorkers.mu.Lock()
	if !contentModerationWorkers.running {
		contentModerationWorkers.mu.Unlock()
		return
	}
	cancel := contentModerationWorkers.cancel
	contentModerationWorkers.running = false
	contentModerationWorkers.stopping = true
	contentModerationWorkers.cancel = nil
	contentModerationWorkers.ctx = nil
	contentModerationWorkers.mu.Unlock()

	// 等待结束后放开 stopping，允许后续重新 Start。
	defer func() {
		contentModerationWorkers.mu.Lock()
		contentModerationWorkers.stopping = false
		contentModerationWorkers.mu.Unlock()
	}()

	if cancel != nil {
		cancel()
	}

	deadline := time.Now().Add(contentModerationStopWait)
	for {
		contentModerationWorkers.mu.Lock()
		active := contentModerationWorkers.active
		contentModerationWorkers.mu.Unlock()
		if active == 0 {
			return
		}
		if time.Now().After(deadline) {
			logContentModerationWarnf(
				"content moderation workers did not stop within %s, %d still running",
				contentModerationStopWait, active,
			)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// spawnContentModerationTask 起一个受生命周期约束的一次性任务协程。
// 关停中不再新起：此时 Stop 已经在等待，新起的协程会漏在等待之外。
func spawnContentModerationTask(fn func(ctx context.Context)) {
	if fn == nil {
		return
	}

	contentModerationWorkers.mu.Lock()
	if !contentModerationWorkers.running {
		contentModerationWorkers.mu.Unlock()
		return
	}
	ctx := contentModerationWorkers.ctx
	contentModerationWorkerBeginLocked()
	contentModerationWorkers.mu.Unlock()

	go func() {
		defer contentModerationWorkerEnd()
		fn(ctx)
	}()
}
