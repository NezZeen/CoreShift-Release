//go:build linux && !android

package tunlayer

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestRefuseIPv6Live runs the layer with IPv6 refused on a real TUN and
// checks what apps see. It changes the routes of the network namespace it
// runs in, so run it as root inside a throwaway one that has IPv6 of its
// own: a default route to REFUSE_GLOBAL6 (an "internet" host) and
// REFUSE_LAN6 in fd00::/8, both listening. testdata/refuse-ipv6-netns.sh
// builds such a namespace pair and runs this test there.
func TestRefuseIPv6Live(t *testing.T) {
	bin, global, lan := os.Getenv("SINGBOX_BIN"), os.Getenv("REFUSE_GLOBAL6"), os.Getenv("REFUSE_LAN6")
	if bin == "" || global == "" || lan == "" {
		t.Skip("SINGBOX_BIN, REFUSE_GLOBAL6 and REFUSE_LAN6 not set")
	}
	// Without the tunnel the IPv6 host answers: the way a leak would go.
	if c, err := net.DialTimeout("tcp", global, 3*time.Second); err != nil {
		t.Fatalf("the IPv6 host must answer before the TUN is up: %v", err)
	} else {
		c.Close()
	}

	o := Options{
		StrictRoute: true,
		RefuseIPv6:  true,
		ExcludeLAN:  true,
		Upstream:    netip.MustParseAddrPort("127.0.0.1:9"), // nothing goes there in this test
		DNS: DNSOptions{
			Remote:        "1.1.1.1",
			Direct:        "192.0.2.53",
			FakeIP:        true,
			BlockSuffixes: []string{"blocked.test"},
		},
	}
	cfg, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "tun.json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "-c", path, "-D", dir)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Signal(syscall.SIGTERM)
		cmd.Wait()
	})
	waitAddr(t, DefaultInterface, DefaultAddress6.Addr())
	time.Sleep(500 * time.Millisecond) // routes and rules follow the address

	// Refused at once, every time: a reset before any handshake, so apps
	// fall back to IPv4 without waiting. Past sing-box's 50 refusals in
	// 30 s, too (no_drop).
	var slowest time.Duration
	defer func() { t.Logf("slowest TCP refusal: %v", slowest) }()
	for i := range 60 {
		start := time.Now()
		c, err := net.DialTimeout("tcp", global, 5*time.Second)
		took := time.Since(start)
		slowest = max(slowest, took)
		if err == nil {
			c.Close()
			t.Fatalf("attempt %d: IPv6 connection made with IPv6 refused", i)
		}
		if !errors.Is(err, syscall.ECONNREFUSED) || took > 500*time.Millisecond {
			t.Fatalf("attempt %d: %v after %v, want connection refused at once", i, err, took)
		}
	}

	// UDP: the first packet gets an ICMP unreachable, which a connected
	// socket reports as refused.
	if c, err := net.Dial("udp", global); err == nil {
		start := time.Now()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		c.Write([]byte("x"))
		_, err = c.Read(make([]byte, 16))
		t.Logf("UDP to %s: %v after %v", global, err, time.Since(start))
		c.Close()
	}

	// The local network keeps its own routes.
	if c, err := net.DialTimeout("tcp", lan, 3*time.Second); err != nil {
		t.Errorf("local IPv6 host: %v", err)
	} else {
		c.Close()
	}

	// DNS sent over IPv6 is answered, not refused.
	m := new(dns.Msg)
	m.SetQuestion("ads.blocked.test.", dns.TypeA)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, server := range []string{netip.AddrPortFrom(DNSAddress(DefaultAddress6), 53).String(), "[2001:4860:4860::8888]:53"} {
		r, _, err := (&dns.Client{Timeout: 3 * time.Second}).ExchangeContext(ctx, m, server)
		if err != nil || r.Rcode != dns.RcodeNameError {
			t.Errorf("DNS over IPv6 to %s: %v, %v", server, r, err)
		}
	}
}

func waitAddr(t *testing.T, name string, addr netip.Addr) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ifc, err := net.InterfaceByName(name); err == nil {
			addrs, _ := ifc.Addrs()
			for _, a := range addrs {
				if n, ok := a.(*net.IPNet); ok {
					if ip, ok := netip.AddrFromSlice(n.IP); ok && ip.Unmap() == addr {
						return
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("interface %s never got %v", name, addr)
}
