package tunlayer

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"
	singservice "github.com/sagernet/sing/service"

	mDNS "github.com/miekg/dns"
)

// These tests run sing-box's own DNS router, in this process, on the DNS
// section of a generated configuration (no TUN, so no rights are needed),
// and ask it names: whether the chain of resolvers (directChain) behaves the
// way sing-box's docs and sources say it does, which the configuration
// check alone cannot show.

// fakeDNS is a resolver on loopback that answers every A query with addr,
// or, with silent, reads and says nothing; queries counts what reached it.
type fakeDNS struct {
	port    uint16
	queries atomic.Int64
}

func startFakeDNS(t *testing.T, host, addr string, rcode int, silent bool) *fakeDNS {
	t.Helper()
	pc, err := net.ListenPacket("udp", host+":0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeDNS{port: uint16(pc.LocalAddr().(*net.UDPAddr).Port)}
	srv := &mDNS.Server{PacketConn: pc, Handler: mDNS.HandlerFunc(func(w mDNS.ResponseWriter, r *mDNS.Msg) {
		f.queries.Add(1)
		if silent {
			return
		}
		m := new(mDNS.Msg)
		m.SetReply(r)
		m.Rcode = rcode
		if rcode == mDNS.RcodeSuccess && r.Question[0].Qtype == mDNS.TypeA {
			m.Answer = []mDNS.RR{&mDNS.A{
				Hdr: mDNS.RR_Header{Name: r.Question[0].Name, Rrtype: mDNS.TypeA, Class: mDNS.ClassINET, Ttl: 60},
				A:   net.ParseIP(addr),
			}}
		}
		w.WriteMsg(m)
	})}
	started := make(chan struct{})
	srv.NotifyStartedFunc = func() { close(started) }
	go srv.ActivateAndServe()
	<-started
	t.Cleanup(func() { srv.Shutdown() })
	return f
}

// refusedPort is a TCP port nothing listens on: dialling it is refused at
// once, as a provider's reset of a blocked resolver is.
func refusedPort(t *testing.T) uint16 {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(l.Addr().(*net.TCPAddr).Port)
	l.Close()
	return port
}

// journal is sing-box's log, in a file.
type journal string

func (j journal) text() string {
	b, _ := os.ReadFile(string(j))
	return string(b)
}

// startRouter runs a sing-box with the DNS section of o, whose servers'
// ports are set by ports (tag to port; all of them on loopback), and
// returns its DNS router.
func startRouter(t *testing.T, o Options, ports map[string]uint16) (adapter.DNSRouter, journal) {
	t.Helper()
	o.Upstream = netip.MustParseAddrPort("127.0.0.1:9")
	o = o.withDefaults()
	dns, err := buildDNS(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range dns["servers"].([]any) {
		srv := s.(obj)
		if p, ok := ports[srv["tag"].(string)]; ok {
			srv["server_port"] = p
		}
	}
	logFile := filepath.Join(t.TempDir(), "sing-box.log")
	cfg, err := json.Marshal(obj{
		"log":       obj{"level": "warn", "output": logFile},
		"dns":       dns,
		"outbounds": []any{obj{"type": "direct", "tag": tagDirect}, obj{"type": "socks", "tag": tagProxy, "server": "127.0.0.1", "server_port": 9}},
		"route":     obj{"final": tagDirect, "default_domain_resolver": tagDNSDirect},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := include.Context(context.Background())
	options, err := sjson.UnmarshalExtendedContext[option.Options](ctx, cfg)
	if err != nil {
		t.Fatalf("%v\n%s", err, cfg)
	}
	instance, err := box.New(box.Options{Context: ctx, Options: options})
	if err != nil {
		t.Fatalf("%v\n%s", err, cfg)
	}
	if err := instance.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { instance.Close() })
	router := singservice.FromContext[adapter.DNSRouter](ctx)
	if router == nil {
		t.Fatal("no DNS router")
	}
	return router, journal(logFile)
}

// lookup asks the router for name's A record and returns the answer's code
// and address.
func lookup(t *testing.T, r adapter.DNSRouter, name string) (rcode int, addr string, took time.Duration) {
	t.Helper()
	q := new(mDNS.Msg)
	q.SetQuestion(mDNS.Fqdn(name), mDNS.TypeA)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := time.Now()
	resp, err := r.Exchange(ctx, q, adapter.DNSQueryOptions{})
	took = time.Since(start)
	if err != nil {
		return -1, err.Error(), took
	}
	for _, rr := range resp.Answer {
		if a, ok := rr.(*mDNS.A); ok {
			addr = a.A.String()
		}
	}
	return resp.Rcode, addr, took
}

func chainOptions(directTCPPort uint16, selective bool) Options {
	o := baseOptions()
	o.Selective = selective
	o.DNS.FakeIP = false
	o.DNS.Direct = "tcp://127.0.0.1:" + itoaPort(directTCPPort)
	o.DNS.DirectFallback = []netip.Addr{netip.MustParseAddr("127.0.0.2")}
	if !selective {
		o.DNS.DirectSuffixes = []string{"direct.test"}
	}
	return o
}

func itoaPort(p uint16) string { return strconv.Itoa(int(p)) }

// A direct resolver that refuses connections (a provider that resets a
// blocked resolver): the names it should have answered come from the
// network's, at once.
func TestChainFallsBackWhenDirectIsRefused(t *testing.T) {
	network := startFakeDNS(t, "127.0.0.2", "192.0.2.53", mDNS.RcodeSuccess, false)
	directPort := refusedPort(t)
	router, j := startRouter(t, chainOptions(directPort, true), map[string]uint16{"network1": network.port})
	rcode, addr, took := lookup(t, router, "site.example")
	if rcode != mDNS.RcodeSuccess || addr != "192.0.2.53" {
		t.Fatalf("answer = %d %q, want the network's resolver's", rcode, addr)
	}
	if took > time.Second {
		t.Errorf("a refused connection cost %v", took)
	}
	// The line the service reads the failure from (service/dnsfallback.go):
	// it names the resolver and its port.
	if !strings.Contains(j.text(), "exchange failed for site.example. IN A: ") || !strings.Contains(j.text(), "127.0.0.1:"+itoaPort(directPort)) {
		t.Errorf("the failure was not reported:\n%s", j.text())
	}
}

// One that takes the packets and says nothing: the network's resolver
// answers after the direct one's own, short, timeout.
func TestChainFallsBackWhenDirectIsSilent(t *testing.T) {
	silent := startFakeDNS(t, "127.0.0.1", "", mDNS.RcodeSuccess, true)
	network := startFakeDNS(t, "127.0.0.2", "192.0.2.53", mDNS.RcodeSuccess, false)
	o := chainOptions(0, true)
	o.DNS.Direct = "127.0.0.1"
	router, _ := startRouter(t, o, map[string]uint16{"direct": silent.port, "network1": network.port})
	rcode, addr, took := lookup(t, router, "site.example")
	if rcode != mDNS.RcodeSuccess || addr != "192.0.2.53" {
		t.Fatalf("answer = %d %q, want the network's resolver's", rcode, addr)
	}
	if took < time.Second || took > 4*time.Second {
		t.Errorf("took %v: the direct resolver's attempt is bounded by %s", took, directAttemptTimeout)
	}
	if silent.queries.Load() == 0 {
		t.Error("the direct resolver was not asked first")
	}
}

// A direct resolver that works is the only one asked: the network's never
// sees the names the user chose another resolver for.
func TestChainDoesNotAskTheNetworkWhenDirectAnswers(t *testing.T) {
	direct := startFakeDNS(t, "127.0.0.1", "192.0.2.7", mDNS.RcodeSuccess, false)
	network := startFakeDNS(t, "127.0.0.2", "192.0.2.53", mDNS.RcodeSuccess, false)
	o := chainOptions(0, true)
	o.DNS.Direct = "127.0.0.1"
	router, _ := startRouter(t, o, map[string]uint16{"direct": direct.port, "network1": network.port})
	for _, name := range []string{"a.example", "b.example", "c.example"} {
		if rcode, addr, _ := lookup(t, router, name); rcode != mDNS.RcodeSuccess || addr != "192.0.2.7" {
			t.Fatalf("%s: answer = %d %q", name, rcode, addr)
		}
	}
	if n := network.queries.Load(); n != 0 {
		t.Errorf("the network's resolver was asked %d times while the direct one answered", n)
	}
}

// A name that does not exist is an answer: the next resolver is not asked.
func TestChainKeepsNXDOMAIN(t *testing.T) {
	direct := startFakeDNS(t, "127.0.0.1", "", mDNS.RcodeNameError, false)
	network := startFakeDNS(t, "127.0.0.2", "192.0.2.53", mDNS.RcodeSuccess, false)
	o := chainOptions(0, true)
	o.DNS.Direct = "127.0.0.1"
	router, _ := startRouter(t, o, map[string]uint16{"direct": direct.port, "network1": network.port})
	if rcode, _, _ := lookup(t, router, "nothing.example"); rcode != mDNS.RcodeNameError {
		t.Errorf("rcode = %d, want NXDOMAIN", rcode)
	}
	if n := network.queries.Load(); n != 0 {
		t.Errorf("the network's resolver was asked %d times about a name that does not exist", n)
	}
}

// Neither answering: the lookup fails with SERVFAIL rather than falling
// through to whatever rule comes next.
func TestChainFailsWhenNoneAnswers(t *testing.T) {
	o := chainOptions(refusedPort(t), true)
	router, _ := startRouter(t, o, map[string]uint16{"network1": refusedUDPPort(t)})
	rcode, addr, _ := lookup(t, router, "site.example")
	if rcode != mDNS.RcodeServerFailure || addr != "" {
		t.Errorf("answer = %d %q, want SERVFAIL", rcode, addr)
	}
}

func refusedUDPPort(t *testing.T) uint16 {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.2:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(pc.LocalAddr().(*net.UDPAddr).Port)
	pc.Close() // ICMP port unreachable comes back
	return port
}

// With the default route through the tunnel, only the names that go direct
// have the chain: the others are the remote resolver's, and never reach
// the network's.
func TestChainOnlyForDirectNames(t *testing.T) {
	network := startFakeDNS(t, "127.0.0.2", "192.0.2.53", mDNS.RcodeSuccess, false)
	o := chainOptions(refusedPort(t), false)
	o.DNS.Remote = "tcp://127.0.0.1:" + itoaPort(refusedPort(t))
	router, _ := startRouter(t, o, map[string]uint16{"network1": network.port})
	if rcode, addr, _ := lookup(t, router, "www.direct.test"); rcode != mDNS.RcodeSuccess || addr != "192.0.2.53" {
		t.Errorf("direct name: answer = %d %q, want the network's resolver's", rcode, addr)
	}
	before := network.queries.Load()
	lookup(t, router, "www.proxied.example") // the dead remote resolver: it fails, and goes nowhere else
	if network.queries.Load() != before {
		t.Error("a name that goes through the tunnel was asked of the network's resolver: a leak")
	}
}

// The network's own names (DNSOptions.LocalSuffixes, names without a dot):
// its resolver's answer is taken when it is an address in the network; a
// public one, which a hostile network could give for any domain it calls
// its own, is not, and the name goes the usual way.
func TestLocalNamesOnlyInTheNetwork(t *testing.T) {
	for _, c := range []struct {
		name, local, want string
	}{
		{"intranet.corp.example", "10.1.2.3", "10.1.2.3"},
		{"proxy", "192.168.5.5", "192.168.5.5"},
		{"www.corp.example", "203.0.113.9", "198.51.100.1"},
		{"mail", "203.0.113.9", "198.51.100.1"},
	} {
		local := startFakeDNS(t, "127.0.0.3", c.local, mDNS.RcodeSuccess, false)
		direct := startFakeDNS(t, "127.0.0.1", "198.51.100.1", mDNS.RcodeSuccess, false)
		o := baseOptions()
		o.Selective = true // the usual way is the direct resolver: one that answers here
		o.DNS.FakeIP = false
		o.DNS.Direct = "127.0.0.1"
		o.DNS.LocalSuffixes = []string{"corp.example"}
		o.DNS.LocalResolver = netip.MustParseAddr("127.0.0.3")
		router, _ := startRouter(t, o, map[string]uint16{tagDNSDirect: direct.port, tagDNSLocal: local.port})
		rcode, addr, _ := lookup(t, router, c.name)
		if rcode != mDNS.RcodeSuccess || addr != c.want {
			t.Errorf("%s (network answers %s): %d %q, want %s", c.name, c.local, rcode, addr, c.want)
		}
		if local.queries.Load() == 0 {
			t.Errorf("%s: the network's resolver was not asked", c.name)
		}
		if rcode, _, _ := lookup(t, router, "wpad.corp.example"); rcode != mDNS.RcodeNameError {
			t.Errorf("wpad: rcode %d, want NXDOMAIN", rcode)
		}
	}
}
