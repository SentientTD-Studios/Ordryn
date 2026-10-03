package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestNormalizeAPIKeyScopes(t *testing.T) {
	got, err := NormalizeAPIKeyScopes([]string{" comments:write", "TASKS:READ", "tasks:read", ""})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tasks:read", "comments:write"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes=%v want %v", got, want)
	}
	for _, bad := range [][]string{nil, {}, {" "}, {"tasks:delete"}, {"tasks:read", "admin"}} {
		if _, err := NormalizeAPIKeyScopes(bad); !errors.Is(err, ErrValidation) {
			t.Errorf("%v: err=%v want validation", bad, err)
		}
	}
}

func TestValidateAPIKeyExpiry(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	ok := []*time.Time{nil, ptrTime(now.Add(time.Hour)), ptrTime(now.Add(MaxProjectAPIKeyLifetime))}
	for _, exp := range ok {
		if err := validateAPIKeyExpiry(exp, now); err != nil {
			t.Errorf("%v: unexpected %v", exp, err)
		}
	}
	bad := []*time.Time{ptrTime(now), ptrTime(now.Add(-time.Hour)), ptrTime(now.Add(MaxProjectAPIKeyLifetime + time.Hour))}
	for _, exp := range bad {
		if err := validateAPIKeyExpiry(exp, now); !errors.Is(err, ErrValidation) {
			t.Errorf("%v: err=%v want validation", exp, err)
		}
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
