// Package dnsprobe asks a DNS server one question, over the network the
// way the TUN layer would, to tell whether the server answers from here and,
// if it does not, why: a provider that blocks a resolver (a white list, a
// filter on DNS over HTTPS) shows as a refused connection, a silence or a
// handshake that dies, and the settings page says so.
//
// The caller chooses how the connection leaves (Dial): around the tunnel,
// bound to the physical interface, for the resolver of direct names; through
// the core's SOCKS port for the resolver the tunnel carries.
package dnsprobe

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"coreshift/engine/internal/tunlayer"

	"github.com/miekg/dns"
	"golang.org/x/net/proxy"
)

// Kind is why a server did not answer.
type Kind string

const (
	KindRefused     Kind = "refused"     // the connection was refused
	KindTimeout     Kind = "timeout"     // nothing came back in time
	KindTLS         Kind = "tls"         // the handshake failed, or the certificate is not the server's
	KindReset       Kind = "reset"       // the connection was cut
	KindUnreachable Kind = "unreachable" // the network has no way there
	KindRcode       Kind = "rcode"       // it answered with an error code (REFUSED, SERVFAIL)
	KindHTTP        Kind = "http"        // DNS over HTTPS: the web server answered with an error
	KindUnsupported Kind = "unsupported" // a protocol this cannot try (QUIC, HTTP/3)
	KindOther       Kind = "other"
)

// Result is what asking a server came to.
type Result struct {
	OK  bool
	RTT time.Duration // of the whole exchange, connection included
	// Kind and Err say why not, when not OK. Err is the detail the kind
	// does not carry: the answer's code, the HTTP status, the raw error.
	Kind Kind
	Err  string
}

// Dial opens a connection the way the probe is to go out.
type Dial func(ctx context.Context, network, addr string) (net.Conn, error)

// Timeout bounds one probe when the caller gives none.
const Timeout = 5 * time.Second

// rootCAs verifies the servers; nil means the system's roots. Tests set it.
var rootCAs *x509.CertPool

// name is what is asked: it exists, and no filter blocks it.
const name = "example.com."

// Check sends one query to srv over dial and reports how it went.
func Check(ctx context.Context, srv tunlayer.DNSServer, dial Dial, timeout time.Duration) Result {
	if timeout <= 0 {
		timeout = Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	var err error
	switch srv.Type {
	case "udp":
		err = overUDP(ctx, srv, dial)
	case "tcp":
		err = overStream(ctx, srv, dial, false)
	case "tls":
		err = overStream(ctx, srv, dial, true)
	case "https":
		err = overHTTPS(ctx, srv, dial)
	default:
		return Result{Kind: KindUnsupported}
	}
	if err != nil {
		return classify(ctx, err)
	}
	return Result{OK: true, RTT: time.Since(start)}
}

func port(srv tunlayer.DNSServer, def uint16) string {
	p := srv.Port
	if p == 0 {
		p = def
	}
	return strconv.Itoa(int(p))
}

func question(id uint16) (*dns.Msg, []byte, error) {
	q := new(dns.Msg)
	q.SetQuestion(name, dns.TypeA)
	q.Id = id
	wire, err := q.Pack()
	return q, wire, err
}

// rcodeError is an answer that says no.
type rcodeError struct{ rcode int }

func (e rcodeError) Error() string { return dns.RcodeToString[e.rcode] }

// check takes an answer: a response to the question, with the code of a
// resolver that knows the name (or knows there is none).
func check(q *dns.Msg, wire []byte) error {
	r := new(dns.Msg)
	if err := r.Unpack(wire); err != nil {
		return fmt.Errorf("not a DNS answer: %w", err)
	}
	if !r.Response || (q.Id != 0 && r.Id != q.Id) {
		return errors.New("not an answer to the question")
	}
	if r.Rcode != dns.RcodeSuccess && r.Rcode != dns.RcodeNameError {
		return rcodeError{r.Rcode}
	}
	return nil
}

func overUDP(ctx context.Context, srv tunlayer.DNSServer, dial Dial) error {
	conn, err := dial(ctx, "udp", net.JoinHostPort(srv.Host, port(srv, 53)))
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	q, wire, err := question(uint16(time.Now().UnixNano()))
	if err != nil {
		return err
	}
	if _, err := conn.Write(wire); err != nil {
		return err
	}
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		// Anything else on the socket is not for us; keep waiting.
		if err := check(q, buf[:n]); err == nil || errors.As(err, new(rcodeError)) {
			return err
		}
	}
}

func overStream(ctx context.Context, srv tunlayer.DNSServer, dial Dial, secure bool) error {
	def := uint16(53)
	if secure {
		def = 853
	}
	conn, err := dial(ctx, "tcp", net.JoinHostPort(srv.Host, port(srv, def)))
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	if secure {
		t := tls.Client(conn, &tls.Config{ServerName: srv.Host, RootCAs: rootCAs, MinVersion: tls.VersionTLS12})
		if err := t.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = t
	}
	q, wire, err := question(uint16(time.Now().UnixNano()))
	if err != nil {
		return err
	}
	framed := binary.BigEndian.AppendUint16(nil, uint16(len(wire)))
	if _, err := conn.Write(append(framed, wire...)); err != nil {
		return err
	}
	var size [2]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return err
	}
	body := make([]byte, binary.BigEndian.Uint16(size[:]))
	if _, err := io.ReadFull(conn, body); err != nil {
		return err
	}
	return check(q, body)
}

// httpStatusError is a DNS-over-HTTPS server's web error.
type httpStatusError struct{ status string }

func (e httpStatusError) Error() string { return "HTTP " + e.status }

func overHTTPS(ctx context.Context, srv tunlayer.DNSServer, dial Dial) error {
	path := srv.Path
	if path == "" {
		path = "/dns-query"
	}
	u := url.URL{Scheme: "https", Host: net.JoinHostPort(srv.Host, port(srv, 443)), Path: path}
	tr := &http.Transport{
		Proxy:               nil,
		DialContext:         dial,
		TLSClientConfig:     &tls.Config{RootCAs: rootCAs, MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: Timeout,
	}
	defer tr.CloseIdleConnections()
	q, wire, err := question(0) // RFC 8484 asks for 0
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(wire))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return httpStatusError{strconv.Itoa(resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return err
	}
	return check(q, body)
}

// classify turns the error of a probe into a Result.
func classify(ctx context.Context, err error) Result {
	var rc rcodeError
	var hs httpStatusError
	switch {
	case errors.As(err, &rc):
		return Result{Kind: KindRcode, Err: rc.Error()}
	case errors.As(err, &hs):
		return Result{Kind: KindHTTP, Err: hs.status}
	}
	s := strings.ToLower(err.Error())
	var (
		unknownCA  x509.UnknownAuthorityError
		hostname   x509.HostnameError
		invalid    x509.CertificateInvalidError
		verify     *tls.CertificateVerificationError
		recordHdr  tls.RecordHeaderError
		netErr     net.Error
		isTimeout  = errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded)
		isNetError = errors.As(err, &netErr)
	)
	switch {
	case strings.Contains(s, "refused"):
		return Result{Kind: KindRefused}
	case errors.As(err, &unknownCA), errors.As(err, &hostname), errors.As(err, &invalid), errors.As(err, &verify), errors.As(err, &recordHdr),
		strings.Contains(s, "tls:"), strings.Contains(s, "x509:"):
		return Result{Kind: KindTLS, Err: tidy(err)}
	case isTimeout || (isNetError && netErr.Timeout()) || strings.Contains(s, "timeout"):
		return Result{Kind: KindTimeout}
	case strings.Contains(s, "reset"), strings.Contains(s, "broken pipe"), strings.Contains(s, "forcibly closed"), strings.Contains(s, "aborted"), strings.Contains(s, "eof"):
		return Result{Kind: KindReset}
	case strings.Contains(s, "unreachable"), strings.Contains(s, "no route to host"), strings.Contains(s, "network is down"):
		return Result{Kind: KindUnreachable}
	}
	return Result{Kind: KindOther, Err: tidy(err)}
}

// KindOfText reads why a lookup failed from a line of the TUN layer's
// journal ("dial tcp 1.2.3.4:443: connect: connection refused"); empty when
// the line says nothing a Kind covers.
func KindOfText(line string) Kind {
	s := strings.ToLower(line)
	switch {
	case strings.Contains(s, "refused"):
		return KindRefused
	case strings.Contains(s, "tls:"), strings.Contains(s, "x509:"):
		return KindTLS
	case strings.Contains(s, "timeout"), strings.Contains(s, "deadline exceeded"):
		return KindTimeout
	case strings.Contains(s, "reset"), strings.Contains(s, "broken pipe"), strings.Contains(s, "forcibly closed"), strings.Contains(s, "aborted"), strings.Contains(s, "eof"):
		return KindReset
	case strings.Contains(s, "unreachable"), strings.Contains(s, "no route to host"), strings.Contains(s, "network is down"):
		return KindUnreachable
	}
	return ""
}

// tidy is err on one short line.
func tidy(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

// SOCKS5 returns a Dial through the SOCKS5 port at addr (the active core's
// inbound), with the credentials it asks, if any. Only TCP goes through.
func SOCKS5(addr, user, pass string, timeout time.Duration) (Dial, error) {
	var auth *proxy.Auth
	if user != "" {
		auth = &proxy.Auth{User: user, Password: pass}
	}
	d, err := proxy.SOCKS5("tcp", addr, auth, &net.Dialer{Timeout: timeout})
	if err != nil {
		return nil, err
	}
	cd, ok := d.(proxy.ContextDialer)
	if !ok {
		return nil, errors.New("dnsprobe: the SOCKS5 dialer takes no context")
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if strings.HasPrefix(network, "udp") {
			return nil, errors.New("dnsprobe: UDP does not go through SOCKS5 here")
		}
		return cd.DialContext(ctx, network, addr)
	}, nil
}
