package resilience_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"ms/shared/resilience"
)

func TestMemoryLocker(t *testing.T) {
	locker := resilience.NewMemoryLocker()
	ctx := context.Background()

	unlock1, err := locker.TryLock(ctx, "drv_101", 1*time.Second)
	if err != nil {
		t.Fatalf("first lock should succeed: %v", err)
	}

	// Immediate second lock should fail
	_, err = locker.TryLock(ctx, "drv_101", 1*time.Second)
	if err != resilience.ErrLockBusy {
		t.Errorf("expected ErrLockBusy, got: %v", err)
	}

	// Release lock
	unlock1()

	// Third lock should now succeed
	unlock2, err := locker.TryLock(ctx, "drv_101", 1*time.Second)
	if err != nil {
		t.Fatalf("lock after unlock should succeed: %v", err)
	}
	unlock2()
}

func TestMemoryLocker_Concurrency(t *testing.T) {
	locker := resilience.NewMemoryLocker()
	ctx := context.Background()

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// 10 concurrent routines compete for the same driver lock
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock, err := locker.TryLock(ctx, "concurrent_key", 500*time.Millisecond)
			if err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				unlock()
			}
		}()
	}
	wg.Wait()

	if successCount == 0 {
		t.Errorf("at least one worker must acquire the lock")
	}
}

func TestCircuitBreaker(t *testing.T) {
	cb := resilience.NewCircuitBreaker("test-service", 2, 50*time.Millisecond)

	customErr := errors.New("remote error")

	// 1st failure: still closed
	_ = cb.Execute(func() error { return customErr })
	if cb.State() != resilience.StateClosed {
		t.Errorf("expected closed state, got: %v", cb.State())
	}

	// 2nd failure: trips to OPEN
	_ = cb.Execute(func() error { return customErr })
	if cb.State() != resilience.StateOpen {
		t.Errorf("expected open state, got: %v", cb.State())
	}

	// 3rd call: fails fast with ErrCircuitOpen
	err := cb.Execute(func() error { return nil })
	if err != resilience.ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got: %v", err)
	}

	// Wait for reset timeout
	time.Sleep(60 * time.Millisecond)

	// Next call: state is HALF-OPEN, successful probe resets to CLOSED
	err = cb.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("expected recovery call to succeed: %v", err)
	}
	if cb.State() != resilience.StateClosed {
		t.Errorf("expected state closed after recovery, got: %v", cb.State())
	}
}

func TestRateLimiter(t *testing.T) {
	// 5 tokens/sec, capacity of 2
	rl := resilience.NewRateLimiter(5.0, 2)

	// First 2 requests succeed (burst capacity)
	if !rl.Allow("127.0.0.1") {
		t.Errorf("1st request should be allowed")
	}
	if !rl.Allow("127.0.0.1") {
		t.Errorf("2nd request should be allowed")
	}

	// 3rd request should be blocked
	if rl.Allow("127.0.0.1") {
		t.Errorf("3rd immediate request should be blocked")
	}

	// Different IP should be allowed
	if !rl.Allow("192.168.1.1") {
		t.Errorf("different client should have separate quota")
	}

	// Middleware test
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"

	// 1st through middleware: 200 OK
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got: %d", rec.Code)
	}
}
