package workers

import (
	"errors"
	"testing"
	"time"
)

func find(t *testing.T, name string, now time.Time) Snapshot {
	t.Helper()
	for _, s := range snapshotsAt(now) {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("worker %q not found", name)
	return Snapshot{}
}

func TestRunRecordsSuccessAndFailure(t *testing.T) {
	resetForTests()
	Register("w", time.Hour)
	if s := find(t, "w", time.Now()); s.Status != StatusPending {
		t.Fatalf("before first run: status %q, want pending", s.Status)
	}

	Run("w", func() error { return errors.New("boom") })
	s := find(t, "w", time.Now())
	if s.Status != StatusError || s.LastError != "boom" || s.Failures != 1 || s.Runs != 1 {
		t.Fatalf("after failure: %+v", s)
	}

	Run("w", func() error { return nil })
	s = find(t, "w", time.Now())
	if s.Status != StatusOK || s.LastError != "" || s.Failures != 1 || s.Runs != 2 {
		t.Fatalf("after recovery: %+v", s)
	}
}

func TestRunRecoversPanic(t *testing.T) {
	resetForTests()
	Run("p", func() error { panic("kaboom") })
	s := find(t, "p", time.Now())
	if s.Running || s.Failures != 1 || s.LastError != "panic: kaboom" {
		t.Fatalf("after panic: %+v", s)
	}
}

func TestLateAndStuck(t *testing.T) {
	resetForTests()
	Register("hourly", time.Hour)
	Run("hourly", func() error { return nil })
	if s := find(t, "hourly", time.Now().Add(90*time.Minute)); s.Status != StatusOK {
		t.Fatalf("90m after an hourly run: %q, want ok", s.Status)
	}
	if s := find(t, "hourly", time.Now().Add(3*time.Hour)); s.Status != StatusLate {
		t.Fatalf("3h after an hourly run: %q, want late", s.Status)
	}

	Register("stuck", time.Minute)
	started := make(chan struct{})
	release := make(chan struct{})
	go Run("stuck", func() error { close(started); <-release; return nil })
	<-started
	if s := find(t, "stuck", time.Now()); s.Status != StatusRunning {
		t.Fatalf("mid-run: %q, want running", s.Status)
	}
	if s := find(t, "stuck", time.Now().Add(10*time.Minute)); s.Status != StatusStuck {
		t.Fatalf("10m into a 1m worker's pass: %q, want stuck", s.Status)
	}
	close(release)
}
