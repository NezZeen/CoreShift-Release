package core

import (
	"fmt"

	"go.yaml.in/yaml/v3"

	"coreshift/engine/internal/node"
)

type mihomo struct{}

// mihomoFeatures leaves out XHTTP and VLESS encryption: `mihomo -t` (1.19.31)
// accepts any "network" value, so a passing check cannot confirm support.
var mihomoFeatures = features(
	[]node.Protocol{node.VLESS, node.VMess, node.Trojan, node.Shadowsocks, node.Hysteria2, node.TUIC, node.AnyTLS, node.WireGuard},
	[]node.Network{node.NetWS, node.NetGRPC, node.NetHTTP, node.NetHTTPUpgrade},
	"flow:xtls-rprx-vision",
	FeatVMessAlterID, FeatTCPHTTPHeader, FeatReality, FeatHysteria2PortHop, FeatTLSInsecure, FeatTLSPin,
	"ss-plugin:obfs-local", "ss-plugin:v2ray-plugin",
)

func (mihomo) Kind() Kind                  { return Mihomo }
func (mihomo) ConfigName() string          { return "config.yaml" }
func (mihomo) Supports(n *node.Node) error { return checkSupport(Mihomo, mihomoFeatures, n) }

func (mihomo) RunArgs(configPath, workDir string) []string {
	return []string{"-f", configPath, "-d", workDir}
}
func (mihomo) CheckArgs(configPath, workDir string) []string {
	return []string{"-t", "-f", configPath, "-d", workDir}
}

func (mihomo) VersionArgs() []string { return []string{"-v"} }

func (m mihomo) Render(n *node.Node, o Options) ([]byte, error) {
	if err := m.Supports(n); err != nil {
		return nil, err
	}
	o = o.withDefaults()
	p, err := mihomoProxy(n, o)
	if err != nil {
		return nil, err
	}
	level := map[string]string{"warn": "warning", "none": "silent"}[o.LogLevel]
	if level == "" {
		level = o.LogLevel
	}
	cfg := obj{
		"allow-lan": false,
		"mode":      "rule",
		"log-level": level,
		"ipv6":      true,
		"profile":   obj{"store-selected": false, "store-fake-ip": false},
		"proxies":   []any{p},
		"rules":     []string{"MATCH,proxy"},
	}
	if o.Auth.Set() {
		// A listener of its own: its users replace the global
		// authentication, which loopback clients may skip.
		cfg["listeners"] = []any{obj{
			"name": "socks-in", "type": "socks", "listen": o.Listen.Addr().String(), "port": o.Listen.Port(), "udp": true,
			"users": []any{obj{"username": o.Auth.User, "password": o.Auth.Pass}},
		}}
	} else {
		cfg["socks-port"], cfg["bind-address"] = o.Listen.Port(), o.Listen.Addr().String()
	}
	if o.Stats.IsValid() {
		cfg["external-controller"] = o.Stats.String()
		cfg["secret"] = o.StatsSecret
	}
	return yaml.Marshal(cfg)
}

func mihomoProxy(n *node.Node, o Options) (obj, error) {
	addr, sni := target(n, o)
	p := obj{"name": "proxy", "server": addr, "port": n.Port, "udp": true}
	sniKey := "sni"
	switch n.Protocol {
	case node.VLESS:
		p["type"] = "vless"
		p["uuid"] = n.UUID
		if n.Flow != "" {
			p["flow"] = n.Flow
		}
		p["tls"] = n.TLS != nil
		sniKey = "servername"
	case node.VMess:
		p["type"] = "vmess"
		p["uuid"] = n.UUID
		p["alterId"] = n.AlterID
		p["cipher"] = orDefault(n.Cipher, "auto")
		p["tls"] = n.TLS != nil
		sniKey = "servername"
	case node.Trojan:
		p["type"] = "trojan"
		p["password"] = n.Password
	case node.Shadowsocks:
		p["type"] = "ss"
		p["cipher"] = n.Cipher
		p["password"] = n.Password
		if pl := n.Shadowsocks; pl != nil && pl.Plugin != "" {
			name, opts := mihomoPlugin(pl)
			p["plugin"] = name
			p["plugin-opts"] = opts
		}
	case node.Hysteria2:
		p["type"] = "hysteria2"
		p["password"] = n.Password
		if h := n.Hysteria2; h != nil {
			if h.Obfs != "" {
				p["obfs"] = h.Obfs
				p["obfs-password"] = h.ObfsPassword
			}
			if h.UpMbps > 0 {
				p["up"] = h.UpMbps
			}
			if h.DownMbps > 0 {
				p["down"] = h.DownMbps
			}
			if h.Ports != "" {
				p["ports"] = h.Ports
			}
		}
	case node.TUIC:
		p["type"] = "tuic"
		p["uuid"] = n.UUID
		p["password"] = n.Password
		if t := n.TUIC; t != nil {
			if t.CongestionControl != "" {
				p["congestion-controller"] = t.CongestionControl
			}
			if t.UDPRelayMode != "" {
				p["udp-relay-mode"] = t.UDPRelayMode
			}
		}
	case node.AnyTLS:
		p["type"] = "anytls"
		p["password"] = n.Password
	case node.WireGuard:
		w := n.WireGuard
		if w == nil {
			return nil, errNoWireGuard
		}
		p["type"] = "wireguard"
		p["private-key"] = w.PrivateKey
		p["public-key"] = w.PeerPublicKey
		if w.PreSharedKey != "" {
			p["pre-shared-key"] = w.PreSharedKey
		}
		for _, a := range w.Address {
			if a.Addr().Is4() {
				p["ip"] = a.Addr().String()
			} else {
				p["ipv6"] = a.Addr().String()
			}
		}
		if len(w.Reserved) > 0 {
			p["reserved"] = ints(w.Reserved)
		}
		if w.MTU > 0 {
			p["mtu"] = w.MTU
		}
		return p, nil
	default:
		return nil, fmt.Errorf("mihomo: unexpected protocol %q", n.Protocol)
	}

	mihomoTransport(p, n.Transport)
	if t := n.TLS; t != nil {
		if sni != "" {
			p[sniKey] = sni
		}
		if len(t.ALPN) > 0 {
			p["alpn"] = t.ALPN
		}
		if t.PinSHA256 != "" && t.Reality == nil {
			// The certificate's SHA-256, checked in place of its signature.
			p["fingerprint"] = t.PinSHA256
		} else if t.Insecure {
			p["skip-cert-verify"] = true
		}
		fp := t.Fingerprint
		if fp == "" && t.Reality != nil {
			fp = "chrome"
		}
		if fp != "" {
			p["client-fingerprint"] = fp
		}
		if r := t.Reality; r != nil {
			p["reality-opts"] = obj{"public-key": r.PublicKey, "short-id": r.ShortID}
		}
	}
	return p, nil
}

func mihomoTransport(p obj, t node.Transport) {
	switch t.Network {
	case "", node.NetTCP:
		if t.HeaderType == "http" {
			p["network"] = "http"
			h := obj{"path": []string{orDefault(t.Path, "/")}}
			if t.Host != "" {
				h["headers"] = obj{"Host": []string{t.Host}}
			}
			p["http-opts"] = h
		}
	case node.NetWS, node.NetHTTPUpgrade:
		p["network"] = "ws"
		ws := obj{"path": orDefault(t.Path, "/")}
		if t.Host != "" {
			ws["headers"] = obj{"Host": t.Host}
		}
		if t.Network == node.NetHTTPUpgrade {
			ws["v2ray-http-upgrade"] = true
		} else if t.EarlyData > 0 {
			ws["max-early-data"] = t.EarlyData
			ws["early-data-header-name"] = "Sec-WebSocket-Protocol"
		}
		p["ws-opts"] = ws
	case node.NetGRPC:
		p["network"] = "grpc"
		p["grpc-opts"] = obj{"grpc-service-name": t.ServiceName}
	case node.NetHTTP:
		p["network"] = "h2"
		h := obj{"path": orDefault(t.Path, "/")}
		if t.Host != "" {
			h["host"] = []string{t.Host}
		}
		p["h2-opts"] = h
	}
}

// mihomoPlugin converts a SIP003 plugin to mihomo's plugin name and options.
func mihomoPlugin(pl *node.ShadowsocksOptions) (string, obj) {
	opts := sip003Opts(pl.PluginOpts)
	switch pl.Plugin {
	case "obfs-local":
		return "obfs", obj{"mode": opts["obfs"], "host": opts["obfs-host"]}
	case "v2ray-plugin":
		o := obj{"mode": orDefault(opts["mode"], "websocket")}
		if _, ok := opts["tls"]; ok {
			o["tls"] = true
		}
		if h := opts["host"]; h != "" {
			o["host"] = h
		}
		if p := opts["path"]; p != "" {
			o["path"] = p
		}
		return "v2ray-plugin", o
	}
	// No other plugin passes Supports; none gets options it does not know.
	o := obj{}
	for k, v := range sip003Opts(sip003Clean(pl.Plugin, pl.PluginOpts)) {
		o[k] = v
	}
	return pl.Plugin, o
}
