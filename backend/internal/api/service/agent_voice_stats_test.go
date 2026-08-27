package service

import "testing"

func TestClampVoiceMaxConcurrentCalls(t *testing.T) {
	cases := map[int]int{-1: 2, 0: 2, 1: 1, 5: 5, 10: 10, 11: 10, 100: 10}
	for in, want := range cases {
		if got := clampVoiceMaxConcurrentCalls(in); got != want {
			t.Errorf("clamp(%d)=%d want %d", in, got, want)
		}
	}
}
