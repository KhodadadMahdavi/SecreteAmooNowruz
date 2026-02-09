package db

import "testing"

func TestDerangedCopy(t *testing.T) {
	t.Helper()

	source := []int64{1, 2, 3, 4, 5}
	out, err := derangedCopy(source)
	if err != nil {
		t.Fatalf("derangedCopy() error = %v", err)
	}
	if len(out) != len(source) {
		t.Fatalf("len(out) = %d, want %d", len(out), len(source))
	}
	if !isDerangement(source, out) {
		t.Fatalf("output is not a derangement: source=%v out=%v", source, out)
	}

	seen := make(map[int64]int, len(out))
	for _, v := range out {
		seen[v]++
	}
	for _, v := range source {
		if seen[v] != 1 {
			t.Fatalf("value %d count = %d, want 1", v, seen[v])
		}
	}
}

func TestDerangedCopyNeedsAtLeastTwo(t *testing.T) {
	t.Helper()

	_, err := derangedCopy([]int64{1})
	if err == nil {
		t.Fatal("derangedCopy() error = nil, want error")
	}
}
