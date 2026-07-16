package utils

import "testing"

func TestIsValidTimezone(t *testing.T) {
	if !IsValidTimezone("America/New_York") {
		t.Fatal("expected valid")
	}
	if IsValidTimezone("Invalid/Zone") {
		t.Fatal("expected invalid")
	}
}

func TestValidItemsPerPage(t *testing.T) {
	if !ValidItemsPerPage(25) {
		t.Fatal("25 should be valid")
	}
	if ValidItemsPerPage(99) {
		t.Fatal("99 should be invalid")
	}
}
