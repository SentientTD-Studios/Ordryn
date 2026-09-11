package version

import (
	"runtime/debug"
	"testing"
)

func TestVersionDefaultIsDev(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must be non-empty")
	}
	if Version != devVersion && Version[0] != 'v' {
		t.Fatalf("Version = %q, want %q or a tagged module version", Version, devVersion)
	}
}

func TestVersionFromBuildInfo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{name: "nil", want: ""},
		{name: "empty", info: &debug.BuildInfo{}, want: ""},
		{name: "devel", info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, want: ""},
		{name: "tagged", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, want: "v1.2.3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := versionFromBuildInfo(tc.info); got != tc.want {
				t.Fatalf("versionFromBuildInfo() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseGitDescribeOutput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"v3.22.0\n", "v3.22.0"},
		{"3.22.0", "v3.22.0"},
		{"", ""},
		{"v3.22.0 extra", ""},
		{"main", ""},
		{"vdev", ""},
	}
	for _, tc := range cases {
		if got := parseGitDescribeOutput([]byte(tc.in)); got != tc.want {
			t.Fatalf("parseGitDescribeOutput(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
