package handlers

import "testing"

func TestSortChangelogEntriesBySemverDescending(t *testing.T) {
	entries := []ChangelogEntry{
		{Version: "v0.18.0-beta", Date: "2026-07-13"},
		{Version: "v0.17.0-beta", Date: "2026-07-07"},
		{Version: "v0.18.1-beta", Date: "2026-07-13"},
		{Version: "v0.16.1-beta", Date: "2026-07-06"},
	}
	sortChangelogEntries(entries)

	want := []string{"v0.18.1-beta", "v0.18.0-beta", "v0.17.0-beta", "v0.16.1-beta"}
	for i, version := range want {
		if entries[i].Version != version {
			t.Fatalf("entries[%d].Version = %q, want %q", i, entries[i].Version, version)
		}
	}
}

func TestSortChangelogEntriesUsesDateWhenSemverEqual(t *testing.T) {
	entries := []ChangelogEntry{
		{Version: "v1.0.0", Date: "2026-01-01"},
		{Version: "v1.0.0", Date: "2026-02-01"},
	}
	sortChangelogEntries(entries)

	if entries[0].Date != "2026-02-01" || entries[1].Date != "2026-01-01" {
		t.Fatalf("unexpected order: %+v", entries)
	}
}

func TestPrepareChangelogEntriesFiltersAndSorts(t *testing.T) {
	entries := []ChangelogEntry{
		{Version: "v0.18.0-beta", Date: "2026-07-13"},
		{Version: "v0.19.0-beta", Date: "2026-07-14"},
		{Version: "v0.18.1-beta", Date: "2026-07-13"},
	}
	out := prepareChangelogEntries(entries, "v0.18.1-beta")

	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
	if out[0].Version != "v0.18.1-beta" || out[1].Version != "v0.18.0-beta" {
		t.Fatalf("unexpected order: %+v", out)
	}
}
