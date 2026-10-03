package core

import (
	"encoding/json"
	"fmt"
	"strings"

	"coreshift/engine/internal/node"
)

type singBox struct{}

var singBoxFeatures = features(
	[]node.Protocol{node.VLESS, node.VMess, node.Trojan, node.Shadowsocks, node.Hysteria2, node.TUIC, node.AnyTLS, node.WireGuard},
	[]node.Network{node.NetWS, node.NetGRPC, node.NetHTTP, node.NetHTTPUpgrade},
	"flow:xtls-rprx-vision",
	FeatVMessAlterID, FeatReality, FeatHysteria2PortHop,
	"ss-plugin:obfs-local", "ss-plugin:v2ray-plugin",
)

func (singBox) Kind() Kind                  { return SingBox }
func (singBox) ConfigName() string          { return "config.json" }
func (singBox) Supports(n *node.Node) error { return checkSupport(SingBox, singBoxFeatures, n) }

func (singBox) RunArgs(configPath, workDir string) []string {
	return []string{"run", "-c", configPath, "-D", workDir}
}
func (singBox) CheckArgs(configPath, workDir string) []string {
	return []string{"check", "-c", configPath, "-D", workDir}
}

func (singBox) VersionArgs() []string { return []string{"version"} }

func (s singBox) Render(n *node.Node, o Options) ([]byte, error) {
	if err := s.Supports(n); err != nil {
		return nil, err
	}
	o = o.withDefaults()
	level := o.LogLevel
	if level == "none" {
		level = "panic"
	}
	in := obj{
		"type":        "socks",
		"tag":         "socks-in",
		"listen":      o.Listen.Addr().String(),
		"listen_port": o.Listen.Port(),
	}
	if o.Auth.Set() {
		in["users"] = []any{obj{"username": o.Auth.User, "password": o.Auth.Pass}}
	}
	cfg := obj{
		"log":      obj{"level": level, "timestamp": true},
		"inbounds": []any{in},
		"route":    obj{"final": "proxy"},
	}
	if o.Stats.IsValid() {
		cfg["experimental"] = obj{"clash_api": obj{"external_controller": o.Stats.String(), "secret": o.StatsSecret}}
	}
	direct := obj{"type": "direct", "tag": "direct"}
	if n.Protocol == node.WireGuard {
		ep, err := singBoxWireGuard(n, o)
		if err != nil {
			return nil, err
		}
		cfg["endpoints"] = []any{ep}
		cfg["outbounds"] = []any{direct}
	} else {
		out, err := singBoxOutbound(n, o)
		if err != nil {
			return nil, err
		}
		cfg["outbounds"] = []any{out, direct}
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func singBoxOutbound(n *node.Node, o Options) (obj, error) {
	addr, sni := target(n, o)
	out := obj{"tag": "proxy", "server": addr, "server_port": n.Port}
	switch n.Protocol {
	case node.VLESS:
		out["type"] = "vless"
		out["uuid"] = n.UUID
		if n.Flow != "" {
			out["flow"] = n.Flow
		}
		out["packet_encoding"] = "xudp"
	case node.VMess:
		out["type"] = "vmess"
		out["uuid"] = n.UUID
		out["security"] = orDefault(n.Cipher, "auto")
		if n.AlterID > 0 {
			out["alter_id"] = n.AlterID
		}
	case node.Trojan:
		out["type"] = "trojan"
		out["password"] = n.Password
	case node.Shadowsocks:
		out["type"] = "shadowsocks"
		out["method"] = n.Cipher
		out["password"] = n.Password
		if p := n.Shadowsocks; p != nil && p.Plugin != "" {
			out["plugin"] = p.Plugin
			out["plugin_opts"] = sip003Clean(p.Plugin, p.PluginOpts)
		}
	case node.Hysteria2:
		out["type"] = "hysteria2"
		out["password"] = n.Password
		if h := n.Hysteria2; h != nil {
			if h.Obfs != "" {
				out["obfs"] = obj{"type": h.Obfs, "password": h.ObfsPassword}
			}
			if h.UpMbps > 0 {
				out["up_mbps"] = h.UpMbps
			}
			if h.DownMbps > 0 {
				out["down_mbps"] = h.DownMbps
			}
			if h.Ports != "" {
				out["server_ports"] = singBoxPortRanges(h.Ports)
			}
		}
	case node.TUIC:
		out["type"] = "tuic"
		out["uuid"] = n.UUID
		out["password"] = n.Password
		if t := n.TUIC; t != nil {
			if t.CongestionControl != "" {
				out["congestion_control"] = t.CongestionControl
			}
			if t.UDPRelayMode != "" {
				out["udp_relay_mode"] = t.UDPRelayMode
			}
		}
	case node.AnyTLS:
		out["type"] = "anytls"
		out["password"] = n.Password
	default:
		return nil, fmt.Errorf("sing-box: unexpected protocol %q", n.Protocol)
	}
	if tr := singBoxTransport(n.Transport); tr != nil {
		out["transport"] = tr
	}
	if n.TLS != nil {
		tls := singBoxTLS(n.TLS, sni)
		// Hysteria2 and TUIC run TLS inside QUIC: no ClientHello over TCP
		// to split.
		if o.Fragment && n.Protocol != node.Hysteria2 && n.Protocol != node.TUIC {
			tls["fragment"] = true
		}
		out["tls"] = tls
	}
	return out, nil
}

func singBoxTLS(t *node.TLS, sni string) obj {
	tls := obj{"enabled": true}
	if sni != "" {
		tls["server_name"] = sni
	}
	if len(t.ALPN) > 0 {
		tls["alpn"] = t.ALPN
	}
	if t.Insecure {
		tls["insecure"] = true
	}
	fp := t.Fingerprint
	if fp == "" && t.Reality != nil {
		fp = "chrome" // REALITY requires uTLS in sing-box
	}
	if fp != "" {
		tls["utls"] = obj{"enabled": true, "fingerprint": fp}
	}
	if r := t.Reality; r != nil {
		tls["reality"] = obj{"enabled": true, "public_key": r.PublicKey, "short_id": r.ShortID}
	}
	return tls
}

func singBoxTransport(t node.Transport) obj {
	switch t.Network {
	case node.NetWS:
		ws := obj{"type": "ws", "path": t.Path}
		if t.Host != "" {
			ws["headers"] = obj{"Host": t.Host}
		}
		if t.EarlyData > 0 {
			ws["max_early_data"] = t.EarlyData
			ws["early_data_header_name"] = "Sec-WebSocket-Protocol"
		}
		return ws
	case node.NetGRPC:
		return obj{"type": "grpc", "service_name": t.ServiceName}
	case node.NetHTTP:
		h := obj{"type": "http", "path": t.Path}
		if t.Host != "" {
			h["host"] = []string{t.Host}
		}
		return h
	case node.NetHTTPUpgrade:
		h := obj{"type": "httpupgrade", "path": t.Path}
		if t.Host != "" {
			h["host"] = t.Host
		}
		return h
	}
	return nil
}

// singBoxPortRanges converts "443,20000-30000" to ["443:443", "20000:30000"].
func singBoxPortRanges(spec string) []string {
	var out []string
	for _, p := range strings.Split(spec, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if lo, hi, ok := strings.Cut(p, "-"); ok {
			out = append(out, lo+":"+hi)
		} else {
			out = append(out, p+":"+p)
		}
	}
	return out
}

func singBoxWireGuard(n *node.Node, o Options) (obj, error) {
	w := n.WireGuard
	if w == nil {
		return nil, errNoWireGuard
	}
	addr, _ := target(n, o)
	peer := obj{
		"address":     addr,
		"port":        n.Port,
		"public_key":  w.PeerPublicKey,
		"allowed_ips": []string{"0.0.0.0/0", "::/0"},
	}
	if w.PreSharedKey != "" {
		peer["pre_shared_key"] = w.PreSharedKey
	}
	if len(w.Reserved) > 0 {
		peer["reserved"] = ints(w.Reserved)
	}
	ep := obj{
		"type":        "wireguard",
		"tag":         "proxy",
		"address":     prefixStrings(w.Address),
		"private_key": w.PrivateKey,
		"peers":       []any{peer},
	}
	if w.MTU > 0 {
		ep["mtu"] = w.MTU
	}
	return ep, nil
}
