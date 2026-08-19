package agentapi

import "testing"

// 守住 update_binding_card 路径（handleUpdateBindingCard / sendNewBindingCard）
// 下发的绑定卡文案：成功卡只保留一句「已绑定 <目录>」，过期卡保留中文错误提示。
func TestBindingCardSummary(t *testing.T) {
	tests := []struct {
		name         string
		cwd          string
		workerStatus string
		wantSummary  string
		wantStatus   string
	}{
		{
			name:         "ready with cwd",
			cwd:          "/workspace/demo",
			workerStatus: "ready",
			wantSummary:  "已绑定 /workspace/demo",
			wantStatus:   "success",
		},
		{
			name:         "ready without cwd",
			cwd:          "",
			workerStatus: "ready",
			wantSummary:  "目录绑定成功。",
			wantStatus:   "success",
		},
		{
			name:         "session expired",
			cwd:          "/workspace/demo",
			workerStatus: "session_expired",
			wantSummary:  "会话已过期，请新建会话后继续对话。",
			wantStatus:   "error",
		},
		{
			name:         "stopped with empty cwd means unbound",
			cwd:          "",
			workerStatus: "stopped",
			wantSummary:  "已解绑工作目录。",
			wantStatus:   "success",
		},
		{
			name:         "stopped with cwd stays bound (stop worker)",
			cwd:          "/workspace/demo",
			workerStatus: "stopped",
			wantSummary:  "已绑定 /workspace/demo",
			wantStatus:   "success",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			summary, status := bindingCardSummary("zh", tc.cwd, tc.workerStatus)
			if summary != tc.wantSummary {
				t.Fatalf("summary=%q want=%q", summary, tc.wantSummary)
			}
			if status != tc.wantStatus {
				t.Fatalf("status=%q want=%q", status, tc.wantStatus)
			}
		})
	}
}
