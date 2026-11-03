package storage

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

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

func openDatabaseConn() (*pgxpool.Pool, error) {
	dsn, err := databaseDSN()
	if err != nil {
		return nil, err
	}
	return pgxpool.New(context.Background(), dsn)
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
