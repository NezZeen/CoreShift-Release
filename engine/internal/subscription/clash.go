package subscription

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"coreshift/engine/internal/node"
)

// clashProxy converts one entry of a Clash / mihomo "proxies:" list.
func clashProxy(p fields) (node.Node, error) {
	port, err := parsePort(p.str("port"))
	if err != nil {
		return node.Node{}, err
	}
	n := node.Node{Name: strings.TrimSpace(p.str("name")), Server: p.str("server"), Port: port}
	tlsOn := false
	switch typ := p.str("type"); typ {
	case "vless":
		n.Protocol = node.VLESS
		n.UUID = p.str("uuid")
		n.Flow = p.str("flow")
		if e := p.str("encryption"); e != "none" {
			n.Encryption = e
		}
		tlsOn = p.bool("tls")
	case "vmess":
		n.Protocol = node.VMess
		n.UUID = p.str("uuid")
		n.AlterID = p.int("alterId")
		n.Cipher = p.str("cipher")
		if n.Cipher == "" {
			n.Cipher = "auto"
		}
		tlsOn = p.bool("tls")
	case "trojan":
		n.Protocol = node.Trojan
		n.Password = p.str("password")
		tlsOn = true
	case "ss":
		n.Protocol = node.Shadowsocks
		n.Cipher = p.str("cipher")
		n.Password = p.str("password")
		if plugin := p.str("plugin"); plugin != "" {
			n.Shadowsocks = clashPlugin(plugin, p.sub("plugin-opts"))
		}
	case "hysteria2":
		n.Protocol = node.Hysteria2
		n.Password = p.str("password")
		opts := node.Hysteria2Options{
			Obfs: p.str("obfs"), ObfsPassword: p.str("obfs-password"),
			UpMbps: p.int("up"), DownMbps: p.int("down"), Ports: p.str("ports"),
		}
		if opts != (node.Hysteria2Options{}) {
			n.Hysteria2 = &opts
		}
		tlsOn = true
	case "tuic":
		n.Protocol = node.TUIC
		n.UUID = p.str("uuid")
		n.Password = p.str("password")
		n.TUIC = &node.TUICOptions{
			CongestionControl: p.str("congestion-controller"),
			UDPRelayMode:      p.str("udp-relay-mode"),
		}
		tlsOn = true
	case "anytls":
		n.Protocol = node.AnyTLS
		n.Password = p.str("password")
		tlsOn = true
	case "wireguard":
		n.Protocol = node.WireGuard
		if n.WireGuard, err = clashWireGuard(p); err != nil {
			return n, err
		}
	default:
		return n, fmt.Errorf("unsupported type %q", typ)
	}

	if n.Transport, err = clashTransport(p); err != nil {
		return n, err
	}
	if tlsOn {
		n.TLS = &node.TLS{
			ServerName:  p.str("servername", "sni"),
			ALPN:        p.strs("alpn"),
			Insecure:    p.bool("skip-cert-verify"),
			Fingerprint: p.str("client-fingerprint"),
		}
		if r := p.sub("reality-opts"); len(r) > 0 {
			n.TLS.Reality = &node.Reality{PublicKey: r.str("public-key"), ShortID: r.str("short-id")}
		}
	}
	return n, finish(&n)
}

func clashTransport(p fields) (node.Transport, error) {
	switch netw := p.str("network"); netw {
	case "", "tcp":
		return node.Transport{}, nil
	case "ws":
		o := p.sub("ws-opts")
		t := node.Transport{Network: node.NetWS, Path: o.str("path"), Host: o.sub("headers").str("Host", "host")}
		if o.bool("v2ray-http-upgrade") {
			t.Network = node.NetHTTPUpgrade
		} else {
			t.EarlyData = uint32(o.int("max-early-data"))
		}
		return t, nil
	case "grpc":
		return node.Transport{Network: node.NetGRPC, ServiceName: p.sub("grpc-opts").str("grpc-service-name")}, nil
	case "h2":
		o := p.sub("h2-opts")
		t := node.Transport{Network: node.NetHTTP, Path: o.str("path")}
		if hosts := o.strs("host"); len(hosts) > 0 {
			t.Host = hosts[0]
		}
		return t, nil
	case "http":
		// Clash "http" is HTTP/1.1 header obfuscation over plain tcp.
		o := p.sub("http-opts")
		t := node.Transport{HeaderType: "http"}
		if paths := o.strs("path"); len(paths) > 0 {
			t.Path = paths[0]
		}
		if hosts := o.sub("headers").strs("Host"); len(hosts) > 0 {
			t.Host = hosts[0]
		}
		return t, nil
	case "xhttp":
		o := p.sub("xhttp-opts")
		return node.Transport{Network: node.NetXHTTP, Path: o.str("path"), Host: o.str("host"), Mode: o.str("mode")}, nil
	default:
		return node.Transport{}, fmt.Errorf("unsupported network %q", netw)
	}
}

func clashWireGuard(p fields) (*node.WireGuardOptions, error) {
	wg := &node.WireGuardOptions{
		PrivateKey:    p.str("private-key"),
		PeerPublicKey: p.str("public-key"),
		PreSharedKey:  p.str("pre-shared-key"),
		MTU:           p.int("mtu"),
	}
	// Newer configs move the peer into a list.
	if peers := p.list("peers"); len(peers) > 0 {
		peer := peers[0]
		wg.PeerPublicKey = peer.str("public-key")
		wg.PreSharedKey = peer.str("pre-shared-key")
		p = mergeFields(p, peer)
	}
	var addrs []string
	for _, k := range []string{"ip", "ipv6"} {
		if s := p.str(k); s != "" {
			addrs = append(addrs, s)
		}
	}
	var err error
	if wg.Address, err = parsePrefixes(addrs); err != nil {
		return nil, err
	}
	if wg.Reserved, err = clashReserved(p); err != nil {
		return nil, err
	}
	return wg, nil
}

func clashReserved(p fields) ([]uint8, error) {
	if _, ok := p["reserved"].([]any); !ok {
		return parseReserved(p.str("reserved"))
	}
	var out []uint8
	for _, s := range p.strs("reserved") {
		v, err := strconv.ParseUint(s, 10, 8)
		if err != nil {
			return nil, errors.New("invalid reserved bytes")
		}
		out = append(out, uint8(v))
	}
	return out, nil
}

func mergeFields(a, b fields) fields {
	out := fields{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// clashPlugin converts Clash's plugin + plugin-opts into the SIP003 plugin
// name and "k=v;k=v" options that share links and sing-box use.
func clashPlugin(name string, o fields) *node.ShadowsocksOptions {
	var parts []string
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	switch name {
	case "obfs":
		name = "obfs-local"
		add("obfs", o.str("mode"))
		add("obfs-host", o.str("host"))
	case "v2ray-plugin":
		add("mode", o.str("mode"))
		if o.bool("tls") {
			parts = append(parts, "tls")
		}
		add("host", o.str("host"))
		add("path", o.str("path"))
	default:
		keys := make([]string, 0, len(o))
		for k := range o {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			add(k, o.str(k))
		}
	}
	return &node.ShadowsocksOptions{Plugin: name, PluginOpts: strings.Join(parts, ";")}
}
