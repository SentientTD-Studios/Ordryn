package hooks

import (
	"strings"
	"testing"
)

func TestInterpolateTokens(t *testing.T) {
	got := Interpolate("Task {name} updated to {status} in project {project}", map[string]string{
		"name":    "Ship hooks",
		"status":  "Done",
		"project": "Ordryn",
	})
	if got != "Task Ship hooks updated to Done in project Ordryn" {
		t.Fatalf("got %q", got)
	}
}

func TestInterpolateUnknownAndAlias(t *testing.T) {
	got := Interpolate("{task} {missing} {id}", map[string]string{"task": "A", "id": "9"})
	if got != "A  9" {
		t.Fatalf("got %q", got)
	}
}

func TestStripBroadcastMentions(t *testing.T) {
	got := stripBroadcastMentions("hello @everyone and @here now")
	if strings.Contains(strings.ToLower(got), "@everyone") || strings.Contains(strings.ToLower(got), "@here") {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateTitle(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := truncateTitle(long)
	if got == long || !strings.HasSuffix(got, "…") {
		t.Fatalf("got len=%d", len(got))
	}
}
