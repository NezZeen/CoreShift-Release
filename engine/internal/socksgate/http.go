package socksgate

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// The gate's HTTP proxy, on the SOCKS port (Config.HTTP): a client whose
// first byte is not SOCKS5's 5 speaks HTTP. The system proxy of Windows
// takes only HTTP proxies (its "socks=" is SOCKS4, which the cores do not
// serve), and so do Android's Wi-Fi settings and many programs.
//
// CONNECT host:port (HTTPS and anything else over TCP) becomes a SOCKS5
// CONNECT to the core, and from its 200 on the bytes are relayed as they
// are. A plain http:// request in absolute form goes to the core the same
// way, rewritten to origin form with "Connection: close": one request per
// connection, which every client handles. A request in origin form
// ("GET / HTTP/1.1") is not a proxy request: that is what a web page
// makes the browser send to 127.0.0.1, and it is refused, so a page
// cannot use the proxy.

// bufConn is a client connection read through a buffer, so that its
// first byte can be looked at before deciding how to serve it.
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func newBufConn(c net.Conn) *bufConn { return &bufConn{Conn: c, r: bufio.NewReader(c)} }

func (c *bufConn) Read(b []byte) (int, error) { return c.r.Read(b) }

// WriteTo hides the connection's own, which would read past the buffer.
func (c *bufConn) WriteTo(w io.Writer) (int64, error) { return c.r.WriteTo(w) }

// CloseWrite half-closes the connection, as relay expects of a TCP one.
func (c *bufConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return c.Conn.Close()
}

// proxyRealm is what a client asked for credentials is told.
const proxyRealm = "CoreShift"

// serveHTTP serves one HTTP proxy request of s; core waits for the
// connection to the core that handle started dialling.
func (g *Gate) serveHTTP(s *session, core func() (net.Conn, error)) {
	c := s.client.(*bufConn)
	req, err := http.ReadRequest(c.r)
	if err != nil {
		return
	}
	if !g.httpAllowed(req) {
		httpError(c, http.StatusProxyAuthRequired, "Proxy-Authenticate: Basic realm=\""+proxyRealm+"\"\r\n")
		return
	}
	target, tunnel, err := httpTarget(req)
	if err != nil {
		httpError(c, http.StatusBadRequest, "")
		return
	}
	msg, err := connectRequest(target)
	if err != nil {
		httpError(c, http.StatusBadRequest, "")
		return
	}
	// The core may take a while to start, and then to reach the
	// destination, as for SOCKS.
	c.SetDeadline(time.Time{})
	up, err := core()
	if err != nil {
		httpError(c, http.StatusBadGateway, "")
		return
	}
	up.SetDeadline(time.Now().Add(replyTimeout))
	if _, err := up.Write(msg); err != nil {
		httpError(c, http.StatusBadGateway, "")
		return
	}
	rep, err := readReply(up)
	if err != nil || rep.code != 0 {
		httpError(c, http.StatusBadGateway, "")
		return
	}
	up.SetDeadline(time.Time{})
	if tunnel {
		if _, err := c.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")); err != nil {
			return
		}
		relay(c, up, &g.up, &g.down)
		return
	}
	req.Header.Del("Proxy-Connection")
	req.Header.Del("Proxy-Authorization")
	req.Close = true
	if err := req.Write(counter{up, &g.up}); err != nil {
		return
	}
	// The request is all there is: what comes back is the answer, and the
	// server closes the connection after it (Connection: close).
	pipe(c, up, &g.down)
}

// httpAllowed reports whether req may use the proxy: without credentials
// when the gate is open to HTTP, else with Auth's or Guest's as Basic
// proxy credentials.
func (g *Gate) httpAllowed(req *http.Request) bool {
	if g.open || g.openHTTP {
		return true
	}
	scheme, enc, ok := strings.Cut(req.Header.Get("Proxy-Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Basic") {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(enc))
	if err != nil {
		return false
	}
	user, pass, ok := strings.Cut(string(raw), ":")
	return ok && g.known([]byte(user), []byte(pass))
}

var errNotProxy = errors.New("socksgate: not a proxy request")

// httpTarget returns where req goes, host:port, and whether it is a
// CONNECT tunnel rather than a plain http:// request.
func httpTarget(req *http.Request) (target string, tunnel bool, err error) {
	if req.Method == http.MethodConnect {
		host, port, err := net.SplitHostPort(req.Host)
		if err != nil || host == "" || port == "" {
			return "", false, errNotProxy
		}
		return net.JoinHostPort(host, port), true, nil
	}
	u := req.URL
	if !u.IsAbs() || !strings.EqualFold(u.Scheme, "http") || u.Host == "" {
		return "", false, errNotProxy
	}
	host, port := u.Hostname(), u.Port()
	if port == "" {
		port = "80"
	}
	if host == "" {
		return "", false, errNotProxy
	}
	return net.JoinHostPort(host, port), false, nil
}

// connectRequest is the SOCKS5 CONNECT to target, host:port: an address
// as one, a name as a name, for the core to resolve through the server.
func connectRequest(target string) ([]byte, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil || p == 0 {
		return nil, fmt.Errorf("socksgate: bad port %q", port)
	}
	b := []byte{5, 1, 0}
	if ip, err := netip.ParseAddr(host); err == nil {
		return appendAddr(b, netip.AddrPortFrom(ip.WithZone(""), uint16(p))), nil
	}
	if host == "" || len(host) > 255 {
		return nil, fmt.Errorf("socksgate: bad host %q", host)
	}
	b = append(b, atypDomain, byte(len(host)))
	b = append(b, host...)
	return binary.BigEndian.AppendUint16(b, uint16(p)), nil
}

// httpError answers with code and no body; extra are header lines.
func httpError(c net.Conn, code int, extra string) {
	fmt.Fprintf(c, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\nConnection: close\r\n%s\r\n", code, http.StatusText(code), extra)
}
