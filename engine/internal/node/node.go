// Package node defines the core-independent description of a proxy server.
//
// Subscriptions are parsed into Node, and per-core adapters render it into
// xray, sing-box or mihomo configuration. Keeping one model is what lets the
// engine swap cores without re-reading the subscription.
package node

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
)

type Protocol string

const (
	VLESS       Protocol = "vless"
	VMess       Protocol = "vmess"
	Trojan      Protocol = "trojan"
	Shadowsocks Protocol = "shadowsocks"
	Hysteria2   Protocol = "hysteria2"
	TUIC        Protocol = "tuic"
	AnyTLS      Protocol = "anytls"
	WireGuard   Protocol = "wireguard"
)

// Network is the stream transport under VLESS, VMess and Trojan.
type Network string

const (
	NetTCP         Network = "tcp"
	NetWS          Network = "ws"
	NetGRPC        Network = "grpc"
	NetHTTP        Network = "http" // HTTP/2
	NetHTTPUpgrade Network = "httpupgrade"
	NetXHTTP       Network = "xhttp"
)

type Node struct {
	Name     string   `json:"name"`
	Protocol Protocol `json:"protocol"`
	Server   string   `json:"server"`
	Port     uint16   `json:"port"`

	UUID       string `json:"uuid,omitempty"`       // vless, vmess, tuic
	Password   string `json:"password,omitempty"`   // trojan, shadowsocks, hysteria2, tuic, anytls
	Flow       string `json:"flow,omitempty"`       // vless, e.g. "xtls-rprx-vision"
	Encryption string `json:"encryption,omitempty"` // vless; empty means "none"
	Cipher     string `json:"cipher,omitempty"`     // vmess security or shadowsocks method
	AlterID    int    `json:"alter_id,omitempty"`   // vmess

	Transport Transport `json:"transport"`
	// TLS is nil when the connection is not wrapped in TLS or REALITY.
	TLS *TLS `json:"tls,omitempty"`

	Shadowsocks *ShadowsocksOptions `json:"shadowsocks,omitempty"`
	Hysteria2   *Hysteria2Options   `json:"hysteria2,omitempty"`
	TUIC        *TUICOptions        `json:"tuic,omitempty"`
	WireGuard   *WireGuardOptions   `json:"wireguard,omitempty"`
}

type Transport struct {
	Network     Network           `json:"network,omitempty"` // empty means tcp
	Path        string            `json:"path,omitempty"`
	Host        string            `json:"host,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	ServiceName string            `json:"service_name,omitempty"` // grpc
	// Mode is the xhttp mode (auto, packet-up, stream-up, stream-one) or the
	// grpc mode (gun, multi).
	Mode string `json:"mode,omitempty"`
	// HeaderType "http" enables HTTP/1.1 header obfuscation on plain tcp.
	HeaderType string `json:"header_type,omitempty"`
	// EarlyData is the WebSocket max early data size (the "?ed=" of share links).
	EarlyData uint32 `json:"early_data,omitempty"`
	// Extra is xhttp's "extra" JSON, limited to the options a subscription
	// may set (SanitizeXHTTPExtra).
	Extra string `json:"extra,omitempty"`
}

type TLS struct {
	ServerName  string   `json:"server_name,omitempty"`
	ALPN        []string `json:"alpn,omitempty"`
	Insecure    bool     `json:"insecure,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"` // uTLS: chrome, firefox, safari, random…
	Reality     *Reality `json:"reality,omitempty"`
}

type Reality struct {
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id,omitempty"`
	SpiderX   string `json:"spider_x,omitempty"`
}

type ShadowsocksOptions struct {
	Plugin     string `json:"plugin,omitempty"`      // e.g. obfs-local, v2ray-plugin
	PluginOpts string `json:"plugin_opts,omitempty"` // "obfs=http;obfs-host=example.com"
}

type Hysteria2Options struct {
	Obfs         string `json:"obfs,omitempty"` // "salamander"
	ObfsPassword string `json:"obfs_password,omitempty"`
	UpMbps       int    `json:"up_mbps,omitempty"`
	DownMbps     int    `json:"down_mbps,omitempty"`
	// Ports is a port-hopping spec such as "20000-30000" or "443,5000-6000".
	Ports string `json:"ports,omitempty"`
}

type TUICOptions struct {
	CongestionControl string `json:"congestion_control,omitempty"` // bbr, cubic, new_reno
	UDPRelayMode      string `json:"udp_relay_mode,omitempty"`     // native, quic
}

type WireGuardOptions struct {
	PrivateKey    string         `json:"private_key"`
	PeerPublicKey string         `json:"peer_public_key"`
	PreSharedKey  string         `json:"pre_shared_key,omitempty"`
	Address       []netip.Prefix `json:"address"`
	Reserved      []uint8        `json:"reserved,omitempty"`
	MTU           int            `json:"mtu,omitempty"`
}

// Validate checks that the fields every core needs are present.
func (n *Node) Validate() error {
	if n.Server == "" {
		return errors.New("missing server")
	}
	if n.Port == 0 {
		return errors.New("missing port")
	}
	need := func(ok bool, what string) error {
		if !ok {
			return fmt.Errorf("%s: missing %s", n.Protocol, what)
		}
		return nil
	}
	var err error
	switch n.Protocol {
	case VLESS, VMess:
		err = need(n.UUID != "", "uuid")
	case TUIC:
		err = errors.Join(need(n.UUID != "", "uuid"), need(n.Password != "", "password"))
	case Trojan, Hysteria2, AnyTLS:
		err = need(n.Password != "", "password")
	case Shadowsocks:
		err = errors.Join(need(n.Cipher != "", "cipher"), need(n.Password != "", "password"))
	case WireGuard:
		w := n.WireGuard
		err = need(w != nil && w.PrivateKey != "" && w.PeerPublicKey != "" && len(w.Address) > 0,
			"private key, peer public key or address")
	default:
		return fmt.Errorf("unknown protocol %q", n.Protocol)
	}
	if err != nil {
		return err
	}
	if n.TLS != nil && n.TLS.Reality != nil && n.TLS.Reality.PublicKey == "" {
		return errors.New("reality: missing public key")
	}
	return nil
}

// Fingerprint identifies a node by everything except its name, so the
// selection survives a subscription refresh that renames servers.
func (n *Node) Fingerprint() string {
	c := *n
	c.Name = ""
	b, _ := json.Marshal(c) // struct field order is fixed and map keys are sorted
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}
