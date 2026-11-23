package utils

import (
	"bytes"
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"GoTodo/internal/statestore"
	"GoTodo/internal/storage"
)

// fakeStore is an in-memory statestore.Store for unit tests (no expiry).
type fakeStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newFakeStore(t *testing.T) *fakeStore {
	f := &fakeStore{m: map[string][]byte{}}
	t.Cleanup(statestore.SetForTest(f))
	return f
}

func (f *fakeStore) Backend() string                                 { return "fake" }
func (f *fakeStore) Ping(context.Context) error                      { return nil }
func (f *fakeStore) Close()                                          {}
func (f *fakeStore) Subscribe(context.Context, func(string, []byte)) {}
func (f *fakeStore) Publish(context.Context, string, []byte) error   { return nil }

func (f *fakeStore) Set(_ context.Context, k string, v []byte, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[k] = append([]byte(nil), v...)
	return nil
}

func (f *fakeStore) SetMany(ctx context.Context, vals map[string][]byte, ttl time.Duration) error {
	for k, v := range vals {
		_ = f.Set(ctx, k, v, ttl)
	}
	return nil
}

func (f *fakeStore) Get(_ context.Context, k string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.m[k]
	if !ok {
		return nil, statestore.ErrNotFound
	}
	return v, nil
}

func (f *fakeStore) GetDel(_ context.Context, k string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.m[k]
	if !ok {
		return nil, statestore.ErrNotFound
	}
	delete(f.m, k)
	return v, nil
}

func (f *fakeStore) CompareAndSet(_ context.Context, k string, old, v []byte, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cur, ok := f.m[k]; !ok || !bytes.Equal(cur, old) {
		return false, nil
	}
	f.m[k] = v
	return true, nil
}

func (f *fakeStore) Del(_ context.Context, k string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.m, k)
	return nil
}

func (f *fakeStore) Exists(_ context.Context, k string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.m[k]
	return ok, nil
}

func (f *fakeStore) Incr(_ context.Context, k string, _ time.Duration) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, _ := strconv.ParseInt(string(f.m[k]), 10, 64)
	n++
	f.m[k] = []byte(strconv.FormatInt(n, 10))
	return n, nil
}

func (f *fakeStore) Allow(context.Context, string, int, float64, time.Duration) (bool, error) {
	return true, nil
}

func TestUndoTokenWorksOnce(t *testing.T) {
	newFakeStore(t)
	ctx := context.Background()
	tok, err := SaveUndoToken(ctx, 5, []UndoTaskSnapshot{{ID: 1, Title: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadUndoToken(ctx, 6, tok); err == nil {
		t.Fatal("another user's token should not load")
	}
	tasks, err := LoadUndoToken(ctx, 5, tok)
	if err != nil || len(tasks) != 1 || tasks[0].Title != "a" {
		t.Fatalf("LoadUndoToken = %+v, %v", tasks, err)
	}
	if _, err := LoadUndoToken(ctx, 5, tok); err == nil {
		t.Fatal("undo token should work only once")
	}
}

func TestDeviceAuthKeyHandedOutOnce(t *testing.T) {
	newFakeStore(t)
	ctx := context.Background()
	rec, err := CreateDeviceAuthRequest(ctx, "Phone", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	key := &storage.APIKey{ID: 9, KeyPrefix: "ord_ab", Name: "Phone"}
	if err := ApproveDeviceAuth(ctx, rec.UserCode, 3, "secret", key); err != nil {
		t.Fatal(err)
	}
	if err := DenyDeviceAuth(ctx, rec.UserCode); err == nil {
		t.Fatal("deny after approve should fail")
	}

	got, plain, err := ConsumeApprovedDeviceToken(ctx, rec.DeviceCode)
	if err != nil || plain != "secret" || got.Status != DeviceAuthApproved || got.KeyID != 9 {
		t.Fatalf("first poll = %+v, %q, %v", got, plain, err)
	}
	got, plain, err = ConsumeApprovedDeviceToken(ctx, rec.DeviceCode)
	if err != nil || plain != "" || got.Status != DeviceAuthApproved {
		t.Fatalf("second poll = %+v, %q, %v; want approved with no key", got, plain, err)
	}
}

func TestDeviceAuthConcurrentPollsGetKeyOnce(t *testing.T) {
	newFakeStore(t)
	ctx := context.Background()
	rec, err := CreateDeviceAuthRequest(ctx, "Phone", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApproveDeviceAuth(ctx, rec.UserCode, 3, "secret", &storage.APIKey{ID: 1}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	keys := 0
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, plain, err := ConsumeApprovedDeviceToken(ctx, rec.DeviceCode)
			if err != nil {
				t.Errorf("poll: %v", err)
				return
			}
			if plain != "" {
				mu.Lock()
				keys++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if keys != 1 {
		t.Fatalf("%d polls received the key; want 1", keys)
	}
}

func TestLoginLockoutCounters(t *testing.T) {
	newFakeStore(t)
	ctx := context.Background()
	const email, ip = "a@example.com", "10.0.0.1"
	for i := 0; i < LoginFailuresPerEmailAndIP; i++ {
		if g, err := CheckLogin(ctx, email, ip); err != nil || g != LoginAllowed {
			t.Fatalf("attempt %d: gate=%v err=%v", i+1, g, err)
		}
		if err := RecordFailedLogin(ctx, email, ip); err != nil {
			t.Fatal(err)
		}
	}
	if g, _ := CheckLogin(ctx, email, ip); g != LoginLocked {
		t.Fatalf("gate after %d failures = %v; want locked", LoginFailuresPerEmailAndIP, g)
	}
	if g, _ := CheckLogin(ctx, email, "10.0.0.2"); g != LoginAllowed {
		t.Fatalf("other IP gate = %v; want allowed", g)
	}
	if err := ClearFailedLogin(ctx, email, ip); err != nil {
		t.Fatal(err)
	}
	if g, _ := CheckLogin(ctx, email, ip); g != LoginAllowed {
		t.Fatalf("gate after clear = %v; want allowed", g)
	}
}
