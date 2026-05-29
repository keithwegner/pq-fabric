package validator

import (
	"sync"
	"time"
)

type rateLimiter struct {
	mu        sync.Mutex
	perMinute float64
	burst     float64
	now       func() time.Time
	buckets   map[string]*rateBucket
}

type rateBucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(perMinute, burst int) *rateLimiter {
	if perMinute <= 0 {
		perMinute = 60
	}
	if burst <= 0 {
		burst = 20
	}
	return &rateLimiter{
		perMinute: float64(perMinute),
		burst:     float64(burst),
		now:       time.Now,
		buckets:   make(map[string]*rateBucket),
	}
}

func (l *rateLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	bucket, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &rateBucket{tokens: l.burst - 1, last: now}
		return true
	}
	elapsed := now.Sub(bucket.last).Minutes()
	if elapsed > 0 {
		bucket.tokens += elapsed * l.perMinute
		if bucket.tokens > l.burst {
			bucket.tokens = l.burst
		}
		bucket.last = now
	}
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}
