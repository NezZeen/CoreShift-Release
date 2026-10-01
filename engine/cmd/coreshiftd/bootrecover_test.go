package main

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

type taskDoc struct {
	Triggers struct {
		Boot *struct{ Enabled string } `xml:"BootTrigger"`
	}
	Principal struct {
		UserID   string `xml:"UserId"`
		RunLevel string
	} `xml:"Principals>Principal"`
	Settings struct {
		DisallowStartIfOnBatteries string
		StopIfGoingOnBatteries     string
		ExecutionTimeLimit         string
	}
	Exec struct {
		Command   string
		Arguments string
	} `xml:"Actions>Exec"`
}

func parseTask(t *testing.T, text string) taskDoc {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(text))
	// The declaration says UTF-16, which the file has but this string does not.
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	var doc taskDoc
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("the task is not valid XML: %v\n%s", err, text)
	}
	return doc
}

func TestBootRecoveryTask(t *testing.T) {
	exe := `C:\Program Files\Core & Shift\coreshiftd.exe`
	doc := parseTask(t, bootRecoveryTaskXML(exe))
	if doc.Triggers.Boot == nil || doc.Triggers.Boot.Enabled != "true" {
		t.Error("the task does not run at boot")
	}
	if doc.Principal.UserID != "S-1-5-18" || doc.Principal.RunLevel != "HighestAvailable" {
		t.Errorf("principal = %+v, want SYSTEM with the highest rights", doc.Principal)
	}
	// A task made by schtasks flags would not start on battery power.
	if doc.Settings.DisallowStartIfOnBatteries != "false" || doc.Settings.StopIfGoingOnBatteries != "false" {
		t.Errorf("the task depends on the power source: %+v", doc.Settings)
	}
	if doc.Settings.ExecutionTimeLimit == "" {
		t.Error("the task has no time limit")
	}
	if doc.Exec.Command != exe || doc.Exec.Arguments != "dns recover" {
		t.Errorf("runs %q %q, want %q dns recover (the & must survive the XML)", doc.Exec.Command, doc.Exec.Arguments, exe)
	}
}

func TestUTF16File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.xml")
	text := "<a>Привет, CoreShift</a>\n"
	if err := utf16File(path, text); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 2 || b[0] != 0xFF || b[1] != 0xFE {
		t.Fatalf("no UTF-16 byte order mark: % x", b[:min(len(b), 4)])
	}
	units := make([]uint16, 0, len(b)/2)
	for i := 2; i+1 < len(b); i += 2 {
		units = append(units, uint16(b[i])|uint16(b[i+1])<<8)
	}
	if got := string(utf16.Decode(units)); got != text {
		t.Errorf("read back %q, want %q", got, text)
	}
}

func TestRotateLogKeepsTheLastRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coreshiftd.log")
	read := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			return "<none>"
		}
		return string(b)
	}
	// Five runs, each one starting the way the service does.
	for _, run := range []string{"one", "two", "three", "four", "five"} {
		rotateLog(path, 3)
		if err := os.WriteFile(path, []byte(run), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for suffix, want := range map[string]string{"": "five", ".1": "four", ".2": "three", ".3": "two"} {
		if got := read(path + suffix); got != want {
			t.Errorf("coreshiftd.log%s = %q, want %q", suffix, got, want)
		}
	}
	if got := read(path + ".4"); got != "<none>" {
		t.Errorf("a fourth copy is kept: %q", got)
	}
}

// A run that wrote nothing must not push a real log out.
func TestRotateLogSkipsAnEmptyLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coreshiftd.log")
	os.WriteFile(path, []byte("crash"), 0o600)
	rotateLog(path, 3)
	os.WriteFile(path, nil, 0o600) // started, wrote nothing
	rotateLog(path, 3)
	if b, _ := os.ReadFile(path + ".1"); string(b) != "crash" {
		t.Errorf("coreshiftd.log.1 = %q, want the log of the crashed run", b)
	}
}
