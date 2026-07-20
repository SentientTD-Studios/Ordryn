package storage

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	sharedPool     *pgxpool.Pool
	poolOnce       sync.Once
	poolErr        error
	poolCloseMutex sync.Mutex
)

func openDatabaseConn() (*pgxpool.Pool, error) {
	required := []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME"}
	config := make(map[string]string)
	for _, key := range required {
		val := os.Getenv(key)
		if val == "" {
			return nil, fmt.Errorf("missing env variable: %s", key)
		}
		config[key] = val
	}

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s",
		config["DB_USER"],
		config["DB_PASSWORD"],
		config["DB_HOST"],
		config["DB_PORT"],
		config["DB_NAME"],
	)

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
		poolOnce = sync.Once{}
	}
}

// ResetSharedPoolForTests closes and resets the singleton between tests.
func ResetSharedPoolForTests() {
	CloseSharedPool()
}
