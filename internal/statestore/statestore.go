// Package statestore keeps short-lived server state: sign-out revocations,
// sign-in lockout counters, device/SSO/OAuth handshakes, undo tokens and rate
// limits. It also carries live-update events between app instances.
//
// Two backends implement it. Redis suits large or busy sites. Postgres uses the
// main database, so a small install needs nothing besides Postgres; it still
// supports several app instances (LISTEN/NOTIFY carries live updates).
// STATE_STORE picks one; see config.StateStoreBackend.
package statestore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"GoTodo/internal/config"
)

// ErrNotFound is returned when a key is missing or has expired.
var ErrNotFound = errors.New("statestore: not found")

// Store is the state backend. Every value expires: ttl must be positive.
type Store interface {
	// Backend is config.StateStoreRedis or config.StateStorePostgres.
	Backend() string
	Ping(ctx context.Context) error

	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// SetMany writes all keys or none.
	SetMany(ctx context.Context, values map[string][]byte, ttl time.Duration) error
	Get(ctx context.Context, key string) ([]byte, error)
	// GetDel reads and deletes key in one step, so only one caller gets it.
	GetDel(ctx context.Context, key string) ([]byte, error)
	// CompareAndSet replaces key's value only if it still equals old, and
	// reports whether it did.
	CompareAndSet(ctx context.Context, key string, old, value []byte, ttl time.Duration) (bool, error)
	Del(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	// Incr adds one to a counter, creating it with ttl. The ttl isn't extended
	// by later increments, so the counter covers a fixed window.
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)

	// Allow takes one token from a token bucket of the given capacity that
	// refills at refillPerSec, reporting whether one was available. An idle
	// bucket is forgotten after ttl.
	Allow(ctx context.Context, key string, capacity int, refillPerSec float64, ttl time.Duration) (bool, error)

	// Publish sends payload to every instance's Subscribe handler, this one included.
	Publish(ctx context.Context, channel string, payload []byte) error
	// Subscribe calls handler for each published message until ctx ends,
	// reconnecting as needed. Messages sent while disconnected are lost.
	Subscribe(ctx context.Context, handler func(channel string, payload []byte))

	Close()
}

var (
	mu      sync.RWMutex
	current Store
)

// Init connects the backend STATE_STORE selects and installs it.
func Init(ctx context.Context) (Store, error) {
	backend, err := config.StateStoreBackend()
	if err != nil {
		return nil, err
	}
	var s Store
	switch backend {
	case config.StateStoreRedis:
		s, err = newRedisStore(ctx)
	case config.StateStorePostgres:
		s, err = newPostgresStore(ctx)
	default:
		err = fmt.Errorf("unknown state store %q", backend)
	}
	if err != nil {
		return nil, fmt.Errorf("%s state store: %w", backend, err)
	}
	set(s)
	return s, nil
}

// Current returns the installed store, or nil before Init (as in unit tests).
func Current() Store {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

func set(s Store) {
	mu.Lock()
	current = s
	mu.Unlock()
}

// SetForTest installs s (nil for none) and returns a function that restores
// the previous store.
func SetForTest(s Store) (restore func()) {
	mu.Lock()
	prev := current
	current = s
	mu.Unlock()
	return func() { set(prev) }
}

// Ping round-trips to the backend and reports how long it took.
func Ping(ctx context.Context) (time.Duration, error) {
	s := Current()
	if s == nil {
		return 0, errors.New("state store is not configured")
	}
	start := time.Now()
	err := s.Ping(ctx)
	return time.Since(start), err
}
