package tasks

import "testing"

func TestNormalizeDueFilter(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"overdue", "overdue"},
		{"TODAY", "today"},
		{" week ", "week"},
		{"none", "none"},
		{"invalid", ""},
		{"", ""},
	}
	for _, tc := range tests {
		if got := NormalizeDueFilter(tc.in); got != tc.want {
			t.Errorf("NormalizeDueFilter(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAppendDueDateCondition(t *testing.T) {
	where, args := appendDueDateCondition("user_id = $1", []interface{}{1}, "overdue", "America/New_York", "t")
	if where == "user_id = $1" {
		t.Fatal("expected overdue condition to be appended")
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
	if args[1] != "America/New_York" {
		t.Fatalf("timezone arg = %v", args[1])
	}

	where2, args2 := appendDueDateCondition("x = 1", nil, "", "UTC", "")
	if where2 != "x = 1" || len(args2) != 0 {
		t.Fatalf("empty filter should be unchanged, got %q %v", where2, args2)
	}
}
