// Package socksgate holds the SOCKS5 port that the TUN layer, and without
// it the user's programs, send traffic to, for as long as a connection
// lasts, in front of whichever core runs.
//
// Each core listens on a loopback port of its own, a new one on every
// start, and requires credentials only the engine knows. The gate checks
// the client's credentials (or lets it in, as the open proxy without the
// TUN layer), connects to the current core, authenticates there and from
// then on relays the bytes unchanged: the client's request, the core's
// reply and the data. Every SOCKS command therefore works the way the core
// implements it. The reply to UDP ASSOCIATE names the core's own UDP relay,
// which the client then talks to directly, so datagrams never take the
// extra hop.
//
// Swapping cores never closes the gate's port: no other program can take
// it in between, as it could when each core opened the port itself.
package socksgate

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"coreshift/engine/internal/core"
)

// Target is the core connections are relayed to: its SOCKS5 inbound and
// the credentials it requires.
type Target struct {
	Addr netip.AddrPort
	Auth core.SOCKSAuth
}

// Valid reports whether there is a core.
func (t Target) Valid() bool { return t.Addr.IsValid() }

type Config struct {
	// Listen is the gate's port.
	Listen netip.AddrPort
	// Auth is what a client must give, unless Open. Without credentials
	// the gate is open.
	Auth core.SOCKSAuth
	// Open lets in clients without credentials: the proxy the user's
	// programs are set to use has none to give (browsers cannot).
	Open bool
	// Wait is how long a connection that comes while no core runs, during
	// a swap, waits for the next one; zero means DefaultWait.
	Wait time.Duration
}

// DefaultWait covers a core starting (a few seconds at most): what
// connects in the meantime goes to the new core rather than failing.
const DefaultWait = 10 * time.Second

// handshakeTimeout bounds the SOCKS greeting on either side.
const handshakeTimeout = 10 * time.Second

// Gate is a listening gate. Its methods are safe for concurrent use.
type Gate struct {
	ln   net.Listener
	auth core.SOCKSAuth
	open bool
	wait time.Duration

	mu      sync.Mutex
	target  Target
	gen     uint64        // bumped by every SetTarget
	changed chan struct{} // closed, and replaced, when the target changes
	closed  bool
	conns   map[*session]struct{}
	wg      sync.WaitGroup
}

// session is one client connection and, once relayed, its connection to
// the core.
type session struct {
	client, core net.Conn
	gen          uint64 // the target core was dialled for
}

func (s *session) close() {
	s.client.Close()
	if s.core != nil {
		s.core.Close()
	}
}

// Listen opens the gate on cfg.Listen. It relays nothing until SetTarget.
func Listen(cfg Config) (*Gate, error) {
	lc := net.ListenConfig{Control: exclusive}
	ln, err := lc.Listen(context.Background(), "tcp", cfg.Listen.String())
	if err != nil {
		return nil, err
	}
	g := &Gate{
		ln: ln, auth: cfg.Auth, open: cfg.Open || !cfg.Auth.Set(), wait: cfg.Wait,
		changed: make(chan struct{}), conns: map[*session]struct{}{},
	}
	if g.wait <= 0 {
		g.wait = DefaultWait
	}
	g.wg.Add(1)
	go g.serve()
	return g, nil
}

// Addr is where the gate listens.
func (g *Gate) Addr() netip.AddrPort {
	return netip.MustParseAddrPort(g.ln.Addr().String())
}

// SetTarget makes t the core new connections go to; the zero Target
// stops relaying until the next one. Connections to the previous core are
// closed: it is being stopped, and a client is better told at once.
func (g *Gate) SetTarget(t Target) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	g.target = t
	g.gen++
	close(g.changed)
	g.changed = make(chan struct{})
	for s := range g.conns {
		if s.core != nil {
			s.close()
			delete(g.conns, s)
		}
	}
}

// Sessions is the number of client connections open, relayed or not.
func (g *Gate) Sessions() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.conns)
}

// Close stops listening and closes every connection.
func (g *Gate) Close() error {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return nil
	}
	g.closed = true
	close(g.changed)
	for s := range g.conns {
		s.close()
	}
	clear(g.conns)
	g.mu.Unlock()
	err := g.ln.Close()
	g.wg.Wait()
	return err
}

func (g *Gate) serve() {
	defer g.wg.Done()
	var backoff time.Duration
	for {
		c, err := g.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// Out of file descriptors and the like: the port stays ours
			// while it passes.
			backoff = min(max(2*backoff, 5*time.Millisecond), time.Second)
			time.Sleep(backoff)
			continue
		}
		backoff = 0
		s := &session{client: c}
		g.mu.Lock()
		if g.closed {
			g.mu.Unlock()
			c.Close()
			return
		}
		g.conns[s] = struct{}{}
		g.wg.Add(1)
		g.mu.Unlock()
		go func() {
			defer g.wg.Done()
			g.handle(s)
			g.mu.Lock()
			delete(g.conns, s)
			g.mu.Unlock()
			s.close()
		}()
	}
}

func (g *Gate) handle(s *session) {
	c := s.client
	c.SetDeadline(time.Now().Add(handshakeTimeout))
	// The connection to the core is made while the client greets: the
	// two handshakes overlap instead of adding up.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type dialed struct {
		up  net.Conn
		err error
	}
	ch := make(chan dialed, 1)
	go func() {
		up, err := g.dialCore(ctx, s)
		ch <- dialed{up, err}
	}()
	if err := g.greet(c); err != nil {
		cancel()
		if d := <-ch; d.up != nil {
			d.up.Close()
		}
		return
	}
	d := <-ch
	if d.err != nil {
		failRequest(c)
		return
	}
	c.SetDeadline(time.Time{})
	relay(c, d.up)
}

var (
	errVersion = errors.New("socksgate: not SOCKS5")
	errMethod  = errors.New("socksgate: no acceptable authentication method")
	errAuth    = errors.New("socksgate: wrong credentials")
)

// greet takes the client's greeting and, unless the gate is open,
// checks its credentials (RFC 1928, 1929).
func (g *Gate) greet(c net.Conn) error {
	var hdr [2]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return err
	}
	if hdr[0] != 5 {
		return errVersion
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return err
	}
	var noAuth, userPass bool
	for _, m := range methods {
		noAuth = noAuth || m == 0
		userPass = userPass || m == 2
	}
	switch {
	case g.open && noAuth:
		_, err := c.Write([]byte{5, 0})
		return err
	case userPass:
		if _, err := c.Write([]byte{5, 2}); err != nil {
			return err
		}
	default:
		c.Write([]byte{5, 0xff})
		return errMethod
	}
	user, pass, err := readUserPass(c)
	if err != nil {
		return err
	}
	// An open gate takes whatever a program was set up with.
	ok := g.open || (equal(user, g.auth.User) && equal(pass, g.auth.Pass))
	if !ok {
		c.Write([]byte{1, 1})
		return errAuth
	}
	_, err = c.Write([]byte{1, 0})
	return err
}

func readUserPass(c net.Conn) (user, pass []byte, err error) {
	var b [2]byte
	if _, err = io.ReadFull(c, b[:]); err != nil {
		return nil, nil, err
	}
	if b[0] != 1 {
		return nil, nil, errors.New("socksgate: bad authentication version")
	}
	user = make([]byte, b[1])
	if _, err = io.ReadFull(c, user); err != nil {
		return nil, nil, err
	}
	if _, err = io.ReadFull(c, b[:1]); err != nil {
		return nil, nil, err
	}
	pass = make([]byte, b[0])
	_, err = io.ReadFull(c, pass)
	return user, pass, err
}

func equal(got []byte, want string) bool {
	return subtle.ConstantTimeCompare(got, []byte(want)) == 1
}

// dialCore connects s to the current core, waiting for one during a swap,
// and authenticates there.
func (g *Gate) dialCore(ctx context.Context, s *session) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, g.wait)
	defer cancel()
	for {
		g.mu.Lock()
		t, gen, changed, closed := g.target, g.gen, g.changed, g.closed
		g.mu.Unlock()
		if closed {
			return nil, net.ErrClosed
		}
		if !t.Valid() {
			select {
			case <-changed:
				continue
			case <-ctx.Done():
				return nil, errors.New("socksgate: no core is running")
			}
		}
		up, err := dialSOCKS(ctx, t)
		if err != nil {
			return nil, err
		}
		g.mu.Lock()
		if g.closed || g.gen != gen {
			// The core changed while this connected: to the next one.
			g.mu.Unlock()
			up.Close()
			continue
		}
		s.core, s.gen = up, gen
		g.mu.Unlock()
		return up, nil
	}
}

// dialSOCKS connects to t and gets as far as the request: the greeting and
// the credentials go in one write.
func dialSOCKS(ctx context.Context, t Target) (net.Conn, error) {
	var d net.Dialer
	up, err := d.DialContext(ctx, "tcp", t.Addr.String())
	if err != nil {
		return nil, err
	}
	up.SetDeadline(time.Now().Add(handshakeTimeout))
	var msg []byte
	want := []byte{5, 0}
	if t.Auth.Set() {
		u, p := t.Auth.User, t.Auth.Pass
		if len(u) > 255 || len(p) > 255 {
			up.Close()
			return nil, errors.New("socksgate: credentials too long")
		}
		msg = append([]byte{5, 1, 2, 1, byte(len(u))}, u...)
		msg = append(append(msg, byte(len(p))), p...)
		want = []byte{5, 2, 1, 0}
	} else {
		msg = []byte{5, 1, 0}
	}
	if _, err := up.Write(msg); err != nil {
		up.Close()
		return nil, err
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(up, got); err != nil {
		up.Close()
		return nil, fmt.Errorf("socksgate: core handshake: %w", err)
	}
	if string(got) != string(want) {
		up.Close()
		return nil, fmt.Errorf("socksgate: the core refused the handshake (% x)", got)
	}
	up.SetDeadline(time.Time{})
	return up, nil
}

// failRequest answers the client's request with a general failure, read
// first so that closing does not reset the connection before the client
// sees the answer.
func failRequest(c net.Conn) {
	var hdr [5]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return
	}
	var rest int
	switch hdr[3] {
	case 1:
		rest = 4 - 1 + 2
	case 4:
		rest = 16 - 1 + 2
	case 3:
		rest = int(hdr[4]) + 2
	default:
		return
	}
	if _, err := io.ReadFull(c, make([]byte, rest)); err != nil {
		return
	}
	c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
}

// relay copies between the client and the core until both sides are done.
// A side that finishes writing is half-closed on the other; an error on
// either closes both.
func relay(c, up net.Conn) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		pipe(up, c)
	}()
	pipe(c, up)
	<-done
}

func pipe(dst, src net.Conn) {
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		src.Close()
		return
	}
	if cw, ok := dst.(interface{ CloseWrite() error }); ok {
		cw.CloseWrite()
	} else {
		dst.Close()
	}
}
