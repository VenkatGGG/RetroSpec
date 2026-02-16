package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type apiRateLimiter struct {
	mu       sync.Mutex
	rps      rate.Limit
	burst    int
	ttl      time.Duration
	clients  map[string]*rate.Limiter
	lastSeen map[string]time.Time
}

func newAPIRateLimiter(requestsPerSec float64, burst int) *apiRateLimiter {
	if requestsPerSec <= 0 || burst <= 0 {
		return nil
	}

	return &apiRateLimiter{
		rps:      rate.Limit(requestsPerSec),
		burst:    burst,
		ttl:      10 * time.Minute,
		clients:  make(map[string]*rate.Limiter),
		lastSeen: make(map[string]time.Time),
	}
}

func (l *apiRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientAddress(r)) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (l *apiRateLimiter) allow(clientID string) bool {
	if clientID == "" {
		clientID = "unknown"
	}

	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	limiter, exists := l.clients[clientID]
	if !exists {
		limiter = rate.NewLimiter(l.rps, l.burst)
		l.clients[clientID] = limiter
	}
	l.lastSeen[clientID] = now

	for key, seenAt := range l.lastSeen {
		if now.Sub(seenAt) > l.ttl {
			delete(l.lastSeen, key)
			delete(l.clients, key)
		}
	}

	return limiter.Allow()
}

func clientAddress(r *http.Request) string {
	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	realIP := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if realIP != "" {
		return realIP
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}

	return strings.TrimSpace(r.RemoteAddr)
}
