package statestore

import (
	"testing"
	"time"
)

func TestMemBucketsRefill(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	m := newMemBuckets()
	m.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !m.allow("k", 3, 1, time.Minute) {
			t.Fatalf("request %d denied within capacity", i+1)
		}
	}
	if m.allow("k", 3, 1, time.Minute) {
		t.Fatal("request over capacity allowed")
	}
	if !m.allow("other", 3, 1, time.Minute) {
		t.Fatal("buckets should be independent")
	}
	now = now.Add(time.Second)
	if !m.allow("k", 3, 1, time.Minute) {
		t.Fatal("no token after one second of refill")
	}
	if m.allow("k", 3, 1, time.Minute) {
		t.Fatal("refill gave more than one token per second")
	}
	now = now.Add(time.Hour)
	for i := 0; i < 3; i++ {
		if !m.allow("k", 3, 1, time.Minute) {
			t.Fatal("bucket should be full again after a long idle")
		}
	}
}

func TestMemBucketsSweepsIdle(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	m := newMemBuckets()
	m.now = func() time.Time { return now }
	m.allow("a", 1, 1, time.Second)
	now = now.Add(2 * bucketSweepEvery)
	m.allow("b", 1, 1, time.Second)
	if _, ok := m.buckets["a"]; ok {
		t.Fatal("idle bucket wasn't swept")
	}
}

func TestNotifyRoundTrip(t *testing.T) {
	msg, err := encodeNotify("user:12", []byte(`{"type":"task.created","task_id":3}`))
	if err != nil {
		t.Fatal(err)
	}
	ch, payload, ok := decodeNotify(msg)
	if !ok || ch != "user:12" || string(payload) != `{"type":"task.created","task_id":3}` {
		t.Fatalf("decode = %q %q %v", ch, payload, ok)
	}
	if _, err := encodeNotify("bad\nchannel", nil); err == nil {
		t.Fatal("newline in channel should be rejected")
	}
	if _, err := encodeNotify("c", []byte{'a', 0}); err == nil {
		t.Fatal("NUL in payload should be rejected")
	}
}
