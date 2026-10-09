// Package doh looks names up with DNS over HTTPS (RFC 8484), asking public
// resolvers reached by a fixed address: finding them needs no DNS, so a
// network whose own resolver fails or lies about a name still gets it.
package doh

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Server is a DNS-over-HTTPS resolver.
type Server struct {
	// Addr is where it is dialled, so its name is never looked up.
	Addr netip.AddrPort
	// URL is its query endpoint. The host checks the certificate.
	URL string
}

// Servers are well-known public resolvers, asked all at once: one of them
// is usually within reach where the others are blocked.
var Servers = []Server{
	{Addr: netip.MustParseAddrPort("1.1.1.1:443"), URL: "https://cloudflare-dns.com/dns-query"},
	{Addr: netip.MustParseAddrPort("77.88.8.8:443"), URL: "https://common.dot.dns.yandex.net/dns-query"},
	{Addr: netip.MustParseAddrPort("8.8.8.8:443"), URL: "https://dns.google/dns-query"},
}

// DefaultTimeout bounds a lookup when Resolver.Timeout is 0.
const DefaultTimeout = 5 * time.Second

// maxAnswer bounds the size of an answer read: a DNS message is at most
// 64 KiB.
const maxAnswer = 64 << 10

// Resolver looks names up through Servers. It keeps connections to them
// open for a while, so a list of names shares them. The zero value asks
// Servers through a plain net.Dialer.
type Resolver struct {
	// Servers to ask; nil means the package's Servers.
	Servers []Server
	// Dial opens connections to the servers' addresses, e.g. bound to the
	// physical interface around a VPN tunnel. nil means a net.Dialer.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// RootCAs verifies the servers; nil means the system's roots.
	RootCAs *x509.CertPool
	// Timeout bounds one lookup; 0 means DefaultTimeout.
	Timeout time.Duration

	once    sync.Once
	servers []Server
	clients []*http.Client // one per server
	err     error
}

// errNotFound is a server's answer that the name has no addresses.
var errNotFound = errors.New("no such host")

func (r *Resolver) init() {
	r.servers = r.Servers
	if r.servers == nil {
		r.servers = Servers
	}
	dial := r.Dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: DefaultTimeout}).DialContext
	}
	for _, s := range r.servers {
		u, err := url.Parse(s.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || !s.Addr.IsValid() {
			r.err = fmt.Errorf("DNS over HTTPS: bad server %v %q", s.Addr, s.URL)
			return
		}
		// Whatever the URL names, the connection goes to the fixed address:
		// the transport never looks a name up.
		to := s.Addr.String()
		r.clients = append(r.clients, &http.Client{Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dial(ctx, network, to)
			},
			TLSClientConfig:     &tls.Config{RootCAs: r.RootCAs, MinVersion: tls.VersionTLS12},
			ForceAttemptHTTP2:   true,
			TLSHandshakeTimeout: DefaultTimeout,
			// A latency test looks dozens of names up at once: they share a
			// few connections rather than open one each.
			MaxConnsPerHost:     2,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     30 * time.Second,
		}})
	}
}

// CloseIdleConnections closes the connections kept open to the servers.
func (r *Resolver) CloseIdleConnections() {
	r.once.Do(r.init)
	for _, c := range r.clients {
		c.CloseIdleConnections()
	}
}

// Lookup returns the addresses of host, IPv4 ones first: its A records, or
// its AAAA records when it has no A. Every server is asked at once and the
// first to give addresses wins. A name no server knows fails with a
// *net.DNSError that IsNotFound; servers that do not answer in time, with
// one that IsTimeout.
func (r *Resolver) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if a, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{a}, nil
	}
	r.once.Do(r.init)
	if r.err != nil {
		return nil, r.err
	}
	servers := r.servers
	if len(servers) == 0 {
		return nil, errors.New("DNS over HTTPS: no servers")
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		addrs []netip.Addr
		err   error
	}
	results := make(chan result, len(servers))
	for i, s := range servers {
		go func() {
			c := r.clients[i]
			addrs, err := ask(ctx, c, s.URL, host, dns.TypeA)
			if err == nil && len(addrs) == 0 {
				addrs, err = ask(ctx, c, s.URL, host, dns.TypeAAAA)
			}
			if err == nil && len(addrs) == 0 {
				err = errNotFound
			}
			if err != nil {
				err = fmt.Errorf("%s: %w", s.Addr.Addr(), err)
			}
			results <- result{addrs, err}
		}()
	}
	var errs []error
	notFound := false
	for range servers {
		res := <-results
		if res.err == nil {
			return res.addrs, nil
		}
		notFound = notFound || errors.Is(res.err, errNotFound)
		errs = append(errs, res.err)
	}
	de := &net.DNSError{Err: errors.Join(errs...).Error(), Name: host, Server: "DNS over HTTPS"}
	switch {
	case notFound:
		de.Err, de.IsNotFound = errNotFound.Error(), true
	case ctx.Err() != nil:
		de.IsTimeout = true
	}
	return nil, de
}

// ask sends one query to the server at endpoint through c and returns the
// addresses of the type asked for in its answer.
func ask(ctx context.Context, c *http.Client, endpoint, host string, qtype uint16) ([]netip.Addr, error) {
	q := new(dns.Msg)
	q.SetQuestion(dns.Fqdn(host), qtype)
	q.Id = 0 // RFC 8484 asks for 0, which caches better
	wire, err := q.Pack()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(wire))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return nil, err
	}
	var a dns.Msg
	if err := a.Unpack(body); err != nil {
		return nil, fmt.Errorf("bad answer: %w", err)
	}
	switch a.Rcode {
	case dns.RcodeSuccess:
	case dns.RcodeNameError:
		return nil, errNotFound
	default:
		return nil, fmt.Errorf("server answered %s", dns.RcodeToString[a.Rcode])
	}
	var addrs []netip.Addr
	for _, rr := range a.Answer {
		var ip net.IP
		switch v := rr.(type) {
		case *dns.A:
			if qtype == dns.TypeA {
				ip = v.A
			}
		case *dns.AAAA:
			if qtype == dns.TypeAAAA {
				ip = v.AAAA
			}
		}
		if a, ok := netip.AddrFromSlice(ip); ok {
			addrs = append(addrs, a.Unmap())
		}
	}
	return addrs, nil
}
