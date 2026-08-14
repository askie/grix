package ws

import "testing"

func TestNoteAgentAPIFenceDrop_CountsPerStream(t *testing.T) {
	s := &Server{}
	if n := s.noteAgentAPIFenceDrop(1, "cm-a"); n != 1 {
		t.Fatalf("first drop should be 1, got %d", n)
	}
	if n := s.noteAgentAPIFenceDrop(1, "cm-a"); n != 2 {
		t.Fatalf("second drop should be 2, got %d", n)
	}
	// 不同 client_msg_id 独立计数
	if n := s.noteAgentAPIFenceDrop(1, "cm-b"); n != 1 {
		t.Fatalf("other stream first drop should be 1, got %d", n)
	}
	// 不同 agent 独立计数
	if n := s.noteAgentAPIFenceDrop(2, "cm-a"); n != 1 {
		t.Fatalf("other agent first drop should be 1, got %d", n)
	}
}

func TestNoteAgentAPIFenceDrop_MapResetKeepsCounting(t *testing.T) {
	s := &Server{agentAPIStreamFenceDropCounts: make(map[string]int64)}
	for i := 0; i < 4096; i++ {
		s.agentAPIStreamFenceDropCounts[string(rune(i))] = 1
	}
	// 达到上限后整体重置，从 1 重新计数（只影响采样相位）
	if n := s.noteAgentAPIFenceDrop(1, "cm-x"); n != 1 {
		t.Fatalf("after reset drop should restart at 1, got %d", n)
	}
	if len(s.agentAPIStreamFenceDropCounts) != 1 {
		t.Fatalf("map should contain only the fresh entry, got %d", len(s.agentAPIStreamFenceDropCounts))
	}
}

func TestShouldLogAgentAPIFenceDrop_Samples(t *testing.T) {
	// 第 1 次必打
	if !shouldLogAgentAPIFenceDrop(1) {
		t.Fatal("first drop should be logged")
	}
	// 非采样点静默
	for _, n := range []int64{2, 3, 50, 99, 101, 199} {
		if shouldLogAgentAPIFenceDrop(n) {
			t.Fatalf("drop %d should be silent", n)
		}
	}
	// 每第 100 次打一条
	for _, n := range []int64{100, 200, 300} {
		if !shouldLogAgentAPIFenceDrop(n) {
			t.Fatalf("drop %d should be logged", n)
		}
	}
}
