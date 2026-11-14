// Package workers records what the background loops (purges, reminders,
// automation, webhook delivery, backups, ...) are doing, so an admin can see
// one that has stopped running, is stuck mid-pass, or keeps failing.
//
// It depends only on the standard library so storage, hooks and domain can
// all report into it without import cycles.
package workers

import (
	"fmt"
	"log"
	"runtime/debug"
	"sort"
	"sync"
	"time"
)

// Status values reported in Snapshot.Status.
const (
	StatusPending = "pending" // registered, hasn't run yet
	StatusOK      = "ok"
	StatusRunning = "running"
	StatusError   = "error" // last pass failed
	StatusLate    = "late"  // hasn't started a pass for well over its interval
	StatusStuck   = "stuck" // current pass has run far longer than its interval
)

type worker struct {
	name         string
	interval     time.Duration
	runs         int64
	failures     int64
	running      bool
	lastStart    time.Time
	lastFinish   time.Time
	lastDuration time.Duration
	lastError    string
	lastErrorAt  time.Time
}

var (
	mu       sync.Mutex
	registry = map[string]*worker{}
)

// Register declares a worker and how often it is expected to run. Calling it
// again updates the interval (useful when the schedule comes from config).
func Register(name string, interval time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	w := registry[name]
	if w == nil {
		w = &worker{name: name}
		registry[name] = w
	}
	w.interval = interval
}

// Run executes one pass of the named worker and records its timing and
// result. A panic is recovered and recorded as a failure, so one bad pass
// doesn't take the loop (or the whole process) down with it.
func Run(name string, fn func() error) {
	mu.Lock()
	w := registry[name]
	if w == nil {
		w = &worker{name: name}
		registry[name] = w
	}
	w.running = true
	w.lastStart = time.Now()
	mu.Unlock()

	var err error
	func() {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
				log.Printf("worker %s: panic: %v\n%s", name, p, debug.Stack())
			}
		}()
		err = fn()
	}()

	mu.Lock()
	defer mu.Unlock()
	now := time.Now()
	w.running = false
	w.runs++
	w.lastFinish = now
	w.lastDuration = now.Sub(w.lastStart)
	if err != nil {
		w.failures++
		w.lastError = err.Error()
		w.lastErrorAt = now
	} else {
		w.lastError = ""
	}
}

// Snapshot is one worker's state at the time Snapshots was called.
type Snapshot struct {
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	IntervalSeconds int64      `json:"interval_seconds"`
	Runs            int64      `json:"runs"`
	Failures        int64      `json:"failures"`
	Running         bool       `json:"running"`
	LastStartedAt   *time.Time `json:"last_started_at"`
	LastFinishedAt  *time.Time `json:"last_finished_at"`
	LastDurationMS  float64    `json:"last_duration_ms"`
	LastError       string     `json:"last_error,omitempty"`
	LastErrorAt     *time.Time `json:"last_error_at,omitempty"`
}

// Snapshots returns every registered worker, sorted by name.
func Snapshots() []Snapshot {
	return snapshotsAt(time.Now())
}

func snapshotsAt(now time.Time) []Snapshot {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Snapshot, 0, len(registry))
	for _, w := range registry {
		s := Snapshot{
			Name:            w.name,
			Status:          status(w, now),
			IntervalSeconds: int64(w.interval / time.Second),
			Runs:            w.runs,
			Failures:        w.failures,
			Running:         w.running,
			LastStartedAt:   timePtr(w.lastStart),
			LastFinishedAt:  timePtr(w.lastFinish),
			LastDurationMS:  float64(w.lastDuration.Microseconds()) / 1000,
			LastError:       w.lastError,
		}
		if w.lastError != "" {
			s.LastErrorAt = timePtr(w.lastErrorAt)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// grace is how far past its interval a worker may drift before it counts as
// late or stuck: twice the interval, and never less than five minutes.
func grace(interval time.Duration) time.Duration {
	g := 2 * interval
	if g < 5*time.Minute {
		g = 5 * time.Minute
	}
	return g
}

func status(w *worker, now time.Time) string {
	if w.running {
		if w.interval > 0 && now.Sub(w.lastStart) > grace(w.interval) {
			return StatusStuck
		}
		return StatusRunning
	}
	if w.lastStart.IsZero() {
		if w.interval > 0 && now.Sub(processStart) > grace(w.interval) {
			return StatusLate
		}
		return StatusPending
	}
	if w.interval > 0 && now.Sub(w.lastStart) > grace(w.interval) {
		return StatusLate
	}
	if w.lastError != "" {
		return StatusError
	}
	return StatusOK
}

var processStart = time.Now()

// ProcessStart is when this process loaded the package, close enough to the
// server's start time for uptime reporting.
func ProcessStart() time.Time { return processStart }

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

// resetForTests clears the registry.
func resetForTests() {
	mu.Lock()
	registry = map[string]*worker{}
	mu.Unlock()
}
