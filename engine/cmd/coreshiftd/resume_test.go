package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResume(t *testing.T) {
	boot := []byte("6b0c7f1e-0d6a-4c1e-9a52-1f1d6f3c2a10")
	now := time.Now()
	for _, tc := range []struct {
		name string
		boot []byte // when it starts again
		age  time.Duration
		want bool
	}{
		{"restart", boot, time.Second, true},
		{"after a reboot", []byte("another boot"), time.Second, false},
		{"long after", boot, resumeMaxAge + time.Minute, false},
		{"unknown boot", nil, time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "resume")
			if err := writeResume(path, boot); err != nil {
				t.Fatal(err)
			}
			at := now.Add(-tc.age)
			os.Chtimes(path, at, at)
			if got := takeResume(path, tc.boot, now); got != tc.want {
				t.Errorf("takeResume = %v, want %v", got, tc.want)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Error("the resume file outlived its start")
			}
			if takeResume(path, tc.boot, now) {
				t.Error("one resume file resumed twice")
			}
		})
	}
	// Without a boot id nothing is written, so a reboot never resumes.
	path := filepath.Join(t.TempDir(), "resume")
	if err := writeResume(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a resume file without a boot id")
	}
}
