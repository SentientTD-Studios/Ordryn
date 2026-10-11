package mailer

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// ErrRateLimited is returned when core mail hits the per-recipient or site-wide cap.
var ErrRateLimited = errors.New("email rate limited")

const (
	coreRecipientLimit  = 5
	coreRecipientWindow = 15 * time.Minute
	coreGlobalLimit     = 60
	coreGlobalWindow    = 15 * time.Minute
)

type coreSendLimiter struct {
	mu          sync.Mutex
	byRecipient map[string][]time.Time
	global      []time.Time
	now         func() time.Time
	recipientN  int
	recipientW  time.Duration
	globalN     int
	globalW     time.Duration
}

var coreLimiter = newCoreSendLimiter()

// Notification mail has its own budget, separate from core (account) mail.
// Instant emails are already batched to at most one per user per few minutes;
// these caps only stop a runaway loop or a very busy site.
const (
	notificationRecipientLimit  = 12
	notificationRecipientWindow = time.Hour
	notificationGlobalLimit     = 300
	notificationGlobalWindow    = 15 * time.Minute
)

var notificationLimiter = &coreSendLimiter{
	byRecipient: map[string][]time.Time{},
	now:         time.Now,
	recipientN:  notificationRecipientLimit,
	recipientW:  notificationRecipientWindow,
	globalN:     notificationGlobalLimit,
	globalW:     notificationGlobalWindow,
}

func allowNotificationSend(toEmail string) error {
	return notificationLimiter.allow(toEmail)
}

func newCoreSendLimiter() *coreSendLimiter {
	return &coreSendLimiter{
		byRecipient: map[string][]time.Time{},
		now:         time.Now,
		recipientN:  coreRecipientLimit,
		recipientW:  coreRecipientWindow,
		globalN:     coreGlobalLimit,
		globalW:     coreGlobalWindow,
	}
}

func allowCoreSend(toEmail string) error {
	return coreLimiter.allow(toEmail)
}

func (l *coreSendLimiter) allow(toEmail string) error {
	now := l.now()
	key := strings.ToLower(strings.TrimSpace(toEmail))

	l.mu.Lock()
	defer l.mu.Unlock()

	l.global = pruneTimes(l.global, now, l.globalW)
	if key != "" {
		l.byRecipient[key] = pruneTimes(l.byRecipient[key], now, l.recipientW)
		if len(l.byRecipient[key]) >= l.recipientN {
			return ErrRateLimited
		}
	}
	if len(l.global) >= l.globalN {
		return ErrRateLimited
	}

	l.global = append(l.global, now)
	if key != "" {
		l.byRecipient[key] = append(l.byRecipient[key], now)
	}
	return nil
}

func (l *coreSendLimiter) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.byRecipient = map[string][]time.Time{}
	l.global = nil
}

func pruneTimes(in []time.Time, now time.Time, window time.Duration) []time.Time {
	if len(in) == 0 {
		return in
	}
	cutoff := now.Add(-window)
	i := 0
	for i < len(in) && !in[i].After(cutoff) {
		i++
	}
	if i == 0 {
		return in
	}
	out := append([]time.Time(nil), in[i:]...)
	return out
}
