package storage

import "testing"

func TestClampTaskTextLength(t *testing.T) {
	tests := []struct{ in, want int }{
		{in: 0, want: DefaultTaskTextLength},
		{in: -5, want: DefaultTaskTextLength},
		{in: 10, want: MinTaskTextLength},
		{in: MinTaskTextLength, want: MinTaskTextLength},
		{in: 20000, want: 20000},
		{in: MaxTaskTextLengthCap, want: MaxTaskTextLengthCap},
		{in: MaxTaskTextLengthCap + 1, want: MaxTaskTextLengthCap},
	}
	for _, tt := range tests {
		if got := ClampTaskTextLength(tt.in); got != tt.want {
			t.Fatalf("ClampTaskTextLength(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestSiteSettingsTaskTextLimits(t *testing.T) {
	var nilSettings *SiteSettings
	if got := nilSettings.TaskTextLimits(); got != DefaultTaskTextLimits() {
		t.Fatalf("nil settings = %+v", got)
	}
	s := &SiteSettings{MaxDescriptionLength: 30000, MaxCommentLength: 0}
	got := s.TaskTextLimits()
	if got.Description != 30000 || got.Comment != DefaultTaskTextLength {
		t.Fatalf("got %+v", got)
	}
}
