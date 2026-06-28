package installer

import "testing"

func TestDependencyNames(t *testing.T) {
	got := dependencyNames("libc6 (>= 2.34), libssl3 | libssl1.1, zlib1g")
	want := []string{"libc6", "libssl3", "zlib1g"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("got[%d] = %q, want %q", idx, got[idx], want[idx])
		}
	}
}
