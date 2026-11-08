package ui

import "testing"

func TestMagnitude(t *testing.T) {
	for n, want := range map[float64]string{
		-1: "?", 0: "0", 999: "999", 1000: "1.0k", 8199: "8.1k", 9999: "9.9k", 10000: "10k",
		48e3: "48k", 999999: "999k", 2.19e6: "2.1m", 12e6: "12m", 3.4e9: "3.4b", 1e12: "1000b",
	} {
		if got := Magnitude(n); got != want {
			t.Errorf("Magnitude(%v) = %q, want %q", n, got, want)
		}
	}
}

// A size as it is read, by 1024s, a tenth below ten of a unit; none unknown.
func TestSize(t *testing.T) {
	for n, want := range map[int64]string{0: "", -1: "", 512: "512 B", 1024: "1 KB", 1536: "1.5 KB", 65536: "64 KB", 3_565_158: "3.4 MB", 1 << 30: "1 GB", 15 << 30: "15 GB"} {
		if got := Size(n); got != want {
			t.Errorf("Size(%d) = %q, want %q", n, got, want)
		}
	}
}
