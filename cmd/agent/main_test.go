package main

import "testing"

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		candidate string
		current   string
		want      bool
	}{
		{"0.2.1", "0.2.0", true},
		{"0.2.0", "0.2.0", false},
		{"0.1.9", "0.2.0", false},
		{"v0.10.0", "0.9.9", true},
		{"1.0", "1.0.0", false},
	}
	for _, tc := range cases {
		if got := versionNewer(tc.candidate, tc.current); got != tc.want {
			t.Fatalf("versionNewer(%q, %q) = %v, want %v", tc.candidate, tc.current, got, tc.want)
		}
	}
}
