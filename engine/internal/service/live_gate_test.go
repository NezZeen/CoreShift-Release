package service

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/protocol/socks"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/store"
)

// udpEcho answers every datagram with "echo:" and the datagram.
func udpEcho(t *testing.T) netip.AddrPort {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			pc.WriteTo(append([]byte("echo:"), buf[:n]...), from)
		}
	}()
	return netip.MustParseAddrPort(pc.LocalAddr().String())
}

// udpThrough sends datagrams to echo through the SOCKS port the way the
// TUN layer does (sing's SOCKS client, UDP ASSOCIATE) and checks the
// answers.
func udpThrough(socksAddr netip.AddrPort, auth core.SOCKSAuth, echo netip.AddrPort) error {
	client := socks.NewClient(N.SystemDialer, metadata.SocksaddrFromNetIP(socksAddr), socks.Version5, auth.User, auth.Pass)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dst := metadata.SocksaddrFromNetIP(echo)
	pc, err := client.ListenPacket(ctx, dst)
	if err != nil {
		return err
	}
	defer pc.Close()
	pc.SetDeadline(time.Now().Add(5 * time.Second))
	for i := range 3 {
		msg := fmt.Sprintf("datagram %d", i)
		if _, err := pc.WriteTo([]byte(msg), dst.UDPAddr()); err != nil {
			return err
		}
		buf := make([]byte, 2048)
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			return err
		}
		if got := string(buf[:n]); got != "echo:"+msg {
			return fmt.Errorf("got %q", got)
		}
	}
	return nil
}

// TestLiveSOCKSPort: through the SOCKS port the service holds, with each
// real core behind it, TCP and UDP get through. Without TUN the port is
// open; on Android it requires the credentials, and the core's own port
// takes neither none nor those.
func TestLiveSOCKSPort(t *testing.T) {
	l := newLiveHarness(t, nil)
	bins := liveCores(t)
	pA := freeTCPPort(t)
	startProxyServer(t, bins[core.SingBox], []map[string]any{ssInbound(pA)})
	sub, err := l.st.Add(context.Background(), store.AddRequest{Name: "pasted", Content: ssLink(pA, "A")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.st.Select(sub.ID, sub.Fingerprints()[0], "A"); err != nil {
		t.Fatal(err)
	}
	echo := udpEcho(t)

	for _, android := range []bool{false, true} {
		l.svc.cfg.AppOutsideVPN = android
		for _, k := range []core.Kind{core.Xray, core.SingBox, core.Mihomo} {
			name := fmt.Sprintf("%s, android %v", k, android)
			set := l.st.Settings()
			set.Cores.Mode, set.Cores.Manual = store.ModeManual, k
			if _, err := l.st.SetSettings(set); err != nil {
				t.Fatal(err)
			}
			if err := l.svc.ConnectSelected(context.Background()); err != nil {
				t.Fatalf("%s: connect: %v", name, err)
			}
			if st := l.svc.Status(); st.Core != k {
				t.Fatalf("%s: status %+v", name, st)
			}
			auth := l.svc.sup.SOCKSAuth()
			if err := l.fetch(t); err != nil {
				t.Errorf("%s: a page: %v", name, err)
			}
			if err := udpThrough(l.listen, auth, echo); err != nil {
				t.Errorf("%s: UDP with the credentials: %v", name, err)
			}
			err := udpThrough(l.listen, core.SOCKSAuth{}, echo)
			if android && err == nil {
				t.Errorf("%s: UDP without credentials got through", name)
			}
			if !android && err != nil {
				t.Errorf("%s: UDP through the open proxy: %v", name, err)
			}
			corePort, _ := l.svc.sup.CoreListen()
			for _, a := range []core.SOCKSAuth{{}, auth} {
				if udpThrough(corePort, a, echo) == nil {
					t.Errorf("%s: the core's own port associated a client of the SOCKS port", name)
				}
			}
		}
	}
}

// TestLiveTakeoverDuringSwap: while the real cores crash one after the
// other, a program trying all along to take the SOCKS port never gets it,
// and the next core answers there.
func TestLiveTakeoverDuringSwap(t *testing.T) {
	l := newLiveHarness(t, nil)
	bins := liveCores(t)
	pA := freeTCPPort(t)
	startProxyServer(t, bins[core.SingBox], []map[string]any{ssInbound(pA)})
	sub, err := l.st.Add(context.Background(), store.AddRequest{Name: "pasted", Content: ssLink(pA, "A")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.st.Select(sub.ID, sub.Fingerprints()[0], "A"); err != nil {
		t.Fatal(err)
	}
	if err := l.svc.ConnectSelected(context.Background()); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	taken := make(chan bool, 1)
	go func() {
		for {
			select {
			case <-stop:
				taken <- false
				return
			default:
			}
			if ln, err := net.Listen("tcp", l.listen.String()); err == nil {
				ln.Close()
				taken <- true
				return
			}
		}
	}()
	for _, next := range []core.Kind{core.SingBox, core.Mihomo} {
		p, ok := l.svc.sup.CoreListen()
		if !ok {
			t.Fatal("no core")
		}
		killListener(t, p.Port())
		l.waitFor(t, "swap to "+string(next), 20*time.Second, func() bool { return l.svc.Status().Core == next })
		if err := l.fetch(t); err != nil {
			t.Fatalf("through %s: %v", next, err)
		}
	}
	close(stop)
	if <-taken {
		t.Fatal("another program took the SOCKS port during a swap")
	}
}
