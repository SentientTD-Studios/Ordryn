package statestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"GoTodo/internal/storage"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

var pgStore *postgresStore

func TestMain(m *testing.M) {
	port := uint32(5446)
	runtimePath := filepath.Join(os.TempDir(), "gotodo-embedded-pg-statestore")
	var db *embeddedpostgres.EmbeddedPostgres
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		db = embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
			Version(embeddedpostgres.V16).
			Port(port).
			Database("gotodo_statestore").
			RuntimePath(runtimePath))
		if last = db.Start(); last == nil {
			break
		}
		fmt.Fprintf(os.Stderr, "start postgres (attempt %d/3): %v\n", attempt, last)
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	if last != nil {
		fmt.Fprintf(os.Stderr, "start postgres: %v\n", last)
		os.Exit(1)
	}

	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_PORT", fmt.Sprintf("%d", port))
	os.Setenv("DB_USER", "postgres")
	os.Setenv("DB_PASSWORD", "postgres")
	os.Setenv("DB_NAME", "gotodo_statestore")
	os.Setenv("DB_SSLMODE", "disable")

	s, err := newPostgresStore(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres store: %v\n", err)
		_ = db.Stop()
		os.Exit(1)
	}
	pgStore = s

	code := m.Run()
	storage.CloseSharedPool()
	_ = db.Stop()
	os.Exit(code)
}

// expire backdates key so it reads as expired without sleeping.
func expire(t *testing.T, key string) {
	t.Helper()
	if _, err := pgStore.pool.Exec(context.Background(),
		`UPDATE ephemeral_state SET expires_at = now() - interval '1 second' WHERE key = $1`, key); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresSetGetExpire(t *testing.T) {
	ctx := context.Background()
	if err := pgStore.Set(ctx, "t:get", []byte("v1"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := pgStore.Set(ctx, "t:get", []byte("v2"), time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := pgStore.Get(ctx, "t:get")
	if err != nil || string(got) != "v2" {
		t.Fatalf("Get = %q, %v; want v2", got, err)
	}
	if ok, _ := pgStore.Exists(ctx, "t:get"); !ok {
		t.Fatal("Exists = false for a live key")
	}
	expire(t, "t:get")
	if _, err := pgStore.Get(ctx, "t:get"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get expired = %v; want ErrNotFound", err)
	}
	if ok, _ := pgStore.Exists(ctx, "t:get"); ok {
		t.Fatal("Exists = true for an expired key")
	}
	if _, err := pgStore.Get(ctx, "t:missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing = %v; want ErrNotFound", err)
	}
}

func TestPostgresGetDelOnlyOnce(t *testing.T) {
	ctx := context.Background()
	if err := pgStore.Set(ctx, "t:once", []byte("token"), time.Minute); err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := pgStore.GetDel(ctx, "t:once"); err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d callers got the value; want exactly 1", wins.Load())
	}

	if err := pgStore.Set(ctx, "t:once-expired", []byte("x"), time.Minute); err != nil {
		t.Fatal(err)
	}
	expire(t, "t:once-expired")
	if _, err := pgStore.GetDel(ctx, "t:once-expired"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetDel expired = %v; want ErrNotFound", err)
	}
}

func TestPostgresSetManyAndDel(t *testing.T) {
	ctx := context.Background()
	err := pgStore.SetMany(ctx, map[string][]byte{"t:a": []byte("1"), "t:b": []byte("2")}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"t:a", "t:b"} {
		if ok, _ := pgStore.Exists(ctx, k); !ok {
			t.Fatalf("%s missing after SetMany", k)
		}
	}
	if err := pgStore.Del(ctx, "t:a"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := pgStore.Exists(ctx, "t:a"); ok {
		t.Fatal("t:a still exists after Del")
	}
}

func TestPostgresCompareAndSet(t *testing.T) {
	ctx := context.Background()
	if err := pgStore.Set(ctx, "t:cas", []byte("old"), time.Minute); err != nil {
		t.Fatal(err)
	}
	ok, err := pgStore.CompareAndSet(ctx, "t:cas", []byte("stale"), []byte("new"), time.Minute)
	if err != nil || ok {
		t.Fatalf("CAS with stale value = %v, %v; want false", ok, err)
	}
	ok, err = pgStore.CompareAndSet(ctx, "t:cas", []byte("old"), []byte("new"), time.Minute)
	if err != nil || !ok {
		t.Fatalf("CAS with current value = %v, %v; want true", ok, err)
	}
	if got, _ := pgStore.Get(ctx, "t:cas"); string(got) != "new" {
		t.Fatalf("value = %q; want new", got)
	}
	expire(t, "t:cas")
	if ok, _ := pgStore.CompareAndSet(ctx, "t:cas", []byte("new"), []byte("x"), time.Minute); ok {
		t.Fatal("CAS succeeded on an expired key")
	}
}

func TestPostgresIncr(t *testing.T) {
	ctx := context.Background()
	for want := int64(1); want <= 3; want++ {
		n, err := pgStore.Incr(ctx, "t:count", time.Minute)
		if err != nil || n != want {
			t.Fatalf("Incr = %d, %v; want %d", n, err, want)
		}
	}
	// Stored as decimal text, like Redis INCR.
	if got, _ := pgStore.Get(ctx, "t:count"); string(got) != "3" {
		t.Fatalf("Get counter = %q; want \"3\"", got)
	}
	// Later increments keep the original window.
	var before, after time.Time
	_ = pgStore.pool.QueryRow(ctx, `SELECT expires_at FROM ephemeral_state WHERE key = 't:count'`).Scan(&before)
	_, _ = pgStore.Incr(ctx, "t:count", time.Hour)
	_ = pgStore.pool.QueryRow(ctx, `SELECT expires_at FROM ephemeral_state WHERE key = 't:count'`).Scan(&after)
	if !before.Equal(after) {
		t.Fatalf("Incr moved the expiry from %v to %v", before, after)
	}
	expire(t, "t:count")
	if n, _ := pgStore.Incr(ctx, "t:count", time.Minute); n != 1 {
		t.Fatalf("Incr after expiry = %d; want 1", n)
	}
}

func TestPostgresSweepDeletesExpired(t *testing.T) {
	ctx := context.Background()
	_ = pgStore.Set(ctx, "t:sweep", []byte("x"), time.Minute)
	expire(t, "t:sweep")
	if _, err := pgStore.pool.Exec(ctx, `DELETE FROM ephemeral_state WHERE expires_at <= now()`); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = pgStore.pool.QueryRow(ctx, `SELECT count(*) FROM ephemeral_state WHERE key = 't:sweep'`).Scan(&n)
	if n != 0 {
		t.Fatal("expired row survived the sweep")
	}
}

func TestPostgresPublishSubscribe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type msg struct {
		channel string
		payload string
	}
	got := make(chan msg, 4)
	pgStore.Subscribe(ctx, func(channel string, payload []byte) {
		got <- msg{channel, string(payload)}
	})

	// LISTEN starts asynchronously; publish until the first message lands.
	deadline := time.After(10 * time.Second)
	for {
		if err := pgStore.Publish(ctx, "user:7", []byte(`{"type":"task.updated"}`)); err != nil {
			t.Fatal(err)
		}
		select {
		case m := <-got:
			if m.channel != "user:7" || m.payload != `{"type":"task.updated"}` {
				t.Fatalf("got %+v", m)
			}
			return
		case <-time.After(200 * time.Millisecond):
		case <-deadline:
			t.Fatal("no notification received")
		}
	}
}

func TestPostgresPublishRejectsOversized(t *testing.T) {
	big := make([]byte, pgNotifyMaxPayload)
	for i := range big {
		big[i] = 'x'
	}
	if err := pgStore.Publish(context.Background(), "user:1", big); err == nil {
		t.Fatal("oversized payload should be rejected")
	}
}
