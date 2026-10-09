package ping

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestICMPLoopback(t *testing.T) {
	rtt, err := ICMP(context.Background(), netip.MustParseAddr("127.0.0.1"), Bind{}, 2, time.Second)
	if errors.Is(err, ErrUnsupported) {
		t.Skip("ICMP echo needs privileges here")
	}
	if err != nil || rtt <= 0 || rtt > time.Second {
		t.Fatalf("rtt %v, err %v", rtt, err)
	}
}

func TestTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	addr := netip.MustParseAddrPort(ln.Addr().String())
	rtt, err := TCP(context.Background(), addr, Bind{Source: addr.Addr()}, 2, time.Second)
	if err != nil || rtt <= 0 {
		t.Fatalf("rtt %v, err %v", rtt, err)
	}
	ln.Close()
	if _, err := TCP(context.Background(), addr, Bind{}, 1, time.Second); err == nil {
		t.Fatal("closed port answered")
	}
}

// scriptedDial answers handshakes in turn: nil connects, errHang waits for
// the attempt's deadline, any other error fails at once. Past the script it
// connects.
type scriptedDial struct {
	script []error
	calls  int
}

var errHang = errors.New("hang")

func (s *scriptedDial) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	var e error
	if s.calls < len(s.script) {
		e = s.script[s.calls]
	}
	s.calls++
	switch {
	case e == errHang:
		<-ctx.Done()
		return nil, ctx.Err()
	case e != nil:
		return nil, e
	}
	a, b := net.Pipe()
	b.Close()
	return a, nil
}

func TestTCPRetry(t *testing.T) {
	old := tcpRetryPause
	tcpRetryPause = time.Millisecond
	t.Cleanup(func() { tcpRetryPause = old })
	refused := errors.New("connection refused")
	dst := netip.MustParseAddrPort("192.0.2.1:443")
	for name, c := range map[string]struct {
		count  int
		script []error
		calls  int
		fails  bool
	}{
		"all answer":                 {3, nil, 3, false},
		"first lost, then answered":  {3, []error{refused}, 4, false},
		"first timed out, then up":   {1, []error{errHang}, 2, false},
		"refused twice":              {3, []error{refused, refused}, 2, true},
		"timed out twice":            {3, []error{errHang, errHang}, 2, true},
		"one count, refused twice":   {1, []error{refused, refused}, 2, true},
		"later failures do not stop": {3, []error{nil, refused, refused}, 3, false},
	} {
		d := &scriptedDial{script: c.script}
		start := time.Now()
		rtt, err := tcp(context.Background(), dst, c.count, 50*time.Millisecond, d.dial)
		if (err != nil) != c.fails || (!c.fails && rtt <= 0) {
			t.Errorf("%s: rtt %v, err %v", name, rtt, err)
		}
		if d.calls != c.calls {
			t.Errorf("%s: %d handshakes, want %d", name, d.calls, c.calls)
		}
		// Two timeouts at most, however many attempts were asked for.
		if el := time.Since(start); el > time.Second {
			t.Errorf("%s: took %v", name, el)
		}
	}
}

// A cancelled test does not wait for the retry.
func TestTCPCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := &scriptedDial{script: []error{errHang}}
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	start := time.Now()
	if _, err := tcp(ctx, netip.MustParseAddrPort("192.0.2.1:443"), 3, 5*time.Second, d.dial); err == nil {
		t.Fatal("cancelled ping succeeded")
	}
	if d.calls != 1 || time.Since(start) > 2*time.Second {
		t.Errorf("%d handshakes in %v", d.calls, time.Since(start))
	}
}

func TestPhysical(t *testing.T) {
	b, err := Physical()
	if errors.Is(err, ErrUnsupported) {
		t.Skip(err)
	}
	if err != nil {
		t.Skipf("no default route here: %v", err)
	}
	t.Logf("physical: %v", b)
	if !b.Source.Is4() || b.Interface == "" {
		t.Fatalf("bind = %+v", b)
	}
}
