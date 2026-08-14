package service

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Stop 必须真的把 worker 停干净——计数归零才算数。
func TestStopContentModerationWorkersDrainsAll(t *testing.T) {
	StartContentModerationWorkers(context.Background())
	StopContentModerationWorkers()

	contentModerationWorkers.mu.Lock()
	active := contentModerationWorkers.active
	running := contentModerationWorkers.running
	contentModerationWorkers.mu.Unlock()

	if active != 0 {
		t.Fatalf("Stop returned while %d goroutines were still running", active)
	}
	if running {
		t.Fatal("running flag must be cleared after Stop")
	}
}

// Stop 之后必须能重新 Start——原来用 sync.Once，停了就再也起不来。
func TestContentModerationWorkersRestartableAfterStop(t *testing.T) {
	StartContentModerationWorkers(context.Background())
	StopContentModerationWorkers()
	StartContentModerationWorkers(context.Background())
	defer StopContentModerationWorkers()

	contentModerationWorkers.mu.Lock()
	running := contentModerationWorkers.running
	contentModerationWorkers.mu.Unlock()
	if !running {
		t.Fatal("workers must be restartable after Stop")
	}
}

// Stop 正在排空时，Start 必须是空操作。
//
// 否则新拉起的 worker 挂在一个「没有被这次 Stop 取消」的 ctx 上，永远不退出，
// Stop 的计数就永远不归零——一路等到上限才返回（实测从 2 秒退化成 90 秒卡死），
// 而且返回时协程还活着，等于没停干净。
func TestStartIsRejectedWhileStopIsDraining(t *testing.T) {
	contentModerationWorkers.mu.Lock()
	contentModerationWorkers.stopping = true
	contentModerationWorkers.mu.Unlock()

	StartContentModerationWorkers(context.Background())

	contentModerationWorkers.mu.Lock()
	running := contentModerationWorkers.running
	active := contentModerationWorkers.active
	contentModerationWorkers.stopping = false
	contentModerationWorkers.mu.Unlock()

	if running || active != 0 {
		t.Fatalf("Start must be a no-op while Stop is draining (running=%v active=%d)", running, active)
	}
}

// 并发 Start/Stop 不得死锁、活锁或 panic。
//
// 两个坑都在这条用例下踩到过：
//   - 用 sync.WaitGroup 时，Stop 正在 Wait、Start 又 Add，计数从 0 涨到 1 直接 panic
//     （sync: WaitGroup is reused before previous Wait has returned）；
//   - Stop 等待期间若允许 Start，新拉起的 worker 会让计数永远不归零，Stop 一直等到
//     上限才返回（活锁，实测从 2 秒退化成 90 秒卡死）。
func TestContentModerationWorkersConcurrentStartStop(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for round := 0; round < 20; round++ {
			var wg sync.WaitGroup
			for i := 0; i < 6; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					StartContentModerationWorkers(context.Background())
				}()
			}
			for i := 0; i < 3; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					StopContentModerationWorkers()
				}()
			}
			wg.Wait()
			StopContentModerationWorkers()

			contentModerationWorkers.mu.Lock()
			active := contentModerationWorkers.active
			contentModerationWorkers.mu.Unlock()
			if active != 0 {
				t.Errorf("round %d: Stop returned with %d goroutines still running", round, active)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("concurrent Start/Stop deadlocked or livelocked")
	}
}

// ⛔ 合规底线：任务一旦从队列弹出，就必须处理完，不许被关停掐断。
//
// worker 的取任务循环用可取消的 ctx（关停时立刻醒），但交给处理函数的 ctx 必须
// 不可取消。否则关停一取消，processContentModerationTask 里第一个 DB 操作就失败
// 返回——任务从 Redis 没了、库里也没记录，恢复扫描找不着它，这条违规消息就永远
// 不会被撤回。
//
// 真正驱动 runContentModerationWorker 跑一遍：塞一条任务进 Redis 队列，起 worker，
// 用打桩的 contentModerationProcessTask 捕获 worker 实际传下去的 ctx，
// 再取消 worker 自己的 ctx，断言捕获到的 ctx 没有被一起取消。
//
// 上一版这里是"复刻"了一遍 WithoutCancel 的用法、测的是 Go 标准库语义，不是
// 生产代码——审查实测过：把 content_moderation_service.go 里的调用改回可取消的
// ctx，那版测试照样全绿。这版改用包级可替换变量 contentModerationProcessTask
// 打桩，直接跑真实的 runContentModerationWorker，才会在回归时真的变红。
func TestWorkerHandsUncancellableCtxToProcessing(t *testing.T) {
	cleanup := setupContentModerationServiceTest(t)
	defer cleanup()

	task := ContentModerationTask{SessionID: "sess-withoutcancel", MsgID: 991001}
	if err := enqueueContentModerationTask(context.Background(), task); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	captured := make(chan context.Context, 1)
	original := contentModerationProcessTask
	contentModerationProcessTask = func(ctx context.Context, got ContentModerationTask) {
		captured <- ctx
	}
	defer func() { contentModerationProcessTask = original }()

	workerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runContentModerationWorker(workerCtx)
	}()

	var processCtx context.Context
	select {
	case processCtx = <-captured:
	case <-time.After(5 * time.Second):
		t.Fatal("worker never popped/processed the task")
	}

	// 此刻任务已经从 Redis 弹出、交给了处理函数。现在关停：worker 的 ctx 被取消。
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not exit after its ctx was cancelled")
	}

	if err := processCtx.Err(); err != nil {
		t.Fatalf("processing ctx died with the worker ctx (err=%v) — a task already popped off "+
			"the queue would be dropped, leaving a violating message live", err)
	}
	select {
	case <-processCtx.Done():
		t.Fatal("processing ctx must not be cancelled by shutdown — the popped task must finish")
	default:
	}
}
