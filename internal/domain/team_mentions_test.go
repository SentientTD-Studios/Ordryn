package domain

import "testing"

func TestTeamMentionHandle(t *testing.T) {
	cases := map[string]string{
		"Design Team":    "design_team",
		"  QA  ":         "qa",
		"Front-End / UI": "front_end_ui",
		"__ops__":        "ops",
		"!!!":            "",
	}
	for in, want := range cases {
		if got := TeamMentionHandle(in); got != want {
			t.Errorf("TeamMentionHandle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTeamMentionMembersEmptyNames(t *testing.T) {
	if got := teamMentionMembers(1, nil, nil); got != nil {
		t.Fatalf("expected nil for no names, got %v", got)
	}
}
