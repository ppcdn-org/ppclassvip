package middleware

import (
	"testing"
	"time"
)

func TestRateLimiterFixedWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	limiter := NewRateLimiter(time.Minute)
	limiter.now = func() time.Time { return now }

	for i := 0; i < 2; i++ {
		if !limiter.Allow("k", 2) {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if limiter.Allow("k", 2) {
		t.Fatal("third request should be rejected")
	}

	now = now.Add(time.Minute)
	if !limiter.Allow("k", 2) {
		t.Fatal("request after the window should be allowed")
	}
}

func TestRateLimiterIndependentKeys(t *testing.T) {
	limiter := NewRateLimiter(time.Minute)
	if !limiter.Allow("a", 1) || !limiter.Allow("b", 1) {
		t.Fatal("different keys should not share a bucket")
	}
	if limiter.Allow("a", 1) {
		t.Fatal("key a should be limited")
	}
}
