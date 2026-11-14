package storage

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	sharedPool     *pgxpool.Pool
	poolOnce       sync.Once
	poolErr        error
	poolCloseMutex sync.Mutex
)

func databaseDSN() (string, error) {
	required := []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME"}
	config := make(map[string]string)
	for _, key := range required {
		val := os.Getenv(key)
		if val == "" {
			return "", fmt.Errorf("missing env variable: %s", key)
		}
		config[key] = val
	}

	sslMode := strings.ToLower(strings.TrimSpace(os.Getenv("DB_SSLMODE")))
	switch sslMode {
	case "", "false", "0":
		sslMode = "disable"
	case "true", "1":
		sslMode = "require"
	}

	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		config["DB_USER"],
		config["DB_PASSWORD"],
		config["DB_HOST"],
		config["DB_PORT"],
		config["DB_NAME"],
		sslMode,
	), nil
}

// Pool defaults. pgxpool's own default (max(4, NumCPU)) is 4 on a small VPS,
// which a handful of slow queries plus the background workers can exhaust,
// leaving every other request queued with no deadline.
const (
	defaultDBMaxConns         = 20
	defaultStatementTimeout   = "60s"
	defaultIdleInTxTimeout    = "5min"
	defaultHealthCheckPeriod  = 30 * time.Second
	defaultMaxConnIdleTime    = 5 * time.Minute
	defaultConnectTimeoutSecs = 10
)

func openDatabaseConn() (*pgxpool.Pool, error) {
	dsn, err := databaseDSN()
	if err != nil {
		return nil, err
	}
	cfg, err := poolConfig(dsn)
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(context.Background(), cfg)
}

// poolConfig applies size caps and server-side timeouts so a stuck query or a
// leaked transaction frees its connection instead of starving the pool.
//
//	DB_MAX_CONNS                  pool size (default 20)
//	DB_STATEMENT_TIMEOUT          per-statement limit, Postgres syntax (default 60s; 0 disables)
//	DB_IDLE_IN_TX_TIMEOUT         kill sessions idle inside a transaction (default 5min; 0 disables)
func poolConfig(dsn string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = defaultDBMaxConns
	if v := strings.TrimSpace(os.Getenv("DB_MAX_CONNS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("DB_MAX_CONNS: want a positive integer, got %q", v)
		}
		cfg.MaxConns = int32(n)
	}
	cfg.HealthCheckPeriod = defaultHealthCheckPeriod
	cfg.MaxConnIdleTime = defaultMaxConnIdleTime
	cfg.ConnConfig.ConnectTimeout = defaultConnectTimeoutSecs * time.Second

	params := cfg.ConnConfig.RuntimeParams
	params["statement_timeout"] = envOr("DB_STATEMENT_TIMEOUT", defaultStatementTimeout)
	params["idle_in_transaction_session_timeout"] = envOr("DB_IDLE_IN_TX_TIMEOUT", defaultIdleInTxTimeout)
	return cfg, nil
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// PingDatabase checks the shared pool can hand out a connection and reach
// Postgres before ctx expires.
func PingDatabase(ctx context.Context) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	if pool == nil {
		return fmt.Errorf("database is not open")
	}
	return pool.Ping(ctx)
}

// PoolStats is a point-in-time view of the shared pool for health checks.
type PoolStats struct {
	OK            bool    `json:"ok"`
	PingMS        float64 `json:"ping_ms"`
	MaxConns      int32   `json:"max_conns"`
	TotalConns    int32   `json:"total_conns"`
	AcquiredConns int32   `json:"acquired_conns"`
	IdleConns     int32   `json:"idle_conns"`
	// EmptyAcquires counts acquires that found no idle connection. That
	// includes ones that just dialed a new connection while the pool was below
	// MaxConns (normal after idle connections are reaped), so on its own it is
	// not a sign of contention; EmptyAcquireWaitMS shows what those cost.
	EmptyAcquires      int64   `json:"empty_acquire_count"`
	EmptyAcquireWaitMS float64 `json:"empty_acquire_wait_ms"`
	// AcquireTotalMS is the time spent in every successful acquire, including
	// the near-instant ones that got an idle connection; it is not just waiting.
	// The JSON name predates this and is kept for API compatibility.
	AcquireTotalMS float64 `json:"acquire_wait_total_ms"`
}

// SharedPoolStats pings the database (bounded by ctx) and reports pool usage.
// A ping that can't get a connection in time shows up as ok=false while
// acquired_conns sits at max_conns: the signature of pool exhaustion.
func SharedPoolStats(ctx context.Context) PoolStats {
	pool, err := OpenDatabase()
	if err != nil || pool == nil {
		return PoolStats{}
	}
	start := time.Now()
	pingErr := pool.Ping(ctx)
	s := pool.Stat()
	return PoolStats{
		OK:                 pingErr == nil,
		PingMS:             float64(time.Since(start).Microseconds()) / 1000,
		MaxConns:           s.MaxConns(),
		TotalConns:         s.TotalConns(),
		AcquiredConns:      s.AcquiredConns(),
		IdleConns:          s.IdleConns(),
		EmptyAcquires:      s.EmptyAcquireCount(),
		EmptyAcquireWaitMS: float64(s.EmptyAcquireWaitTime().Microseconds()) / 1000,
		AcquireTotalMS:     float64(s.AcquireDuration().Microseconds()) / 1000,
	}
}

// OpenDatabase returns a shared connection pool (singleton).
func OpenDatabase() (*pgxpool.Pool, error) {
	poolOnce.Do(func() {
		sharedPool, poolErr = openDatabaseConn()
	})
	return sharedPool, poolErr
}

// CloseDatabase is a no-op for the shared pool; kept for API compatibility.
func CloseDatabase(pool *pgxpool.Pool) {
	_ = pool
}

// CloseSharedPool closes the shared pool (tests/shutdown).
func CloseSharedPool() {
	poolCloseMutex.Lock()
	defer poolCloseMutex.Unlock()
	if sharedPool != nil {
		sharedPool.Close()
		sharedPool = nil
	}
	// Also forget a failed open (e.g. env not set yet), so the next call retries.
	poolErr = nil
	poolOnce = sync.Once{}
}

// ResetSharedPoolForTests closes and resets the singleton between tests. The next
// connection may be to a different database, so caches of database rows go too.
func ResetSharedPoolForTests() {
	CloseSharedPool()
	InvalidateSiteRoleCache()
	resetSystemUserCache()
	resetTaskTextLimitsCache()
}

// PGEnv returns the libpq environment (PGHOST, PGPORT, PGUSER, PGPASSWORD,
// PGDATABASE, PGSSLMODE) for this site's database, for tools like pg_dump. The
// password travels in the environment, never on a command line.
func PGEnv() ([]string, error) {
	vals := map[string]string{}
	for _, key := range []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME"} {
		v := os.Getenv(key)
		if v == "" {
			return nil, fmt.Errorf("missing env variable: %s", key)
		}
		vals[key] = v
	}
	sslMode := strings.ToLower(strings.TrimSpace(os.Getenv("DB_SSLMODE")))
	switch sslMode {
	case "", "false", "0":
		sslMode = "disable"
	case "true", "1":
		sslMode = "require"
	}
	return []string{
		"PGHOST=" + vals["DB_HOST"], "PGPORT=" + vals["DB_PORT"], "PGUSER=" + vals["DB_USER"],
		"PGPASSWORD=" + vals["DB_PASSWORD"], "PGDATABASE=" + vals["DB_NAME"], "PGSSLMODE=" + sslMode,
	}, nil
}
