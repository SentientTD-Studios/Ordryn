package utils

import "testing"

func TestLoginFailureCounters(t *testing.T) {
	got := loginFailureCounters(" Alice@Example.com ", "203.0.113.9")
	want := []failureCounter{
		{"rl:fail:email-ip:alice@example.com|203.0.113.9", LoginFailuresPerEmailAndIP},
		{"rl:fail:email:alice@example.com", LoginFailuresPerEmail},
		{"rl:fail:ip:203.0.113.9", LoginFailuresPerIP},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d counters, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("counter %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// Someone guessing from another IP uses a different email-and-IP counter, so
	// the owner's own counter (the one that locks after 5) is untouched.
	other := loginFailureCounters("alice@example.com", "198.51.100.1")
	if other[0].key == got[0].key {
		t.Fatal("email-and-IP counters must differ per IP")
	}
	if LoginFailuresPerEmail <= LoginFailuresPerEmailAndIP {
		t.Fatal("the per-email backstop must be looser than the email-and-IP lockout")
	}

	if c := loginFailureCounters("", "203.0.113.9"); len(c) != 1 || c[0].limit != LoginFailuresPerIP {
		t.Fatalf("without an email only the IP counter applies: %+v", c)
	}
}
