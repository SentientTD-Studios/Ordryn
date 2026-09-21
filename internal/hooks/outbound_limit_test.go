package hooks

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseRetryAfterHeaderAndBody(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "12")
	if got := parseRetryAfter(h, nil); got != 12*time.Second {
		t.Fatalf("header seconds=%s", got)
	}

	h = http.Header{}
	body := []byte(`{"message":"You are being rate limited.","retry_after":1.5}`)
	if got := parseRetryAfter(h, body); got != 1500*time.Millisecond {
		t.Fatalf("discord body=%s", got)
	}

	h = http.Header{}
	body = []byte(`{"error":{"code":429,"retry_after":"4"}}`)
	if got := parseRetryAfter(h, body); got != 4*time.Second {
		t.Fatalf("nested body=%s", got)
	}
}

func TestRetryDelayForErrorPrefersRetryAfter(t *testing.T) {
	err := &webhookHTTPError{Status: 429, RetryAfter: 2 * time.Second}
	if got := retryDelayForError(err, 1); got != 2*time.Second {
		t.Fatalf("got %s", got)
	}
	tiny := &webhookHTTPError{Status: 429, RetryAfter: 200 * time.Millisecond}
	if got := retryDelayForError(tiny, 1); got != time.Second {
		t.Fatalf("min delay=%s", got)
	}
}

func TestOutboundLimiterHoldsDestination(t *testing.T) {
	l := newOutboundLimiter()
	fixed := time.Date(2026, 9, 21, 16, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return fixed }
	key := destHoldKey("discord", 7, 0)
	l.holdUntil(key, 5*time.Second, "Destination is rate-limited. Next retry in 5 seconds.")
	delay, msg := l.delay(key)
	if delay != 5*time.Second {
		t.Fatalf("delay=%s", delay)
	}
	if !strings.Contains(strings.ToLower(msg), "rate-limited") {
		t.Fatalf("msg=%q", msg)
	}
	l.now = func() time.Time { return fixed.Add(6 * time.Second) }
	if delay, _ = l.delay(key); delay != 0 {
		t.Fatalf("expired hold=%s", delay)
	}
	l.clear(key)
}
