package service

import "testing"

func TestMaxAgentsPerUser(t *testing.T) {
	if maxAgentsPerUser != 100 {
		t.Fatalf("maxAgentsPerUser = %d, want 100", maxAgentsPerUser)
	}
}
