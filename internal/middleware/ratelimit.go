package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateBucket struct {
	count   int
	resetAt time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	window  time.Duration
	now     func() time.Time
}

func NewRateLimiter(window time.Duration) *RateLimiter {
	if window <= 0 {
		window = time.Minute
	}
	return &RateLimiter{
		buckets: make(map[string]*rateBucket),
		window:  window,
		now:     time.Now,
	}
}

func (l *RateLimiter) Allow(key string, limit int) bool {
	if l == nil || limit <= 0 {
		return true
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	bucket := l.buckets[key]
	if bucket == nil || !now.Before(bucket.resetAt) {
		l.buckets[key] = &rateBucket{count: 1, resetAt: now.Add(l.window)}
		l.cleanupLocked(now)
		return true
	}
	if bucket.count >= limit {
		return false
	}
	bucket.count++
	return true
}

func (l *RateLimiter) cleanupLocked(now time.Time) {
	if len(l.buckets) < 4096 {
		return
	}
	for key, bucket := range l.buckets {
		if !now.Before(bucket.resetAt) {
			delete(l.buckets, key)
		}
	}
}

func RateLimit(limiter *RateLimiter, scope string, limit int) gin.HandlerFunc {
	return RateLimitFunc(limiter, scope, func() int { return limit })
}

func RateLimitFunc(limiter *RateLimiter, scope string, limit func() int) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := scope + ":" + rateLimitKey(c)
		if !limiter.Allow(key, limit()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":      http.StatusTooManyRequests,
				"message":   "too many requests",
				"requestId": GetRequestID(c),
			})
			return
		}
		c.Next()
	}
}

func rateLimitKey(c *gin.Context) string {
	if participantID := ParticipantID(c); participantID != 0 {
		return "participant:" + strconv.FormatUint(participantID, 10)
	}
	if userID := UserID(c); userID != 0 {
		return "user:" + strconv.FormatUint(userID, 10)
	}
	return "ip:" + c.ClientIP()
}
