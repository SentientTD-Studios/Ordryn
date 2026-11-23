package statestore

import (
	"math"
	"sync"
	"time"
)

// memBuckets is an in-process token-bucket limiter. The Postgres backend uses
// it for rate limits so ordinary API traffic doesn't write to the database on
// every request. Each instance keeps its own buckets, so with N instances a
// client can get up to N times the limit, and a restart refills them; neither
// matters for the sites the Postgres backend is meant for. Sign-in lockouts
// are counters in the database, not buckets, so they hold across instances
// and restarts.
type memBuckets struct {
	mu        sync.Mutex
	buckets   map[string]*memBucket
	lastSweep time.Time
	now       func() time.Time
}

type memBucket struct {
	tokens  float64
	last    time.Time
	expires time.Time
}

// bucketSweepEvery bounds how often idle buckets are cleared out.
const bucketSweepEvery = time.Minute

func newMemBuckets() *memBuckets {
	return &memBuckets{buckets: make(map[string]*memBucket), now: time.Now}
}

// LocalLimiter is an in-process token-bucket limiter for routes that don't
// need a shared limit, whatever the state store backend. It keeps per-request
// rate limiting off Redis for authenticated, high-frequency callers.
type LocalLimiter struct{ b *memBuckets }

// NewLocalLimiter returns an empty LocalLimiter.
func NewLocalLimiter() *LocalLimiter { return &LocalLimiter{b: newMemBuckets()} }

// Allow takes one token from the bucket named key and reports whether one was
// available (see memBuckets.allow).
func (l *LocalLimiter) Allow(key string, capacity int, refillPerSec float64, ttl time.Duration) bool {
	return l.b.allow(key, capacity, refillPerSec, ttl)
}

func (m *memBuckets) allow(key string, capacity int, refillPerSec float64, ttl time.Duration) bool {
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()

	if now.Sub(m.lastSweep) >= bucketSweepEvery {
		for k, b := range m.buckets {
			if now.After(b.expires) {
				delete(m.buckets, k)
			}
		}
		m.lastSweep = now
	}

	b, ok := m.buckets[key]
	if !ok || now.After(b.expires) {
		b = &memBucket{tokens: float64(capacity), last: now}
		m.buckets[key] = b
	}
	elapsed := math.Max(0, now.Sub(b.last).Seconds())
	b.tokens = math.Min(float64(capacity), b.tokens+elapsed*refillPerSec)
	b.last = now
	b.expires = now.Add(ttl)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
