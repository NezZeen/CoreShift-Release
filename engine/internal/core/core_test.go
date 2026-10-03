package core

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"

	"coreshift/engine/internal/node"
)

var allKinds = []Kind{Xray, SingBox, Mihomo}

// TestCompatibility pins which cores run which nodes: this is exactly what
// auto-swap chains and the X/S/M badges in the UI are built from.
func TestCompatibility(t *testing.T) {
	all := allKinds
	want := map[string][]Kind{
		"vless-reality-vision": all,
		"vless-xhttp-reality":  {Xray},
		"vless-ws-tls-ed":      all,
		"vless-grpc-tls":       all,
		"vless-httpupgrade":    all,
		"vless-h2":             {SingBox, Mihomo},
		"vmess-ws-tls":         all,
		"vmess-tcp-http":       {Xray, Mihomo},
		"vmess-alterid":        {SingBox, Mihomo},
		"trojan-tcp":           all,
		"trojan-grpc":          all,
		"ss-aes":               all,
		"ss-2022":              all,
		"ss-obfs":              {SingBox, Mihomo},
		"ss-v2ray":             {SingBox, Mihomo},
		"trojan-insecure":      {SingBox, Mihomo}, // Xray no longer skips the certificate check
		"hy2":                  all,
		"hy2-pin":              {Xray, Mihomo}, // sing-box pins public keys, not certificates
		"hy2-insecure":         {SingBox, Mihomo},
		"tuic":                 {SingBox, Mihomo},
		"anytls":               {SingBox, Mihomo},
		"wireguard":            all,
	}
	fx := fixtures(t)
	if len(fx) != len(want) {
		t.Fatalf("%d fixtures but %d expectations", len(fx), len(want))
	}
	for name, n := range fx {
		if got := Compatible(&n, allKinds); !slices.Equal(got, want[name]) {
			t.Errorf("%s: compatible = %v, want %v", name, got, want[name])
		}
	}
}

func TestCompatibleKeepsPriorityOrder(t *testing.T) {
	n := fixtures(t)["trojan-tcp"]
	prio := []Kind{Mihomo, Xray, SingBox}
	if got := Compatible(&n, prio); !slices.Equal(got, prio) {
		t.Errorf("got %v, want %v", got, prio)
	}
}

func TestUnsupportedErrorNamesFeature(t *testing.T) {
	n := fixtures(t)["vless-xhttp-reality"]
	_, err := singBox{}.Render(&n, Options{})
	var ue *UnsupportedError
	if !errors.As(err, &ue) || ue.Core != SingBox || ue.Feature != "transport:xhttp" {
		t.Fatalf("err = %v, want sing-box/transport:xhttp", err)
	}
	enc := n
	enc.Transport = node.Transport{}
	enc.Encryption = "mlkem768x25519plus.native.0rtt.key"
	if err := (mihomo{}).Supports(&enc); err == nil {
		t.Error("mihomo must not claim VLESS encryption support")
	}
	if err := (xray{}).Supports(&enc); err != nil {
		t.Errorf("xray should support VLESS encryption: %v", err)
	}
}

func decodeJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	return m
}

// dig walks nested maps and lists: dig(m, "outbounds", 0, "settings").
func dig(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			l, _ := v.([]any)
			if k >= len(l) {
				return nil
			}
			v = l[k]
		}
	}
	return v
}

func render(t *testing.T, a Adapter, n node.Node, o Options) []byte {
	t.Helper()
	b, err := a.Render(&n, o)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestXrayRender(t *testing.T) {
	fx := fixtures(t)
	cfg := decodeJSON(t, render(t, xray{}, fx["vless-reality-vision"], Options{}))
	out := dig(cfg, "outbounds", 0)
	if dig(out, "protocol") != "vless" || dig(out, "settings", "vnext", 0, "users", 0, "flow") != "xtls-rprx-vision" {
		t.Errorf("vless outbound = %v", out)
	}
	if dig(out, "streamSettings", "security") != "reality" || dig(out, "streamSettings", "realitySettings", "serverName") != "www.example.com" {
		t.Errorf("reality settings = %v", dig(out, "streamSettings"))
	}
	if dig(cfg, "inbounds", 0, "port") != float64(17890) || dig(cfg, "inbounds", 0, "protocol") != "socks" {
		t.Errorf("inbound = %v", dig(cfg, "inbounds", 0))
	}

	ws := decodeJSON(t, render(t, xray{}, fx["vless-ws-tls-ed"], Options{}))
	if p := dig(ws, "outbounds", 0, "streamSettings", "wsSettings", "path"); p != "/ws?ed=2048" {
		t.Errorf("ws path = %v, want early data back in the path", p)
	}

	wg := decodeJSON(t, render(t, xray{}, fx["wireguard"], Options{}))
	if r := dig(wg, "outbounds", 0, "settings", "reserved"); !reflect.DeepEqual(r, []any{1.0, 2.0, 3.0}) {
		t.Errorf("reserved = %#v, want a number list", r)
	}
}

func TestSingBoxRender(t *testing.T) {
	fx := fixtures(t)
	hy2 := decodeJSON(t, render(t, singBox{}, fx["hy2"], Options{}))
	if p := dig(hy2, "outbounds", 0, "server_ports"); !reflect.DeepEqual(p, []any{"443:443", "20000:30000"}) {
		t.Errorf("server_ports = %v", p)
	}
	if dig(hy2, "route", "final") != "proxy" {
		t.Error("route.final must be the proxy")
	}

	rv := decodeJSON(t, render(t, singBox{}, fx["vless-reality-vision"], Options{}))
	if dig(rv, "outbounds", 0, "tls", "utls", "fingerprint") != "chrome" || dig(rv, "outbounds", 0, "tls", "reality", "enabled") != true {
		t.Errorf("reality tls = %v", dig(rv, "outbounds", 0, "tls"))
	}

	wg := decodeJSON(t, render(t, singBox{}, fx["wireguard"], Options{}))
	if dig(wg, "endpoints", 0, "tag") != "proxy" || dig(wg, "endpoints", 0, "peers", 0, "port") != float64(51820) {
		t.Errorf("wireguard endpoint = %v", dig(wg, "endpoints", 0))
	}
}

func TestMihomoRender(t *testing.T) {
	fx := fixtures(t)
	decode := func(n node.Node) map[string]any {
		var m map[string]any
		if err := yaml.Unmarshal(render(t, mihomo{}, n, Options{}), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	ss := decode(fx["ss-obfs"])
	if dig(ss, "proxies", 0, "plugin") != "obfs" || dig(ss, "proxies", 0, "plugin-opts", "mode") != "http" {
		t.Errorf("ss plugin = %v", dig(ss, "proxies", 0))
	}
	if !reflect.DeepEqual(ss["rules"], []any{"MATCH,proxy"}) || ss["socks-port"] != 17890 {
		t.Errorf("config = %v", ss)
	}
	up := decode(fx["vless-httpupgrade"])
	if dig(up, "proxies", 0, "network") != "ws" || dig(up, "proxies", 0, "ws-opts", "v2ray-http-upgrade") != true {
		t.Errorf("httpupgrade = %v", dig(up, "proxies", 0))
	}
	rv := decode(fx["vless-reality-vision"])
	if dig(rv, "proxies", 0, "servername") != "www.example.com" || dig(rv, "proxies", 0, "reality-opts", "short-id") != "6ba85179e30d4fc2" {
		t.Errorf("reality = %v", dig(rv, "proxies", 0))
	}
}

func TestFragment(t *testing.T) {
	fx := fixtures(t)
	on := Options{Fragment: true}

	x := decodeJSON(t, render(t, xray{}, fx["vless-reality-vision"], on))
	if dig(x, "outbounds", 0, "streamSettings", "sockopt", "dialerProxy") != "fragment" ||
		dig(x, "outbounds", 2, "tag") != "fragment" || dig(x, "outbounds", 2, "settings", "fragment", "packets") != "tlshello" {
		t.Errorf("xray outbounds = %v", dig(x, "outbounds"))
	}
	// Without TLS there is no ClientHello to split.
	if wg := decodeJSON(t, render(t, xray{}, fx["wireguard"], on)); len(dig(wg, "outbounds").([]any)) != 2 {
		t.Errorf("xray wireguard outbounds = %v", dig(wg, "outbounds"))
	}
	if off := decodeJSON(t, render(t, xray{}, fx["vless-reality-vision"], Options{})); dig(off, "outbounds", 0, "streamSettings", "sockopt") != nil {
		t.Errorf("xray fragments without being asked: %v", dig(off, "outbounds", 0))
	}

	s := decodeJSON(t, render(t, singBox{}, fx["vless-reality-vision"], on))
	if dig(s, "outbounds", 0, "tls", "fragment") != true {
		t.Errorf("sing-box tls = %v", dig(s, "outbounds", 0, "tls"))
	}
	// Hysteria2's TLS runs inside QUIC.
	if hy2 := decodeJSON(t, render(t, singBox{}, fx["hy2"], on)); dig(hy2, "outbounds", 0, "tls", "fragment") != nil {
		t.Errorf("sing-box hysteria2 tls = %v", dig(hy2, "outbounds", 0, "tls"))
	}
	if hy2 := decodeJSON(t, render(t, xray{}, fx["hy2"], on)); len(dig(hy2, "outbounds").([]any)) != 2 ||
		dig(hy2, "outbounds", 0, "streamSettings", "sockopt") != nil {
		t.Errorf("xray hysteria2 outbounds = %v", dig(hy2, "outbounds"))
	}
}

// hy2Node is a Hysteria2 server with every option a subscription can set.
func hy2Node() node.Node {
	return node.Node{
		Name: "hy2", Protocol: node.Hysteria2, Server: "hy.example.com", Port: 443, Password: "secret-auth",
		TLS: &node.TLS{ServerName: "sni.example.com", ALPN: []string{"h3"}},
		Hysteria2: &node.Hysteria2Options{Obfs: "salamander", ObfsPassword: "ob-pw", UpMbps: 50, DownMbps: 200,
			Ports: "20000-30000,40000"},
	}
}

func TestHysteria2Render(t *testing.T) {
	n := hy2Node()
	o := Options{ServerAddr: "198.51.100.7"}

	x := dig(decodeJSON(t, render(t, xray{}, n, o)), "outbounds", 0)
	want := map[string]any{
		"tag": "proxy", "protocol": "hysteria",
		"settings": map[string]any{"version": 2.0, "address": "198.51.100.7", "port": 443.0},
		"streamSettings": map[string]any{
			"network": "hysteria", "security": "tls",
			"tlsSettings":      map[string]any{"serverName": "sni.example.com", "alpn": []any{"h3"}},
			"hysteriaSettings": map[string]any{"version": 2.0, "auth": "secret-auth"},
			"finalmask": map[string]any{
				"udp": []any{map[string]any{"type": "salamander", "settings": map[string]any{"password": "ob-pw"}}},
				"quicParams": map[string]any{"brutalUp": "50 mbps", "brutalDown": "200 mbps",
					"udpHop": map[string]any{"ports": "20000-30000,40000", "interval": 30.0}},
			},
		},
	}
	if !reflect.DeepEqual(x, want) {
		t.Errorf("xray outbound:\n got %v\nwant %v", x, want)
	}

	s := dig(decodeJSON(t, render(t, singBox{}, n, o)), "outbounds", 0)
	if dig(s, "type") != "hysteria2" || dig(s, "server") != "198.51.100.7" || dig(s, "password") != "secret-auth" ||
		dig(s, "up_mbps") != 50.0 || dig(s, "down_mbps") != 200.0 ||
		!reflect.DeepEqual(dig(s, "server_ports"), []any{"20000:30000", "40000:40000"}) ||
		dig(s, "obfs", "type") != "salamander" || dig(s, "obfs", "password") != "ob-pw" ||
		dig(s, "tls", "server_name") != "sni.example.com" || !reflect.DeepEqual(dig(s, "tls", "alpn"), []any{"h3"}) {
		t.Errorf("sing-box outbound = %v", s)
	}

	var m map[string]any
	if err := yaml.Unmarshal(render(t, mihomo{}, n, o), &m); err != nil {
		t.Fatal(err)
	}
	p := dig(m, "proxies", 0)
	if dig(p, "type") != "hysteria2" || dig(p, "server") != "198.51.100.7" || dig(p, "password") != "secret-auth" ||
		dig(p, "up") != 50 || dig(p, "down") != 200 || dig(p, "ports") != "20000-30000,40000" ||
		dig(p, "obfs") != "salamander" || dig(p, "obfs-password") != "ob-pw" || dig(p, "sni") != "sni.example.com" {
		t.Errorf("mihomo proxy = %v", p)
	}

	// A pinned certificate: Xray and mihomo check it, sing-box cannot.
	pinned := hy2Node()
	pinned.TLS.PinSHA256 = "21140e7cd89135e97d3f9c4b89a154063351b277e03fd696469fdfed12fd43d0"
	pinned.TLS.Insecure = true // moot with a pin
	xp := decodeJSON(t, render(t, xray{}, pinned, o))
	if ts := dig(xp, "outbounds", 0, "streamSettings", "tlsSettings"); dig(ts, "pinnedPeerCertSha256") != pinned.TLS.PinSHA256 || dig(ts, "allowInsecure") != nil {
		t.Errorf("xray pinned tls = %v", ts)
	}
	if err := yaml.Unmarshal(render(t, mihomo{}, pinned, o), &m); err != nil {
		t.Fatal(err)
	}
	if p := dig(m, "proxies", 0); dig(p, "fingerprint") != pinned.TLS.PinSHA256 || dig(p, "skip-cert-verify") != nil {
		t.Errorf("mihomo pinned proxy = %v", p)
	}
	var ue *UnsupportedError
	if err := (singBox{}).Supports(&pinned); !errors.As(err, &ue) || ue.Feature != FeatTLSPin {
		t.Errorf("sing-box with a pin: %v", err)
	}

	// Insecure: Xray refuses to start with allowInsecure.
	insecure := hy2Node()
	insecure.TLS.Insecure = true
	if err := (xray{}).Supports(&insecure); !errors.As(err, &ue) || ue.Feature != FeatTLSInsecure {
		t.Errorf("xray with insecure TLS: %v", err)
	}

	// No options at all: plain TLS with the server name as SNI.
	bare := node.Node{Protocol: node.Hysteria2, Server: "hy.example.com", Port: 8443, Password: "pw"}
	b := dig(decodeJSON(t, render(t, xray{}, bare, Options{})), "outbounds", 0, "streamSettings")
	if dig(b, "finalmask") != nil || dig(b, "tlsSettings", "serverName") != "hy.example.com" || dig(b, "security") != "tls" {
		t.Errorf("xray bare hysteria2 stream = %v", b)
	}
}

func TestServerAddrKeepsHostnameAsSNI(t *testing.T) {
	n := fixtures(t)["vless-ws-tls-ed"] // server is edge.example.com
	n.TLS.ServerName = ""               // so SNI must fall back to it
	o := Options{ServerAddr: "198.51.100.7"}
	x := decodeJSON(t, render(t, xray{}, n, o))
	if dig(x, "outbounds", 0, "settings", "vnext", 0, "address") != "198.51.100.7" ||
		dig(x, "outbounds", 0, "streamSettings", "tlsSettings", "serverName") != "edge.example.com" {
		t.Errorf("xray = %v", dig(x, "outbounds", 0))
	}
	s := decodeJSON(t, render(t, singBox{}, n, o))
	if dig(s, "outbounds", 0, "server") != "198.51.100.7" || dig(s, "outbounds", 0, "tls", "server_name") != "edge.example.com" {
		t.Errorf("sing-box = %v", dig(s, "outbounds", 0))
	}
}
