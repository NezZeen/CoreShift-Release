package core

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"coreshift/engine/internal/node"
)

type xray struct{}

// xrayFeatures, checked against Xray 26.3.27:
//   - HTTP/2 transport was removed (Xray says it "migrated to XHTTP stream-one").
//   - "allowInsecure" was removed: since 2026-06-01 Xray refuses to start
//     with it. "pinnedPeerCertSha256" (a certificate's SHA-256) replaces it.
//   - Hysteria2 is protocol "hysteria" with settings {version: 2, address,
//     port} over transport "hysteria" with hysteriaSettings {version: 2,
//     auth} and TLS; Salamander is a finalmask UDP mask and Brutal rates and
//     port hopping are finalmask.quicParams (brutalUp, brutalDown, udpHop).
//     Read from Xray's infra/conf (hysteria.go, transport_internet.go) at
//     v26.3.27, as `xray run -test` ignores unknown fields; it is also what
//     Remnawave's Xray JSON generator writes. Run on 127.0.0.1
//     (TestHysteria2Loopback): an Xray client gets through to both an Xray
//     and a sing-box Hysteria2 server, plain and with Salamander, with a
//     pinned self-signed certificate and with udpHop; a wrong auth or pin is
//     refused. udpHop without an interval makes Xray panic.
var xrayFeatures = features(
	[]node.Protocol{node.VLESS, node.VMess, node.Trojan, node.Shadowsocks, node.Hysteria2, node.WireGuard},
	[]node.Network{node.NetWS, node.NetGRPC, node.NetHTTPUpgrade, node.NetXHTTP},
	"flow:xtls-rprx-vision", "flow:xtls-rprx-vision-udp443",
	FeatVLESSEncryption, FeatTCPHTTPHeader, FeatReality, FeatHysteria2PortHop, FeatTLSPin,
)

func (xray) Kind() Kind                  { return Xray }
func (xray) ConfigName() string          { return "config.json" }
func (xray) Supports(n *node.Node) error { return checkSupport(Xray, xrayFeatures, n) }

func (xray) RunArgs(configPath, _ string) []string { return []string{"run", "-c", configPath} }
func (xray) CheckArgs(configPath, _ string) []string {
	return []string{"run", "-test", "-c", configPath}
}

func (xray) VersionArgs() []string { return []string{"version"} }

func (x xray) Render(n *node.Node, o Options) ([]byte, error) {
	if err := x.Supports(n); err != nil {
		return nil, err
	}
	o = o.withDefaults()
	out, err := xrayOutbound(n, o)
	if err != nil {
		return nil, err
	}
	level := map[string]string{"warn": "warning"}[o.LogLevel]
	if level == "" {
		level = o.LogLevel
	}
	socks := obj{"auth": "noauth", "udp": true}
	if o.Auth.Set() {
		socks = obj{"auth": "password", "accounts": []any{obj{"user": o.Auth.User, "pass": o.Auth.Pass}}, "udp": true}
	}
	cfg := obj{
		// The access log would record every destination the user visits.
		"log": obj{"loglevel": level, "access": "none"},
		"inbounds": []any{obj{
			"tag":      "socks-in",
			"listen":   o.Listen.Addr().String(),
			"port":     o.Listen.Port(),
			"protocol": "socks",
			"settings": socks,
		}},
		"outbounds": []any{out, obj{"tag": "direct", "protocol": "freedom"}},
	}
	if o.Fragment && xrayFragments(n) {
		// The proxy dials the server through a freedom outbound that cuts
		// the TLS ClientHello into records sent a moment apart.
		out["streamSettings"].(obj)["sockopt"] = obj{"dialerProxy": "fragment"}
		cfg["outbounds"] = append(cfg["outbounds"].([]any), obj{
			"tag":            "fragment",
			"protocol":       "freedom",
			"settings":       obj{"fragment": obj{"packets": "tlshello", "length": "100-200", "interval": "10-20"}},
			"streamSettings": obj{"sockopt": obj{"tcpNoDelay": true}},
		})
	}
	if o.Stats.IsValid() {
		cfg["stats"] = obj{}
		cfg["policy"] = obj{"system": obj{"statsInboundUplink": true, "statsInboundDownlink": true}}
		// Only the read-only stats service; the handler service could
		// change the core.
		cfg["api"] = obj{"tag": "api", "listen": o.Stats.String(), "services": []string{"StatsService"}}
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func xrayOutbound(n *node.Node, o Options) (obj, error) {
	addr, sni := target(n, o)
	server := func(extra obj) []any {
		s := obj{"address": addr, "port": n.Port}
		for k, v := range extra {
			s[k] = v
		}
		return []any{s}
	}
	var settings obj
	switch n.Protocol {
	case node.VLESS:
		enc := n.Encryption
		if enc == "" {
			enc = "none"
		}
		user := obj{"id": n.UUID, "encryption": enc}
		if n.Flow != "" {
			user["flow"] = n.Flow
		}
		settings = obj{"vnext": server(obj{"users": []any{user}})}
	case node.VMess:
		cipher := n.Cipher
		if cipher == "" {
			cipher = "auto"
		}
		settings = obj{"vnext": server(obj{"users": []any{obj{"id": n.UUID, "security": cipher}}})}
	case node.Trojan:
		settings = obj{"servers": server(obj{"password": n.Password})}
	case node.Shadowsocks:
		settings = obj{"servers": server(obj{"method": n.Cipher, "password": n.Password})}
	case node.WireGuard:
		w := n.WireGuard
		if w == nil {
			return nil, errNoWireGuard
		}
		peer := obj{"endpoint": hostPort(addr, n.Port), "publicKey": w.PeerPublicKey}
		if w.PreSharedKey != "" {
			peer["preSharedKey"] = w.PreSharedKey
		}
		settings = obj{"secretKey": w.PrivateKey, "address": prefixStrings(w.Address), "peers": []any{peer}}
		if len(w.Reserved) > 0 {
			settings["reserved"] = ints(w.Reserved)
		}
		if w.MTU > 0 {
			settings["mtu"] = w.MTU
		}
		return obj{"tag": "proxy", "protocol": "wireguard", "settings": settings}, nil
	case node.Hysteria2:
		stream, err := xrayHysteriaStream(n, sni)
		if err != nil {
			return nil, err
		}
		return obj{
			"tag":            "proxy",
			"protocol":       "hysteria",
			"settings":       obj{"version": 2, "address": addr, "port": n.Port},
			"streamSettings": stream,
		}, nil
	default:
		return nil, fmt.Errorf("xray: unexpected protocol %q", n.Protocol)
	}
	stream, err := xrayStream(n, sni)
	if err != nil {
		return nil, err
	}
	return obj{"tag": "proxy", "protocol": string(n.Protocol), "settings": settings, "streamSettings": stream}, nil
}

func xrayStream(n *node.Node, sni string) (obj, error) {
	t := n.Transport
	s := obj{}
	switch t.Network {
	case "", node.NetTCP:
		s["network"] = "raw"
		if t.HeaderType == "http" {
			req := obj{"path": []string{orDefault(t.Path, "/")}}
			if t.Host != "" {
				req["headers"] = obj{"Host": []string{t.Host}}
			}
			s["rawSettings"] = obj{"header": obj{"type": "http", "request": req}}
		}
	case node.NetWS:
		s["network"] = "ws"
		path := t.Path
		if t.EarlyData > 0 {
			sep := "?"
			if strings.Contains(path, "?") {
				sep = "&"
			}
			path += fmt.Sprintf("%sed=%d", sep, t.EarlyData)
		}
		s["wsSettings"] = withHost(obj{"path": path}, t.Host)
	case node.NetGRPC:
		s["network"] = "grpc"
		g := obj{"serviceName": t.ServiceName}
		if t.Mode == "multi" {
			g["multiMode"] = true
		}
		if t.Host != "" {
			g["authority"] = t.Host
		}
		s["grpcSettings"] = g
	case node.NetHTTPUpgrade:
		s["network"] = "httpupgrade"
		s["httpupgradeSettings"] = withHost(obj{"path": t.Path}, t.Host)
	case node.NetXHTTP:
		s["network"] = "xhttp"
		x := withHost(obj{"path": t.Path}, t.Host)
		if t.Mode != "" {
			x["mode"] = t.Mode
		}
		// Checked here too: nodes saved by earlier versions kept it as the
		// subscription sent it.
		clean, err := node.SanitizeXHTTPExtra(t.Extra)
		if err != nil {
			return nil, fmt.Errorf("xray: %w", err)
		}
		if clean != "" {
			var extra any
			if err := json.Unmarshal([]byte(clean), &extra); err != nil {
				return nil, fmt.Errorf("xray: xhttp extra is not valid JSON")
			}
			x["extra"] = extra
		}
		s["xhttpSettings"] = x
	default:
		return nil, fmt.Errorf("xray: unexpected transport %q", t.Network)
	}

	tls := n.TLS
	switch {
	case tls == nil:
		s["security"] = "none"
	case tls.Reality != nil:
		s["security"] = "reality"
		r := obj{
			"serverName":  sni,
			"fingerprint": orDefault(tls.Fingerprint, "chrome"),
			"publicKey":   tls.Reality.PublicKey,
			"shortId":     tls.Reality.ShortID,
		}
		if tls.Reality.SpiderX != "" {
			r["spiderX"] = tls.Reality.SpiderX
		}
		s["realitySettings"] = r
	default:
		s["security"] = "tls"
		ts := xrayTLSSettings(tls, sni)
		if tls.Fingerprint != "" {
			ts["fingerprint"] = tls.Fingerprint
		}
		s["tlsSettings"] = ts
	}
	return s, nil
}

// xrayTLSSettings are the tlsSettings every TLS client shares. Insecure
// nodes never get here (FeatTLSInsecure): Xray no longer has allowInsecure.
func xrayTLSSettings(tls *node.TLS, sni string) obj {
	ts := obj{"serverName": sni}
	if len(tls.ALPN) > 0 {
		ts["alpn"] = tls.ALPN
	}
	if tls.PinSHA256 != "" {
		ts["pinnedPeerCertSha256"] = tls.PinSHA256
	}
	return ts
}

// xrayHysteriaStream is the "hysteria" transport of a Hysteria2 node: QUIC,
// always TLS (without uTLS, which has no QUIC form), with Salamander, the
// Brutal rates and port hopping in finalmask.
func xrayHysteriaStream(n *node.Node, sni string) (obj, error) {
	tls := n.TLS
	if tls == nil {
		tls = &node.TLS{}
		if _, err := netip.ParseAddr(n.Server); err != nil {
			sni = n.Server
		}
	}
	s := obj{
		"network":          "hysteria",
		"security":         "tls",
		"tlsSettings":      xrayTLSSettings(tls, sni),
		"hysteriaSettings": obj{"version": 2, "auth": n.Password},
	}
	h := n.Hysteria2
	if h == nil {
		return s, nil
	}
	mask := obj{}
	switch h.Obfs {
	case "":
	case "salamander":
		mask["udp"] = []any{obj{"type": "salamander", "settings": obj{"password": h.ObfsPassword}}}
	default:
		return nil, fmt.Errorf("xray: hysteria2 obfuscation %q is not supported", h.Obfs)
	}
	quic := obj{}
	if h.UpMbps > 0 {
		quic["brutalUp"] = fmt.Sprintf("%d mbps", h.UpMbps)
	}
	if h.DownMbps > 0 {
		quic["brutalDown"] = fmt.Sprintf("%d mbps", h.DownMbps)
	}
	if h.Ports != "" {
		ports, err := node.NormalizePorts(h.Ports)
		if err != nil {
			return nil, fmt.Errorf("xray: hysteria2 %w", err)
		}
		// Without an interval Xray 26.3.27 panics (a zero ticker); 30 s is
		// what sing-box and mihomo hop at by default.
		quic["udpHop"] = obj{"ports": ports, "interval": 30}
	}
	if len(quic) > 0 {
		mask["quicParams"] = quic
	}
	if len(mask) > 0 {
		s["finalmask"] = mask
	}
	return s, nil
}

// xrayFragments reports whether n has a TLS ClientHello over TCP to split:
// not WireGuard nor Hysteria2 (QUIC), nor plain connections, nor XHTTP over
// HTTP/3 (QUIC).
func xrayFragments(n *node.Node) bool {
	if n.Protocol == node.WireGuard || n.Protocol == node.Hysteria2 || n.TLS == nil {
		return false
	}
	return n.Transport.Network != node.NetXHTTP || !slices.Contains(n.TLS.ALPN, "h3")
}

func withHost(o obj, host string) obj {
	if host != "" {
		o["host"] = host
	}
	return o
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
