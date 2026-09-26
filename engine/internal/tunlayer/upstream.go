package tunlayer

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// upstream is a parsed DNS server address. Accepted forms:
//
//	1.1.1.1  udp://1.1.1.1:53  tcp://1.1.1.1  tls://1.1.1.1  https://1.1.1.1/dns-query
//	h3://1.1.1.1/dns-query  quic://dns.adguard-dns.com
type upstream struct {
	Type string // sing-box DNS server type
	Host string
	Port uint16 // 0 = protocol default
	Path string // https and h3 only; empty = /dns-query
}

var schemeTypes = map[string]string{
	"udp": "udp", "tcp": "tcp",
	"tls": "tls", "dot": "tls",
	"https": "https", "doh": "https",
	"h3":   "h3",
	"quic": "quic", "doq": "quic",
}

func parseUpstream(raw string) (upstream, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return upstream{}, fmt.Errorf("tunlayer: empty DNS server")
	}
	if !strings.Contains(raw, "://") {
		host, port, err := splitHostPort(raw)
		if err != nil {
			return upstream{}, fmt.Errorf("tunlayer: DNS server %q: %w", raw, err)
		}
		return upstream{Type: "udp", Host: host, Port: port}, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return upstream{}, fmt.Errorf("tunlayer: DNS server %q: %w", raw, err)
	}
	typ, ok := schemeTypes[strings.ToLower(u.Scheme)]
	if !ok {
		return upstream{}, fmt.Errorf("tunlayer: DNS server %q: unsupported scheme %q", raw, u.Scheme)
	}
	if u.Hostname() == "" {
		return upstream{}, fmt.Errorf("tunlayer: DNS server %q: missing host", raw)
	}
	up := upstream{Type: typ, Host: u.Hostname()}
	if p := u.Port(); p != "" {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil || n == 0 {
			return upstream{}, fmt.Errorf("tunlayer: DNS server %q: bad port", raw)
		}
		up.Port = uint16(n)
	}
	if (typ == "https" || typ == "h3") && u.Path != "" && u.Path != "/" && u.Path != "/dns-query" {
		up.Path = u.Path
	}
	return up, nil
}

// splitHostPort accepts "1.1.1.1", "1.1.1.1:5353", "2606:4700::1111",
// "[2606:4700::1111]:53" and "dns.example".
func splitHostPort(s string) (string, uint16, error) {
	if a, err := netip.ParseAddr(s); err == nil {
		return a.String(), 0, nil
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().String(), ap.Port(), nil
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return s, 0, nil // bare hostname
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return "", 0, fmt.Errorf("bad port %q", port)
	}
	return host, uint16(n), nil
}

func (u upstream) isIP() bool {
	_, err := netip.ParseAddr(u.Host)
	return err == nil
}

func (u upstream) server(tag string) obj {
	s := obj{"type": u.Type, "tag": tag, "server": u.Host}
	if u.Port != 0 {
		s["server_port"] = u.Port
	}
	if u.Path != "" {
		s["path"] = u.Path
	}
	return s
}

// ValidateDNSServer reports whether raw is a DNS server address the layer
// accepts (see upstream for the forms).
func ValidateDNSServer(raw string) error {
	_, err := parseUpstream(raw)
	return err
}
