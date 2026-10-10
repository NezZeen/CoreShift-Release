package service

import (
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"coreshift/engine/internal/tunlayer"
)

// The TUN layer counts as up only once its interface has its address: an
// adapter left half registered by an earlier run may be listed without it.
func TestInterfaceHasItsAddress(t *testing.T) {
	ifcs, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, ifc := range ifcs {
		if ifc.Flags&net.FlagLoopback == 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			ip, _ := netip.AddrFromSlice(n.IP.To4())
			if !interfaceHas(ifc.Name, ip) {
				t.Errorf("%s has %s", ifc.Name, ip)
			}
			if interfaceHas(ifc.Name, netip.MustParseAddr("172.19.0.1")) {
				t.Errorf("%s does not have 172.19.0.1", ifc.Name)
			}
			if interfaceHas("no-such-interface", ip) {
				t.Error("a missing interface has an address")
			}
			return
		}
	}
	t.Skip("no loopback interface with IPv4")
}

// An updated sing-box checks the TUN layer's config before the running
// layer stops: a real sing-box takes it, and refuses one it cannot read.
// Set SINGBOX_BIN to the executable to run it.
func TestSingBoxChecksTUNConfig(t *testing.T) {
	bin := os.Getenv("SINGBOX_BIN")
	if bin == "" {
		t.Skip("SINGBOX_BIN not set")
	}
	dir := t.TempDir()
	tun := &singBoxTUN{bin: bin, dir: dir}
	o := tunlayer.Options{Upstream: netip.MustParseAddrPort("127.0.0.1:17890"), CacheFile: filepath.Join(dir, "cache.db"),
		DNS: tunlayer.DNSOptions{Remote: "https://1.1.1.1/dns-query", Direct: "192.168.1.1", FakeIP: true}}
	if err := tun.Check(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cache.db")); err == nil {
		t.Error("the check opened the cache the running layer holds")
	}
	if _, err := os.Stat(filepath.Join(dir, "tun-check.json")); err == nil {
		t.Error("the checked config is left behind")
	}
	tun.bin = fakeCore // not a sing-box: refuses the arguments
	if err := tun.Check(context.Background(), o); err == nil {
		t.Error("a core that refuses the config passes the check")
	}
}
