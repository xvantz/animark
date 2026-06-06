// Package rate provides a per-key token bucket rate limiter.
package rate

import (
	"sync"
	"time"
)

// Bucket is a token bucket for a single key.
type Bucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

// Limiter manages multiple token buckets keyed by name (e.g. provider name).
type Limiter struct {
	mu         sync.Mutex
	buckets    map[string]*Bucket
	maxTokens  float64
	refillRate float64
}

// New creates a rate limiter.
//   maxTokens: maximum burst size (e.g. 30 requests)
//   refillDuration: how often one token is added (e.g. 1s = 1 request per second)
func New(maxTokens int, refillDuration time.Duration) *Limiter {
	return &Limiter{
		buckets:    make(map[string]*Bucket),
		maxTokens:  float64(maxTokens),
		refillRate: 1.0 / refillDuration.Seconds(),
	}
}

// Allow checks if a request for the given key is allowed.
// Consumes one token if available, returns true.
func (l *Limiter) Allow(key string) bool {
	return l.AllowN(key, 1)
}

// AllowN checks if N requests for the given key are allowed.
// Consumes N tokens if available, returns true.
func (l *Limiter) AllowN(key string, n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		b = &Bucket{
			tokens:     l.maxTokens,
			maxTokens:  l.maxTokens,
			refillRate: l.refillRate,
			lastRefill: time.Now(),
		}
		l.buckets[key] = b
	}

	b.refill()
	if b.tokens >= float64(n) {
		b.tokens -= float64(n)
		return true
	}
	return false
}

// Remaining returns the available tokens for a key without consuming any.
func (l *Limiter) Remaining(key string) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		return l.maxTokens
	}
	b.refill()
	return b.tokens
}

// Reset clears all buckets.
func (l *Limiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buckets = make(map[string]*Bucket)
}

func (b *Bucket) refill() {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens = min(b.tokens+elapsed*b.refillRate, b.maxTokens)
	b.lastRefill = now
}
