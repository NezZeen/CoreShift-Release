package proc

import (
	"fmt"
	"slices"
	"testing"
)

func TestLineWriter(t *testing.T) {
	var got []string
	w := &lineWriter{onLine: func(s string) { got = append(got, s) }}
	fmt.Fprint(w, "first\nsec")
	fmt.Fprint(w, "ond\r\n\x1b[31mFATAL\x1b[0m bad config\n\npartial")
	w.flush()
	want := []string{"first", "second", "FATAL bad config", "partial"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripANSI(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[31mERROR\x1b[0m[1] x":      "ERROR[1] x",
		"\x1b[2K\x1b[1;32mok\x1b[m":      "ok",
		"[31mERROR [0m[1] x":             "ERROR [1] x",
		"[38;5;207m534559423 [0m 0ms] x": "534559423  0ms] x",
		"keep [a,b] and [3] and [m]":     "keep [a,b] and [3] and [m]",
		"plain":                          "plain",
	} {
		if got := StripANSI(in); got != want {
			t.Errorf("StripANSI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTailKeepsLastLines(t *testing.T) {
	tl := &tail{max: 3}
	for i := 1; i <= 5; i++ {
		tl.add(fmt.Sprint(i))
	}
	if got := tl.last(2); got != "4 | 5" {
		t.Errorf("last(2) = %q", got)
	}
	if got := tl.last(10); got != "3 | 4 | 5" {
		t.Errorf("last(10) = %q", got)
	}
}
