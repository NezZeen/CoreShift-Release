package proc

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func startGraceful(t *testing.T, spec Spec, ready func() bool) *Process {
	t.Helper()
	g, err := NewGroup()
	if err != nil {
		t.Fatal(err)
	}
	spec.Graceful = true
	p, err := g.Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Stop)
	if err := p.WaitFor(context.Background(), 10*time.Second, "ready", ready); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGracefulStopLetsProcessCleanUp(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, exe("graceful"))
	if out, err := exec.Command("go", "build", "-o", bin, "./testdata/graceful").CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, out)
	}
	marker := filepath.Join(dir, "cleaned")
	readyCh := make(chan struct{})
	p := startGraceful(t, Spec{Name: "graceful", Path: bin, Args: []string{marker}, OnLine: func(l string) {
		if l == "ready" {
			close(readyCh)
		}
	}}, func() bool {
		select {
		case <-readyCh:
			return true
		default:
			return false
		}
	})
	start := time.Now()
	p.Stop()
	if b, err := os.ReadFile(marker); err != nil || string(b) != "clean" {
		t.Fatalf("process did not clean up (%v), stopped after %s: %v", err, time.Since(start), p.ExitError())
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("graceful stop took %s", time.Since(start))
	}
}

// TestSingBoxStopsGracefully checks the real TUN-layer binary exits by
// itself, with status 0, rather than being killed. SINGBOX_BIN enables it.
func TestSingBoxStopsGracefully(t *testing.T) {
	bin := os.Getenv("SINGBOX_BIN")
	if bin == "" {
		t.Skip("SINGBOX_BIN not set")
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	os.WriteFile(cfg, []byte(fmt.Sprintf(`{"inbounds":[{"type":"mixed","listen":"127.0.0.1","listen_port":%d}]}`, port)), 0o644)
	var lines []string
	p := startGraceful(t, Spec{Name: "sing-box", Path: bin, Args: []string{"run", "-c", cfg}, Dir: dir,
		OnLine: func(l string) { lines = append(lines, l) }},
		func() bool { return PortOpen(netip.MustParseAddrPort(fmt.Sprintf("127.0.0.1:%d", port))) })
	start := time.Now()
	p.Stop()
	if p.err != nil || time.Since(start) >= gracefulTimeout {
		t.Errorf("sing-box did not exit by itself: %v after %s\n%s", p.err, time.Since(start), strings.Join(lines, "\n"))
	}
}
