package subscription

import (
	"fmt"
	"strings"

	"coreshift/engine/internal/node"
)

// singBoxGroupTypes are outbounds that are not servers; they are ignored
// rather than reported as skipped.
var singBoxGroupTypes = map[string]bool{
	"selector": true, "urltest": true, "direct": true, "block": true, "dns": true,
}

// singBoxOutbound converts one sing-box outbound or endpoint.
func singBoxOutbound(o fields) (node.Node, error) {
	typ := o.str("type")
	if typ == "wireguard" {
		return singBoxWireGuard(o)
	}
	port, err := parsePort(o.str("server_port"))
	if err != nil {
		return node.Node{}, err
	}
	n := node.Node{Name: strings.TrimSpace(o.str("tag")), Server: o.str("server"), Port: port}
	switch typ {
	case "vless":
		n.Protocol = node.VLESS
		n.UUID = o.str("uuid")
		n.Flow = o.str("flow")
	case "vmess":
		n.Protocol = node.VMess
		n.UUID = o.str("uuid")
		n.AlterID = o.int("alter_id")
		n.Cipher = o.str("security")
		if n.Cipher == "" {
			n.Cipher = "auto"
		}
	case "trojan":
		n.Protocol = node.Trojan
		n.Password = o.str("password")
	case "shadowsocks":
		n.Protocol = node.Shadowsocks
		n.Cipher = o.str("method")
		n.Password = o.str("password")
		if plugin := o.str("plugin"); plugin != "" {
			n.Shadowsocks = &node.ShadowsocksOptions{Plugin: plugin, PluginOpts: o.str("plugin_opts")}
		}
	case "hysteria2":
		n.Protocol = node.Hysteria2
		n.Password = o.str("password")
		obfs := o.sub("obfs")
		opts := node.Hysteria2Options{
			Obfs: obfs.str("type"), ObfsPassword: obfs.str("password"),
			UpMbps: o.int("up_mbps"), DownMbps: o.int("down_mbps"),
			// sing-box writes ranges as "20000:30000".
			Ports: strings.ReplaceAll(strings.Join(o.strs("server_ports"), ","), ":", "-"),
		}
		if opts != (node.Hysteria2Options{}) {
			n.Hysteria2 = &opts
		}
	case "tuic":
		n.Protocol = node.TUIC
		n.UUID = o.str("uuid")
		n.Password = o.str("password")
		n.TUIC = &node.TUICOptions{
			CongestionControl: o.str("congestion_control"),
			UDPRelayMode:      o.str("udp_relay_mode"),
		}
	case "anytls":
		n.Protocol = node.AnyTLS
		n.Password = o.str("password")
	default:
		return n, fmt.Errorf("unsupported type %q", typ)
	}
	if n.Transport, err = singBoxTransport(o.sub("transport")); err != nil {
		return n, err
	}
	if t := o.sub("tls"); t.bool("enabled") {
		n.TLS = &node.TLS{
			ServerName: t.str("server_name"),
			ALPN:       t.strs("alpn"),
			Insecure:   t.bool("insecure"),
		}
		if u := t.sub("utls"); u.bool("enabled") {
			n.TLS.Fingerprint = u.str("fingerprint")
		}
		if r := t.sub("reality"); r.bool("enabled") {
			n.TLS.Reality = &node.Reality{PublicKey: r.str("public_key"), ShortID: r.str("short_id")}
		}
	}
	return n, finish(&n)
}

func singBoxTransport(t fields) (node.Transport, error) {
	switch typ := t.str("type"); typ {
	case "":
		return node.Transport{}, nil
	case "ws":
		return node.Transport{
			Network: node.NetWS, Path: t.str("path"), Host: t.sub("headers").str("Host", "host"),
			EarlyData: uint32(t.int("max_early_data")),
		}, nil
	case "grpc":
		return node.Transport{Network: node.NetGRPC, ServiceName: t.str("service_name")}, nil
	case "http":
		tr := node.Transport{Network: node.NetHTTP, Path: t.str("path")}
		if hosts := t.strs("host"); len(hosts) > 0 {
			tr.Host = hosts[0]
		}
		return tr, nil
	case "httpupgrade":
		return node.Transport{Network: node.NetHTTPUpgrade, Path: t.str("path"), Host: t.str("host")}, nil
	default:
		return node.Transport{}, fmt.Errorf("unsupported transport %q", typ)
	}
}

// singBoxWireGuard handles both the 1.11+ endpoint and the legacy outbound.
func singBoxWireGuard(o fields) (node.Node, error) {
	n := node.Node{Name: strings.TrimSpace(o.str("tag")), Protocol: node.WireGuard}
	wg := &node.WireGuardOptions{PrivateKey: o.str("private_key"), MTU: o.int("mtu")}
	addrs := o.strs("address")
	peer := o
	if peers := o.list("peers"); len(peers) > 0 {
		peer = peers[0]
		n.Server = peer.str("address")
		n.Port, _ = parsePort(peer.str("port"))
	} else {
		addrs = o.strs("local_address")
		n.Server = o.str("server")
		n.Port, _ = parsePort(o.str("server_port"))
		wg.PeerPublicKey = o.str("peer_public_key")
	}
	if k := peer.str("public_key"); k != "" {
		wg.PeerPublicKey = k
	}
	wg.PreSharedKey = peer.str("pre_shared_key")
	var err error
	if wg.Address, err = parsePrefixes(addrs); err != nil {
		return n, err
	}
	for _, s := range peer.strs("reserved") {
		var b uint8
		if _, err := fmt.Sscan(s, &b); err != nil {
			return n, fmt.Errorf("invalid reserved bytes")
		}
		wg.Reserved = append(wg.Reserved, b)
	}
	n.WireGuard = wg
	return n, finish(&n)
}
