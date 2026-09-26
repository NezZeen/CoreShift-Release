package core

import (
	"encoding/json"
	"fmt"
	"strings"

	"coreshift/engine/internal/node"
)

type xray struct{}

// xrayFeatures, checked against Xray 26.3.27:
//   - HTTP/2 transport was removed (Xray says it "migrated to XHTTP stream-one").
//   - Hysteria2 exists as protocol "hysteria" + transport "hysteria", but
//     `xray run -test` ignores unknown fields, so the schema is not verified yet.
var xrayFeatures = features(
	[]node.Protocol{node.VLESS, node.VMess, node.Trojan, node.Shadowsocks, node.WireGuard},
	[]node.Network{node.NetWS, node.NetGRPC, node.NetHTTPUpgrade, node.NetXHTTP},
	"flow:xtls-rprx-vision", "flow:xtls-rprx-vision-udp443",
	FeatVLESSEncryption, FeatTCPHTTPHeader, FeatReality,
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
	cfg := obj{
		// The access log would record every destination the user visits.
		"log": obj{"loglevel": level, "access": "none"},
		"inbounds": []any{obj{
			"tag":      "socks-in",
			"listen":   o.Listen.Addr().String(),
			"port":     o.Listen.Port(),
			"protocol": "socks",
			"settings": obj{"auth": "noauth", "udp": true},
		}},
		"outbounds": []any{out, obj{"tag": "direct", "protocol": "freedom"}},
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
		if t.Extra != "" {
			var extra any
			if err := json.Unmarshal([]byte(t.Extra), &extra); err != nil {
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
		ts := obj{"serverName": sni}
		if len(tls.ALPN) > 0 {
			ts["alpn"] = tls.ALPN
		}
		if tls.Insecure {
			ts["allowInsecure"] = true
		}
		if tls.Fingerprint != "" {
			ts["fingerprint"] = tls.Fingerprint
		}
		s["tlsSettings"] = ts
	}
	return s, nil
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
