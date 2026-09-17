package live

import (
	"testing"

	"GoTodo/internal/hooks"
)

func TestAfterJoinReviewedDispatches(t *testing.T) {
	var got []string
	stop := ListenHooks(func(ev hooks.Event) {
		got = append(got, ev.Type)
		if ev.JoinEmail != "new@example.com" {
			t.Errorf("email=%q", ev.JoinEmail)
		}
	})
	defer stop()

	AfterJoinReviewed("new@example.com", "please", true)
	AfterJoinReviewed("new@example.com", "nope", false)

	if len(got) != 2 || got[0] != TypeJoinApproved || got[1] != TypeJoinDenied {
		t.Fatalf("got %v", got)
	}
}
