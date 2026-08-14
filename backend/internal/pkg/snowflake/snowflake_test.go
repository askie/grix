package snowflake

import (
	"sync"
	"testing"
)

func TestMain(m *testing.M) {
	// Initialize with machine ID 1 for testing
	if err := Init(1); err != nil {
		panic("failed to init snowflake: " + err.Error())
	}
	m.Run()
}

func TestGenID(t *testing.T) {
	id := GenID()

	if id == 0 {
		t.Error("GenID() returned 0, expected non-zero ID")
	}

	if id < 0 {
		t.Error("GenID() returned negative ID")
	}
}

func TestGenIDUniqueness(t *testing.T) {
	const count = 10000
	ids := make(map[int64]bool, count)

	for i := 0; i < count; i++ {
		id := GenID()
		if ids[id] {
			t.Errorf("duplicate ID generated: %d", id)
		}
		ids[id] = true
	}
}

func TestGenIDConcurrent(t *testing.T) {
	const goroutines = 10
	const idsPerGoroutine = 1000

	var wg sync.WaitGroup
	idChan := make(chan int64, goroutines*idsPerGoroutine)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < idsPerGoroutine; j++ {
				idChan <- GenID()
			}
		}()
	}

	wg.Wait()
	close(idChan)

	ids := make(map[int64]bool)
	for id := range idChan {
		if ids[id] {
			t.Errorf("duplicate ID in concurrent test: %d", id)
		}
		ids[id] = true
	}

	expectedCount := goroutines * idsPerGoroutine
	if len(ids) != expectedCount {
		t.Errorf("expected %d unique IDs, got %d", expectedCount, len(ids))
	}
}

func TestGenIDMonotonic(t *testing.T) {
	// IDs should generally be increasing (with some tolerance for clock skew)
	const count = 100
	prevID := int64(0)

	for i := 0; i < count; i++ {
		id := GenID()
		if id <= prevID {
			t.Errorf("ID not monotonically increasing: prev=%d, current=%d", prevID, id)
		}
		prevID = id
	}
}

func TestMultipleInits(t *testing.T) {
	// Re-initializing should work
	err := Init(2)
	if err != nil {
		t.Errorf("re-init failed: %v", err)
	}

	id := GenID()
	if id == 0 {
		t.Error("GenID() returned 0 after re-init")
	}

	// Restore original
	_ = Init(1)
}

func BenchmarkGenID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenID()
	}
}

func BenchmarkGenIDParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			GenID()
		}
	})
}
