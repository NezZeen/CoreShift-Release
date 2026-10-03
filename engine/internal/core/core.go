// Package core renders a node.Node into the configuration of a specific proxy
// core (xray, sing-box, mihomo) and knows which nodes each core can run.
//
// Every core runs the same way: a SOCKS5 inbound on Options.Listen, which the
// TUN layer forwards to, and a single outbound for the selected node. That is
// what makes cores interchangeable for auto-swap.
package core

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"coreshift/engine/internal/node"
)

type Kind string

const (
	Xray    Kind = "xray"
	SingBox Kind = "sing-box"
	Mihomo  Kind = "mihomo"
)

// Adapter describes one core.
type Adapter interface {
	Kind() Kind
	// Supports returns an *UnsupportedError if the core cannot run n.
	Supports(n *node.Node) error
	// Render builds the complete core configuration for n.
	Render(n *node.Node, o Options) ([]byte, error)
	// ConfigName is the file name the configuration is written to.
	ConfigName() string
	// RunArgs and CheckArgs are the command lines that run the core, or only
	// validate the configuration, with the config at configPath. workDir is
	// the core's data directory.
	RunArgs(configPath, workDir string) []string
	CheckArgs(configPath, workDir string) []string
	// VersionArgs print the core's version; see ParseVersion.
	VersionArgs() []string
}

// Options are the per-run settings shared by every core.
type Options struct {
	// Listen is the SOCKS5 inbound the TUN layer forwards to.
	Listen netip.AddrPort
	// Auth, when set, is required by the inbound.
	Auth SOCKSAuth
	// LogLevel is one of debug, info, warn, error, none. Default warn.
	LogLevel string
	// ServerAddr, if set, is dialled instead of the node's server. The engine
	// resolves hostnames itself so a core never needs DNS to reach its own
	// server; TLS keeps the original hostname as SNI.
	ServerAddr string
	// Stats, if set, is a loopback port where the core reports its traffic
	// counters (see ReadTraffic). StatsSecret guards it where the core
	// supports a secret: the Clash API could otherwise change the core.
	Stats       netip.AddrPort
	StatsSecret string
	// Fragment splits the TLS ClientHello of the connection to the server
	// into pieces, which gets past DPI that blocks by the name it carries.
	// Xray and sing-box support it; mihomo connects without it.
	Fragment bool
}

// DefaultListen is where the active core accepts traffic from the TUN layer.
var DefaultListen = netip.MustParseAddrPort("127.0.0.1:17890")

func (o Options) withDefaults() Options {
	if !o.Listen.IsValid() {
		o.Listen = DefaultListen
	}
	if o.LogLevel == "" {
		o.LogLevel = "warn"
	}
	return o
}

// Adapters returns every known core, in the default priority order.
func Adapters() []Adapter { return []Adapter{xray{}, singBox{}, mihomo{}} }

// ByKind returns the adapter for k.
func ByKind(k Kind) (Adapter, bool) {
	for _, a := range Adapters() {
		if a.Kind() == k {
			return a, true
		}
	}
	return nil, false
}

// Compatible returns the kinds from priority (in that order) able to run n.
// This is the auto-swap chain for n.
func Compatible(n *node.Node, priority []Kind) []Kind {
	var out []Kind
	for _, k := range priority {
		if a, ok := ByKind(k); ok && a.Supports(n) == nil {
			out = append(out, k)
		}
	}
	return out
}

// Feature is a capability a node requires from a core.
type Feature string

const (
	FeatVLESSEncryption  Feature = "vless-encryption"
	FeatVMessAlterID     Feature = "vmess-alterid"
	FeatTCPHTTPHeader    Feature = "tcp-http-header"
	FeatReality          Feature = "reality"
	FeatHysteria2PortHop Feature = "hysteria2-port-hopping"
	featProtocolPrefix           = "protocol:"
	featTransportPrefix          = "transport:"
	featFlowPrefix               = "flow:"
	featSSPluginPrefix           = "ss-plugin:"
)

const (
	// FeatTLSInsecure: certificates are not checked. Xray removed
	// allowInsecure (an error since 2026-06-01).
	FeatTLSInsecure Feature = "tls-insecure"
	// FeatTLSPin: the server's certificate is checked against its SHA-256
	// (node.TLS.PinSHA256). sing-box pins public keys, not certificates.
	FeatTLSPin Feature = "tls-pin"
)

func protocolFeature(p node.Protocol) Feature { return Feature(featProtocolPrefix + string(p)) }
func transportFeature(n node.Network) Feature { return Feature(featTransportPrefix + string(n)) }

// Requirements lists what a core must support to run n.
func Requirements(n *node.Node) []Feature {
	f := []Feature{protocolFeature(n.Protocol)}
	if n.Flow != "" {
		f = append(f, Feature(featFlowPrefix+n.Flow))
	}
	if n.Encryption != "" {
		f = append(f, FeatVLESSEncryption)
	}
	if n.Protocol == node.VMess && n.AlterID > 0 {
		f = append(f, FeatVMessAlterID)
	}
	if t := n.Transport.Network; t != "" && t != node.NetTCP {
		f = append(f, transportFeature(t))
	}
	if n.Transport.HeaderType == "http" {
		f = append(f, FeatTCPHTTPHeader)
	}
	if t := n.TLS; t != nil {
		// A pin is checked instead of the signature: it makes "insecure"
		// moot.
		switch {
		case t.Reality != nil:
			f = append(f, FeatReality)
		case t.PinSHA256 != "":
			f = append(f, FeatTLSPin)
		case t.Insecure:
			f = append(f, FeatTLSInsecure)
		}
	}
	if n.Shadowsocks != nil && n.Shadowsocks.Plugin != "" {
		f = append(f, Feature(featSSPluginPrefix+n.Shadowsocks.Plugin))
	}
	if n.Hysteria2 != nil && n.Hysteria2.Ports != "" {
		f = append(f, FeatHysteria2PortHop)
	}
	return f
}

// UnsupportedError explains why a core cannot run a node.
type UnsupportedError struct {
	Core    Kind
	Feature Feature
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("%s does not support %s", e.Core, e.Feature)
}

// Features lists what core k supports, in the terms Requirements uses:
// "protocol:vless", "transport:ws", "reality" and so on. Plain TCP is
// implied.
func Features(k Kind) []Feature {
	switch k {
	case Xray:
		return slices.Clone(xrayFeatures)
	case SingBox:
		return slices.Clone(singBoxFeatures)
	case Mihomo:
		return slices.Clone(mihomoFeatures)
	}
	return nil
}

func checkSupport(k Kind, supported []Feature, n *node.Node) error {
	for _, f := range Requirements(n) {
		if !slices.Contains(supported, f) {
			return &UnsupportedError{Core: k, Feature: f}
		}
	}
	return nil
}

func features(protocols []node.Protocol, transports []node.Network, extra ...Feature) []Feature {
	var out []Feature
	for _, p := range protocols {
		out = append(out, protocolFeature(p))
	}
	for _, t := range transports {
		out = append(out, transportFeature(t))
	}
	return append(out, extra...)
}

type obj = map[string]any

// target returns the address to dial and the TLS server name for n.
func target(n *node.Node, o Options) (addr, sni string) {
	addr = n.Server
	if o.ServerAddr != "" {
		addr = o.ServerAddr
	}
	if n.TLS != nil {
		sni = n.TLS.ServerName
		if _, err := netip.ParseAddr(n.Server); sni == "" && err != nil {
			sni = n.Server
		}
	}
	return addr, sni
}

func hostPort(host string, port uint16) string {
	return net.JoinHostPort(host, strconv.Itoa(int(port)))
}

func prefixStrings(ps []netip.Prefix) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}
	return out
}

// ints avoids []uint8 being encoded as a base64 string.
func ints(b []uint8) []int {
	out := make([]int, len(b))
	for i, v := range b {
		out[i] = int(v)
	}
	return out
}

// sip003Opts parses "obfs=http;obfs-host=example.com;tls" into a map; flags
// without a value become "true".
func sip003Opts(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ";") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			v = "true"
		}
		out[k] = v
	}
	return out
}

// sip003Keys are the plugin options a subscription may set. sing-box's
// v2ray-plugin also reads "cert", a certificate file path, which a core
// running as SYSTEM must not be pointed at by a panel.
var sip003Keys = map[string][]string{
	"obfs-local":   {"obfs", "obfs-host"},
	"v2ray-plugin": {"mode", "host", "path", "tls", "mux", "certRaw"},
}

// sip003Clean keeps the options of plugin that sip003Keys allows, in their
// order; values are kept as written.
func sip003Clean(plugin, opts string) string {
	allowed := sip003Keys[plugin]
	var out []string
	for _, part := range strings.Split(opts, ";") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		k, _, _ := strings.Cut(part, "=")
		if slices.Contains(allowed, k) {
			out = append(out, part)
		}
	}
	return strings.Join(out, ";")
}

var errNoWireGuard = errors.New("wireguard options missing")
