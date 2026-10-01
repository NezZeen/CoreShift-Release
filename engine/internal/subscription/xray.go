package subscription

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"coreshift/engine/internal/node"
)

// Xray JSON subscriptions are what panels such as Remnawave serve to
// Xray-based apps: one config, or a list of them, each a complete Xray
// config whose proxy outbounds are the servers. The rest of the config (the
// inbounds, routing, balancers, observatory) is for the app that runs it;
// CoreShift keeps the servers, in the order the config lists them, and picks
// among them itself.

// xrayNotServers are outbound protocols that carry traffic nowhere remote.
var xrayNotServers = map[string]bool{
	"freedom": true, "direct": true, "blackhole": true, "block": true, "dns": true, "loopback": true,
}

// parseXrayJSON reads a list of Xray configs, or the one config.
func parseXrayJSON(configs []fields) (Result, error) {
	res := Result{Format: FormatXray}
	idx := 0
	template := false
	for _, cfg := range configs {
		if _, ok := cfg["remnawave"]; ok {
			template = true
		}
		var proxies []fields
		for _, o := range cfg.list("outbounds") {
			if !xrayNotServers[strings.ToLower(o.str("protocol"))] {
				proxies = append(proxies, o)
			}
		}
		selectors := xrayBalancerSelectors(cfg)
		for _, o := range proxies {
			idx++
			n, err := xrayOutbound(o, cfg.str("remarks", "remark"), len(proxies) == 1)
			if err != nil {
				res.Skipped = append(res.Skipped, Skipped{Index: idx, Kind: strings.ToLower(o.str("protocol")), Reason: err.Error()})
				continue
			}
			res.Nodes = append(res.Nodes, n)
			if inBalancer(o.str("tag"), selectors) {
				res.Auto = append(res.Auto, n.Fingerprint())
			}
		}
	}
	if len(res.Nodes) == 0 && len(res.Skipped) == 0 && template {
		return res, errors.New("this is a Remnawave template, not a subscription: the panel adds the servers when it serves it to an app")
	}
	return res, nil
}

// xrayBalancerSelectors returns what the config's balancers select from:
// outbound tags, of which Xray takes every outbound whose tag starts with one.
func xrayBalancerSelectors(cfg fields) []string {
	var out []string
	for _, b := range cfg.sub("routing").list("balancers") {
		for _, sel := range b.strs("selector") {
			if sel != "" {
				out = append(out, sel)
			}
		}
	}
	return out
}

func inBalancer(tag string, selectors []string) bool {
	for _, sel := range selectors {
		if strings.HasPrefix(tag, sel) {
			return true
		}
	}
	return false
}

// xrayOutbound converts one proxy outbound. remarks is the config's own
// name, used when the outbound's tag is the generic "proxy" and the config
// holds one server only.
func xrayOutbound(o fields, remarks string, only bool) (node.Node, error) {
	proto := strings.ToLower(o.str("protocol"))
	settings := o.sub("settings")
	n := node.Node{Name: strings.TrimSpace(o.str("remarks", "name"))}
	if n.Name == "" {
		tag := strings.TrimSpace(o.str("tag"))
		if (tag == "" || tag == "proxy") && only {
			tag = strings.TrimSpace(remarks)
		}
		n.Name = tag
	}
	var err error
	switch proto {
	case "vless", "vmess":
		n.Protocol = node.Protocol(proto)
		// A server with its users, or, in the newer flat form, both in settings.
		srv, user := settings, settings
		if vnext := settings.list("vnext"); len(vnext) > 0 {
			srv = vnext[0]
			user = fields{}
			if users := srv.list("users"); len(users) > 0 {
				user = users[0]
			}
		}
		n.Server = strings.TrimSpace(srv.str("address"))
		if n.Port, err = parsePort(srv.str("port")); err != nil {
			return n, err
		}
		n.UUID = user.str("id")
		if proto == "vless" {
			n.Flow = user.str("flow")
			if e := user.str("encryption"); e != "none" {
				n.Encryption = e
			}
		} else {
			n.AlterID = user.int("alterId")
			if n.Cipher = user.str("security"); n.Cipher == "" {
				n.Cipher = "auto"
			}
		}
	case "trojan", "shadowsocks", "ss":
		srv := settings
		if servers := settings.list("servers"); len(servers) > 0 {
			srv = servers[0]
		}
		n.Server = strings.TrimSpace(srv.str("address"))
		if n.Port, err = parsePort(srv.str("port")); err != nil {
			return n, err
		}
		n.Password = srv.str("password")
		if proto == "trojan" {
			n.Protocol = node.Trojan
		} else {
			n.Protocol = node.Shadowsocks
			n.Cipher = srv.str("method")
		}
	case "wireguard":
		return xrayWireGuard(n, settings)
	default:
		return n, fmt.Errorf("unsupported protocol %q", proto)
	}
	ss := o.sub("streamSettings")
	if n.Transport, err = xrayTransport(ss); err != nil {
		return n, err
	}
	if n.TLS, err = xrayTLS(ss); err != nil {
		return n, err
	}
	return n, finish(&n)
}

func xrayWireGuard(n node.Node, s fields) (node.Node, error) {
	n.Protocol = node.WireGuard
	wg := &node.WireGuardOptions{PrivateKey: s.str("secretKey"), MTU: s.int("mtu")}
	peers := s.list("peers")
	if len(peers) == 0 {
		return n, errors.New("wireguard: no peer")
	}
	peer := peers[0]
	host, port, ok := splitEndpoint(peer.str("endpoint"))
	if !ok {
		return n, errors.New("wireguard: the peer has no endpoint")
	}
	n.Server = host
	var err error
	if n.Port, err = parsePort(port); err != nil {
		return n, err
	}
	wg.PeerPublicKey = peer.str("publicKey")
	wg.PreSharedKey = peer.str("preSharedKey")
	if wg.Address, err = parsePrefixes(s.strs("address")); err != nil {
		return n, err
	}
	for _, r := range s.strs("reserved") {
		var b uint8
		if _, err := fmt.Sscan(r, &b); err != nil {
			return n, errors.New("wireguard: invalid reserved bytes")
		}
		wg.Reserved = append(wg.Reserved, b)
	}
	n.WireGuard = wg
	return n, finish(&n)
}

// splitEndpoint splits "host:port" and "[v6]:port".
func splitEndpoint(s string) (host, port string, ok bool) {
	i := strings.LastIndex(s, ":")
	if i <= 0 {
		return "", "", false
	}
	return strings.Trim(s[:i], "[]"), s[i+1:], true
}

// xrayTransport reads streamSettings' network and its settings.
func xrayTransport(ss fields) (node.Transport, error) {
	switch network := strings.ToLower(ss.str("network")); network {
	case "", "tcp", "raw":
		t := node.Transport{Network: node.NetTCP}
		tcp := ss.sub("tcpSettings")
		if len(tcp) == 0 {
			tcp = ss.sub("rawSettings")
		}
		h := tcp.sub("header")
		if h.str("type") == "http" {
			t.HeaderType = "http"
			req := h.sub("request")
			if paths := req.strs("path"); len(paths) > 0 {
				t.Path = paths[0]
			}
			if hosts := req.sub("headers").strs("Host"); len(hosts) > 0 {
				t.Host = hosts[0]
			}
		}
		return t, nil
	case "ws", "websocket":
		w := ss.sub("wsSettings")
		t := node.Transport{Network: node.NetWS, Host: w.str("host")}
		if t.Host == "" {
			t.Host = w.sub("headers").str("Host", "host")
		}
		t.Path, t.EarlyData = splitEarlyData(w.str("path"))
		return t, nil
	case "grpc":
		g := ss.sub("grpcSettings")
		t := node.Transport{Network: node.NetGRPC, ServiceName: g.str("serviceName"), Host: g.str("authority")}
		if g.bool("multiMode") {
			t.Mode = "multi"
		}
		return t, nil
	case "http", "h2":
		h := ss.sub("httpSettings")
		t := node.Transport{Network: node.NetHTTP, Path: h.str("path")}
		if hosts := h.strs("host"); len(hosts) > 0 {
			t.Host = hosts[0]
		}
		return t, nil
	case "httpupgrade":
		h := ss.sub("httpupgradeSettings")
		return node.Transport{Network: node.NetHTTPUpgrade, Path: h.str("path"), Host: h.str("host")}, nil
	case "xhttp", "splithttp":
		x := ss.sub("xhttpSettings")
		if len(x) == 0 {
			x = ss.sub("splithttpSettings")
		}
		t := node.Transport{Network: node.NetXHTTP, Path: x.str("path"), Host: x.str("host"), Mode: x.str("mode")}
		if t.Mode == "auto" {
			t.Mode = ""
		}
		if extra, ok := x["extra"]; ok && extra != nil {
			b, err := json.Marshal(extra)
			if err != nil {
				return t, fmt.Errorf("xhttp: invalid extra: %w", err)
			}
			if string(b) != "{}" {
				t.Extra = string(b)
			}
		}
		return t, nil
	default:
		return node.Transport{}, fmt.Errorf("unsupported transport %q", network)
	}
}

// xrayTLS reads streamSettings' security: TLS, REALITY or none.
func xrayTLS(ss fields) (*node.TLS, error) {
	switch security := strings.ToLower(ss.str("security")); security {
	case "", "none":
		return nil, nil
	case "tls", "xtls":
		s := ss.sub("tlsSettings")
		return &node.TLS{
			ServerName:  s.str("serverName"),
			ALPN:        s.strs("alpn"),
			Insecure:    s.bool("allowInsecure"),
			Fingerprint: xrayFingerprint(s),
		}, nil
	case "reality":
		s := ss.sub("realitySettings")
		return &node.TLS{
			ServerName:  s.str("serverName"),
			Fingerprint: xrayFingerprint(s),
			Reality:     &node.Reality{PublicKey: s.str("publicKey"), ShortID: s.str("shortId"), SpiderX: s.str("spiderX")},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported security %q", security)
	}
}

func xrayFingerprint(s fields) string {
	if fp := s.str("fingerprint"); fp != "none" {
		return fp
	}
	return ""
}
