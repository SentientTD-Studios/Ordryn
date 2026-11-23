package statestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"GoTodo/internal/config"
	"GoTodo/internal/storage"
	"GoTodo/internal/workers"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// pgNotifyChannel carries live updates between instances.
	pgNotifyChannel = "ordryn_live"
	// pgNotifyMaxPayload is just under Postgres's 8000-byte NOTIFY limit.
	// Live events are small invalidations (a type and a few ids), far below it.
	pgNotifyMaxPayload = 7900
	// stateSweepEvery is how often expired rows are deleted. Reads already
	// ignore them, so this only keeps the table small.
	stateSweepEvery = 5 * time.Minute
)

// postgresStore keeps state in the ephemeral_state table of the main database
// and rate-limit buckets in memory (see memBuckets).
type postgresStore struct {
	pool    *pgxpool.Pool
	buckets *memBuckets
}

func newPostgresStore(ctx context.Context) (*postgresStore, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, err
	}
	if err := ensureStateSchema(ctx, pool); err != nil {
		return nil, err
	}
	s := &postgresStore{pool: pool, buckets: newMemBuckets()}
	startStateSweeper(pool)
	return s, nil
}

// ensureStateSchema creates the state table. It's an ordinary (logged) table:
// sign-out revocations and lockouts must survive a database crash.
func ensureStateSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS ephemeral_state (
			key        TEXT PRIMARY KEY,
			value      BYTEA NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_ephemeral_state_expires_at ON ephemeral_state (expires_at);
	`)
	if err != nil {
		return fmt.Errorf("create ephemeral_state: %w", err)
	}
	return nil
}

func startStateSweeper(pool *pgxpool.Pool) {
	const name = "state-store-sweep"
	workers.Register(name, stateSweepEvery)
	go func() {
		ticker := time.NewTicker(stateSweepEvery)
		defer ticker.Stop()
		for {
			workers.Run(name, func() error {
				_, err := pool.Exec(context.Background(), `DELETE FROM ephemeral_state WHERE expires_at <= now()`)
				return err
			})
			<-ticker.C
		}
	}()
}

func (s *postgresStore) Backend() string { return config.StateStorePostgres }

func (s *postgresStore) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Close leaves the shared pool open; it belongs to the storage package.
func (s *postgresStore) Close() {}

const upsertSQL = `
	INSERT INTO ephemeral_state (key, value, expires_at)
	VALUES ($1, $2, now() + $3 * interval '1 millisecond')
	ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, expires_at = EXCLUDED.expires_at`

func (s *postgresStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	_, err := s.pool.Exec(ctx, upsertSQL, key, value, ttl.Milliseconds())
	return err
}

func (s *postgresStore) SetMany(ctx context.Context, values map[string][]byte, ttl time.Duration) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for k, v := range values {
			if _, err := tx.Exec(ctx, upsertSQL, k, v, ttl.Milliseconds()); err != nil {
				return err
			}
		}
		return nil
	})
}

func pgResult(b []byte, err error) ([]byte, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return b, err
}

func (s *postgresStore) Get(ctx context.Context, key string) ([]byte, error) {
	var b []byte
	err := s.pool.QueryRow(ctx,
		`SELECT value FROM ephemeral_state WHERE key = $1 AND expires_at > now()`, key).Scan(&b)
	return pgResult(b, err)
}

func (s *postgresStore) GetDel(ctx context.Context, key string) ([]byte, error) {
	var b []byte
	var live bool
	err := s.pool.QueryRow(ctx,
		`DELETE FROM ephemeral_state WHERE key = $1 RETURNING value, expires_at > now()`, key).Scan(&b, &live)
	if err == nil && !live {
		return nil, ErrNotFound
	}
	return pgResult(b, err)
}

func (s *postgresStore) CompareAndSet(ctx context.Context, key string, old, value []byte, ttl time.Duration) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE ephemeral_state SET value = $3, expires_at = now() + $4 * interval '1 millisecond'
		WHERE key = $1 AND value = $2 AND expires_at > now()`, key, old, value, ttl.Milliseconds())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *postgresStore) Del(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM ephemeral_state WHERE key = $1`, key)
	return err
}

func (s *postgresStore) Exists(ctx context.Context, key string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM ephemeral_state WHERE key = $1 AND expires_at > now())`, key).Scan(&ok)
	return ok, err
}

// Incr stores the count as decimal text, matching what Redis INCR stores, so
// Get returns the same bytes from either backend. An expired counter restarts
// at 1 with a fresh window.
func (s *postgresStore) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO ephemeral_state AS st (key, value, expires_at)
		VALUES ($1, '1'::bytea, now() + $2 * interval '1 millisecond')
		ON CONFLICT (key) DO UPDATE SET
			value = CASE WHEN st.expires_at <= now() THEN '1'::bytea
				ELSE convert_to((convert_from(st.value, 'UTF8')::bigint + 1)::text, 'UTF8') END,
			expires_at = CASE WHEN st.expires_at <= now() THEN EXCLUDED.expires_at ELSE st.expires_at END
		RETURNING convert_from(value, 'UTF8')::bigint`, key, ttl.Milliseconds()).Scan(&n)
	return n, err
}

func (s *postgresStore) Allow(_ context.Context, key string, capacity int, refillPerSec float64, ttl time.Duration) (bool, error) {
	return s.buckets.allow(key, capacity, refillPerSec, ttl), nil
}

// encodeNotify packs a channel and payload into one NOTIFY message. Channel
// names never contain a newline, so the first one separates them.
func encodeNotify(channel string, payload []byte) (string, error) {
	if strings.Contains(channel, "\n") {
		return "", fmt.Errorf("channel %q contains a newline", channel)
	}
	if bytes.IndexByte(payload, 0) >= 0 {
		return "", errors.New("payload contains a NUL byte")
	}
	msg := channel + "\n" + string(payload)
	if len(msg) > pgNotifyMaxPayload {
		return "", fmt.Errorf("live event too large for NOTIFY (%d bytes)", len(msg))
	}
	return msg, nil
}

func decodeNotify(msg string) (channel string, payload []byte, ok bool) {
	channel, rest, ok := strings.Cut(msg, "\n")
	return channel, []byte(rest), ok
}

func (s *postgresStore) Publish(ctx context.Context, channel string, payload []byte) error {
	msg, err := encodeNotify(channel, payload)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `SELECT pg_notify($1, $2)`, pgNotifyChannel, msg)
	return err
}

// Subscribe holds one dedicated connection (outside the pool) open on LISTEN,
// reconnecting with backoff. It needs a direct or session-pooled connection:
// LISTEN doesn't work through a transaction-pooling proxy like PgBouncer in
// transaction mode.
func (s *postgresStore) Subscribe(ctx context.Context, handler func(channel string, payload []byte)) {
	connCfg := s.pool.Config().ConnConfig.Copy()
	go func() {
		backoff := time.Second
		for ctx.Err() == nil {
			err := s.listen(ctx, connCfg, handler, func() { backoff = time.Second })
			if ctx.Err() != nil {
				return
			}
			log.Printf("postgres live-update listener: %v; reconnecting in %s", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 30*time.Second)
		}
	}()
}

func (s *postgresStore) listen(ctx context.Context, cfg *pgx.ConnConfig, handler func(string, []byte), connected func()) error {
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "LISTEN "+pgNotifyChannel); err != nil {
		return err
	}
	connected()
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if channel, payload, ok := decodeNotify(n.Payload); ok {
			handler(channel, payload)
		}
	}
}
