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
