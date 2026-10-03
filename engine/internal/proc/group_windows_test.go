package proc

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procIsProcessInJob = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")

func inJob(t *testing.T, pid int, job windows.Handle) bool {
	t.Helper()
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	var in int32
	if r, _, err := procIsProcessInJob.Call(uintptr(h), uintptr(job), uintptr(unsafe.Pointer(&in))); r == 0 {
		t.Fatal(err)
	}
	return in != 0
}

// The process is in the job from its first instruction on: it starts
// suspended, is assigned, and only then runs.
func TestStartRunsTheProcessInTheJob(t *testing.T) {
	g, err := NewGroup()
	if err != nil {
		t.Fatal(err)
	}
	var started atomic.Bool
	cmdExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	p, err := g.Start(Spec{Name: "cmd", Path: cmdExe, Args: []string{"/c", "echo started & ping -n 3 127.0.0.1 >nul"},
		OnLine: func(l string) {
			if l == "started" {
				started.Store(true)
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	if !inJob(t, p.cmd.Process.Pid, g.g.job) {
		t.Error("the process is not in the job")
	}
	// And it runs: it was resumed.
	if err := p.WaitFor(context.Background(), 10*time.Second, "output", started.Load); err != nil {
		t.Fatal(err)
	}
}
