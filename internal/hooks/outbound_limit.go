package hooks

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// ErrRateLimited is returned when a chat destination responds HTTP 429.
var ErrRateLimited = errors.New("destination rate limited")

type destHold struct {
	until time.Time
	msg   string
}

type outboundLimiter struct {
	mu   sync.Mutex
	hold map[string]destHold
	now  func() time.Time
}

var destLimiter = newOutboundLimiter()

func newOutboundLimiter() *outboundLimiter {
	return &outboundLimiter{
		hold: map[string]destHold{},
		now:  time.Now,
	}
}

func destHoldKey(extensionID string, projectID, userID int) string {
	return strings.TrimSpace(extensionID) + ":" + itoa(projectID) + ":" + itoa(userID)
}

func (l *outboundLimiter) delay(key string) (time.Duration, string) {
	if key == "" {
		return 0, ""
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	h, ok := l.hold[key]
	if !ok {
		return 0, ""
	}
	if !h.until.After(now) {
		delete(l.hold, key)
		return 0, ""
	}
	return h.until.Sub(now), h.msg
}

func (l *outboundLimiter) holdUntil(key string, d time.Duration, msg string) {
	if key == "" || d <= 0 {
		return
	}
	until := l.now().Add(d)
	l.mu.Lock()
	defer l.mu.Unlock()
	if cur, ok := l.hold[key]; ok && cur.until.After(until) {
		return
	}
	if strings.TrimSpace(msg) == "" {
		msg = rateLimitMessage(d)
	}
	l.hold[key] = destHold{until: until, msg: msg}
}

func (l *outboundLimiter) clear(key string) {
	if key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hold, key)
}

func (l *outboundLimiter) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hold = map[string]destHold{}
}

func retryDelayForError(err error, attempts int) time.Duration {
	next := backoff(attempts)
	if ra := retryAfterOf(err); ra > 0 {
		next = ra
		if next < time.Second {
			next = time.Second
		}
	}
	return next
}

func rateLimitMessage(d time.Duration) string {
	if d > 0 {
		return "Destination is rate-limited. Next retry in " + formatRetryAfter(d) + "."
	}
	return "Destination is rate-limited. Deliveries will retry automatically."
}
