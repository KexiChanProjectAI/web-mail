package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type rateLimitEntry struct {
	tokens     int
	lastUpdate time.Time
	mu         sync.Mutex
}

type rateLimiter struct {
	requestsPerMinute int
	window            time.Duration
	entries           sync.Map
	stopCleanup       chan struct{}
}

func newRateLimiter(requestsPerMinute int) *rateLimiter {
	rl := &rateLimiter{
		requestsPerMinute: requestsPerMinute,
		window:            time.Minute,
		stopCleanup:       make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *rateLimiter) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCleanup:
			return
		}
	}
}

func (rl *rateLimiter) cleanup() {
	now := time.Now()
	rl.entries.Range(func(key, value interface{}) bool {
		entry := value.(*rateLimitEntry)
		entry.mu.Lock()
		if now.Sub(entry.lastUpdate) > rl.window {
			rl.entries.Delete(key)
		}
		entry.mu.Unlock()
		return true
	})
}

func (rl *rateLimiter) Allow(ip string) (bool, int, int64) {
	now := time.Now()
	resetAt := now.Add(rl.window).Unix()

	loaded, _ := rl.entries.LoadOrStore(ip, &rateLimitEntry{
		tokens:     rl.requestsPerMinute,
		lastUpdate: now,
	})
	entry := loaded.(*rateLimitEntry)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	elapsed := now.Sub(entry.lastUpdate)
	entry.lastUpdate = now

	recovered := int(elapsed.Seconds() * float64(rl.requestsPerMinute) / rl.window.Seconds())
	if recovered > 0 {
		entry.tokens += recovered
		if entry.tokens > rl.requestsPerMinute {
			entry.tokens = rl.requestsPerMinute
		}
	}

	remaining := entry.tokens - 1
	if remaining < 0 {
		remaining = 0
	}

	if entry.tokens <= 0 {
		return false, 0, resetAt
	}
	entry.tokens--
	return true, remaining, resetAt
}

func (rl *rateLimiter) Stop() {
	close(rl.stopCleanup)
}

func getClientIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	return r.RemoteAddr
}

func RateLimit(requestsPerMinute int, burst int) func(http.Handler) http.Handler {
	if requestsPerMinute <= 0 {
		requestsPerMinute = 60
	}
	if burst <= 0 {
		burst = requestsPerMinute
	}
	logger := slog.Default()
	rl := newRateLimiter(requestsPerMinute)
	limitStr := fmt.Sprintf("%d", requestsPerMinute)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)
			allowed, remaining, resetAt := rl.Allow(ip)

			w.Header().Set("X-RateLimit-Limit", limitStr)
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt))

			if !allowed {
				logger.Warn("rate limit exceeded", "ip", ip, "path", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"rate limit exceeded"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
