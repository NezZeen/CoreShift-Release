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
		"hy2":                  {SingBox, Mihomo},
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
