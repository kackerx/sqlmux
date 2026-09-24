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
