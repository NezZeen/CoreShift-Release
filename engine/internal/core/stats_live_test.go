package core

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coreshift/engine/internal/node"
)

func TestParseVersion(t *testing.T) {
	for out, want := range map[string]string{
		"Xray 26.3.27 (Xray, Penetrates Everything.) d2758a0 (go1.26.1 windows/amd64)\nA unified platform": "26.3.27",
		"sing-box version 1.14.2\n\nEnvironment: go1.26":                                                   "1.14.2",
		"Mihomo Meta v1.19.31 windows amd64 with go1.26.8 Mon Sep 14 13:22:27 UTC 2026":                    "1.19.31",
		"sing-box version 1.15.0-beta.3":                                                                   "1.15.0-beta.3",
		"something else":                                                                                   "",
	} {
		if got := ParseVersion(out); got != want {
			t.Errorf("ParseVersion(%q) = %q, want %q", out, got, want)
		}
	}
}

func freePort(t *testing.T) netip.AddrPort {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return netip.MustParseAddrPort(l.Addr().String())
}

func waitPort(t *testing.T, ap netip.AddrPort) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if c, err := net.DialTimeout("tcp", ap.String(), 100*time.Millisecond); err == nil {
			c.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("nothing listens on %s", ap)
}

func startCore(t *testing.T, bin string, args []string, dir string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("%s output:\n%s", filepath.Base(bin), out.String())
		}
	})
}

// TestTrafficCounters runs each core against a local Shadowsocks server
// (sing-box) and checks that ReadTraffic sees a download through it, and
// that the SOCKS inbound takes only its credentials. Needs
// XRAY_BIN, SINGBOX_BIN and MIHOMO_BIN, like TestCoresAcceptConfigs.
func TestTrafficCounters(t *testing.T) {
	bins := map[Kind]string{Xray: os.Getenv("XRAY_BIN"), SingBox: os.Getenv("SINGBOX_BIN"), Mihomo: os.Getenv("MIHOMO_BIN")}
	if bins[SingBox] == "" {
		t.Skip("SINGBOX_BIN not set")
	}
	const size = 1 << 20
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, size))
	}))
	defer web.Close()

	ss := freePort(t)
	serverDir := t.TempDir()
	serverCfg, _ := json.Marshal(obj{
		"inbounds": []any{obj{"type": "shadowsocks", "listen": "127.0.0.1", "listen_port": ss.Port(),
			"method": "aes-128-gcm", "password": "test-password"}},
		"outbounds": []any{obj{"type": "direct"}},
	})
	if err := os.WriteFile(filepath.Join(serverDir, "server.json"), serverCfg, 0o600); err != nil {
		t.Fatal(err)
	}
	startCore(t, bins[SingBox], []string{"run", "-c", "server.json", "-D", serverDir}, serverDir)
	waitPort(t, ss)

	n := node.Node{Name: "local", Protocol: node.Shadowsocks, Server: "127.0.0.1", Port: ss.Port(),
		Cipher: "aes-128-gcm", Password: "test-password"}
	for _, a := range Adapters() {
		bin := bins[a.Kind()]
		t.Run(string(a.Kind()), func(t *testing.T) {
			if bin == "" {
				t.Skip("binary not set")
			}
			v, err := Version(context.Background(), a.Kind(), bin)
			if err != nil || v == "" {
				t.Fatalf("Version: %q, %v", v, err)
			}
			t.Logf("version %s", v)

			dir := t.TempDir()
			o := Options{Listen: freePort(t), Stats: freePort(t), StatsSecret: "s3cret", Auth: NewSOCKSAuth()}
			cfg, err := a.Render(&n, o)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, a.ConfigName())
			if err := os.WriteFile(path, cfg, 0o600); err != nil {
				t.Fatal(err)
			}
			startCore(t, bin, a.RunArgs(path, dir), dir)
			waitPort(t, o.Listen)
			waitPort(t, o.Stats)

			before, err := ReadTraffic(context.Background(), a.Kind(), o.Stats, o.StatsSecret)
			if err != nil {
				t.Fatalf("ReadTraffic before: %v", err)
			}
			// Without the credentials the inbound refuses.
			open := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
				Proxy: http.ProxyURL(&url.URL{Scheme: "socks5", Host: o.Listen.String()}),
			}}
			if resp, err := open.Get(web.URL); err == nil {
				resp.Body.Close()
				t.Fatal("the SOCKS inbound let a client in without credentials")
			}
			wrong := SOCKSAuth{User: o.Auth.User, Pass: "wrong"}
			if resp, err := (&http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(wrong.ProxyURL(o.Listen))}}).Get(web.URL); err == nil {
				resp.Body.Close()
				t.Fatal("the SOCKS inbound took a wrong password")
			}
			client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
				Proxy: http.ProxyURL(o.Auth.ProxyURL(o.Listen)),
			}}
			resp, err := client.Get(web.URL)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if got != size {
				t.Fatalf("downloaded %d bytes", got)
			}
			var after Traffic
			for i := 0; i < 30; i++ {
				if after, err = ReadTraffic(context.Background(), a.Kind(), o.Stats, o.StatsSecret); err == nil && after.Down-before.Down >= size {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if err != nil || after.Down-before.Down < size || after.Up <= before.Up {
				t.Fatalf("traffic %+v → %+v (%v), want at least %d down", before, after, err, size)
			}
			t.Logf("traffic %+v", after)

			if a.Kind() != Xray {
				// The secret must be required.
				if _, err := ReadTraffic(context.Background(), a.Kind(), o.Stats, "wrong"); err == nil {
					t.Error("stats readable with a wrong secret")
				}
			}
		})
	}
}
