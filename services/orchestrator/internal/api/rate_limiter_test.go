package api

import (
	"net/http/httptest"
	"testing"
)

func TestClientAddressPrefersForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/issues", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	req.Header.Set("X-Real-IP", "198.51.100.4")
	req.RemoteAddr = "127.0.0.1:1234"

	if got := clientAddress(req); got != "203.0.113.9" {
		t.Fatalf("expected forwarded IP, got %q", got)
	}
}

func TestRateLimiterBlocksExcessBurst(t *testing.T) {
	limiter := newAPIRateLimiter(1, 1)
	if limiter == nil {
		t.Fatal("expected limiter to be created")
	}

	if !limiter.allow("192.0.2.10") {
		t.Fatal("first request should be allowed")
	}
	if limiter.allow("192.0.2.10") {
		t.Fatal("second immediate request should be rate limited")
	}
}
