// Package resilience provides concurrency control, distributed locks, circuit breakers, and rate limiters.
package resilience

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ─── 1. Atomic Driver Claim Locker (Distributed Lock) ─────────────────────────

var ErrLockBusy = errors.New("lock already acquired by another worker")

// Locker provides atomic, time-bound mutual exclusion for resource keys (e.g. driver IDs).
type Locker interface {
	TryLock(ctx context.Context, key string, ttl time.Duration) (unlock func(), err error)
}

// MemoryLocker is a thread-safe, in-memory TTL lock engine.
type MemoryLocker struct {
	mu    sync.Mutex
	locks map[string]time.Time
}

// NewMemoryLocker creates an in-memory Locker.
func NewMemoryLocker() *MemoryLocker {
	return &MemoryLocker{
		locks: make(map[string]time.Time),
	}
}

// TryLock attempts to acquire a lock on key for ttl duration.
// Returns ErrLockBusy if the key is already locked by another operation.
func (m *MemoryLocker) TryLock(ctx context.Context, key string, ttl time.Duration) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	if exp, exists := m.locks[key]; exists && exp.After(now) {
		return nil, ErrLockBusy
	}

	m.locks[key] = now.Add(ttl)
	released := false

	unlock := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if !released {
			released = true
			delete(m.locks, key)
		}
	}

	return unlock, nil
}

// ─── 2. Circuit Breaker ─────────────────────────────────────────────────────

type State int

const (
	StateClosed State = iota
	StateHalfOpen
	StateOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateHalfOpen:
		return "HALF_OPEN"
	case StateOpen:
		return "OPEN"
	default:
		return "UNKNOWN"
	}
}

var ErrCircuitOpen = errors.New("circuit breaker is OPEN: downstream service temporarily unreachable")

// CircuitBreaker guards remote gRPC and HTTP operations against cascading failures.
type CircuitBreaker struct {
	mu           sync.Mutex
	name         string
	maxFailures  int
	resetTimeout time.Duration

	state        State
	failures     int
	lastFailure  time.Time
}

// NewCircuitBreaker creates a circuit breaker that trips after maxFailures consecutive errors
// and probes recovery after resetTimeout.
func NewCircuitBreaker(name string, maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	if maxFailures <= 0 {
		maxFailures = 5
	}
	if resetTimeout <= 0 {
		resetTimeout = 10 * time.Second
	}
	return &CircuitBreaker{
		name:         name,
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		state:        StateClosed,
	}
}

// Execute runs fn if the circuit is healthy. If the circuit is open, it fails fast.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()
	now := time.Now()

	switch cb.state {
	case StateOpen:
		if now.Sub(cb.lastFailure) > cb.resetTimeout {
			cb.state = StateHalfOpen
		} else {
			cb.mu.Unlock()
			return ErrCircuitOpen
		}
	}
	cb.mu.Unlock()

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failures++
		cb.lastFailure = time.Now()
		if cb.failures >= cb.maxFailures || cb.state == StateHalfOpen {
			cb.state = StateOpen
		}
		return err
	}

	// Success
	if cb.state == StateHalfOpen || cb.failures > 0 {
		cb.state = StateClosed
		cb.failures = 0
	}
	return nil
}

// State returns current circuit state.
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// ─── 3. Token Bucket Rate Limiter ───────────────────────────────────────────

type clientBucket struct {
	tokens     float64
	lastRefill time.Time
}

// RateLimiter implements a per-client token-bucket rate limiter.
type RateLimiter struct {
	mu       sync.Mutex
	rate     float64 // tokens per second
	capacity float64 // max burst capacity
	clients  map[string]*clientBucket
}

// NewRateLimiter creates a RateLimiter with tokensPerSec and maxBurst.
func NewRateLimiter(tokensPerSec float64, maxBurst int) *RateLimiter {
	return &RateLimiter{
		rate:     tokensPerSec,
		capacity: float64(maxBurst),
		clients:  make(map[string]*clientBucket),
	}
}

// Allow reports whether a request from clientKey is allowed under the rate limit.
func (rl *RateLimiter) Allow(clientKey string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.clients[clientKey]
	if !exists {
		rl.clients[clientKey] = &clientBucket{
			tokens:     rl.capacity - 1.0,
			lastRefill: now,
		}
		return true
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > rl.capacity {
		b.tokens = rl.capacity
	}
	b.lastRefill = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

// Middleware creates an HTTP handler middleware enforcing rate limits per client IP.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract client IP
		ip := r.RemoteAddr
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ip = strings.TrimSpace(strings.Split(xff, ",")[0])
		}
		if colon := strings.LastIndex(ip, ":"); colon != -1 {
			ip = ip[:colon]
		}

		if !rl.Allow(ip) {
			w.Header().Set("Retry-After", "2")
			http.Error(w, "429 Too Many Requests: rate limit exceeded, please retry shortly.", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
