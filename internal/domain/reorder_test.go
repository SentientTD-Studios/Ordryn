package domain

import "testing"

func TestApplyRelativeReorder(t *testing.T) {
	all := []int{10, 20, 30, 40, 50}
	ordered := []int{40, 20}
	got, err := ApplyRelativeReorder(all, ordered)
	if err != nil {
		t.Fatal(err)
	}
	// slots of 20 and 40 in all are indexes 1 and 3; sorted slots [1,3] get [40,20]
	want := []int{10, 40, 30, 20, 50}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestApplyRelativeReorderDuplicate(t *testing.T) {
	_, err := ApplyRelativeReorder([]int{1, 2}, []int{1, 1})
	if err == nil {
		t.Fatal("expected error")
	}
}
