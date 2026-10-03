package subscription

import (
	"testing"

	"coreshift/engine/internal/node"
)

const testPin = "21140e7cd89135e97d3f9c4b89a154063351b277e03fd696469fdfed12fd43d0"

// remnawaveHysteria is an Xray JSON subscription as Remnawave 2.7+ serves it
// with a Hysteria host: the "🎲Автовыбор" config of a template with
// injectHosts, a balancer over the outbounds tagged "proxy…", a VLESS
// REALITY server and a Hysteria2 one with Salamander, Brutal rates, port
// hopping and a pinned self-signed certificate, then the template's own
// outbounds.
const remnawaveHysteria = `[{
  "remarks": "🎲Автовыбор",
  "dns": {"servers": ["1.1.1.1"]},
  "inbounds": [{"tag": "socks", "port": 10808, "listen": "127.0.0.1", "protocol": "socks", "settings": {"udp": true}}],
  "observatory": {"subjectSelector": ["proxy"], "probeUrl": "https://www.gstatic.com/generate_204", "probeInterval": "30s"},
  "routing": {
    "domainStrategy": "IPIfNonMatch",
    "balancers": [{"tag": "Super_Balancer", "selector": ["proxy"], "strategy": {"type": "leastPing"}, "fallbackTag": "direct"}],
    "rules": [{"type": "field", "network": "tcp,udp", "balancerTag": "Super_Balancer"}]
  },
  "outbounds": [
    {"tag": "proxy", "protocol": "vless",
     "settings": {"vnext": [{"address": "nl.example.com", "port": 443,
       "users": [{"id": "11111111-2222-3333-4444-555555555555", "encryption": "none", "flow": "xtls-rprx-vision"}]}]},
     "streamSettings": {"network": "tcp", "tcpSettings": {}, "security": "reality",
       "realitySettings": {"serverName": "www.microsoft.com", "publicKey": "PUBKEY", "shortId": "6ba85179e30d4fc2", "fingerprint": "chrome"}}},
    {"tag": "proxy-2", "protocol": "hysteria",
     "settings": {"address": "hy.example.com", "port": 443, "version": 2},
     "streamSettings": {"network": "hysteria",
       "hysteriaSettings": {"version": 2, "auth": "hy2-user-secret"},
       "security": "tls",
       "tlsSettings": {"serverName": "hy.example.com", "enableSessionResumption": false, "fingerprint": "chrome", "alpn": ["h3"],
         "pinnedPeerCertSha256": "21:14:0E:7C:D8:91:35:E9:7D:3F:9C:4B:89:A1:54:06:33:51:B2:77:E0:3F:D6:96:46:9F:DF:ED:12:FD:43:D0"},
       "finalmask": {
         "udp": [{"type": "salamander", "settings": {"password": "obfs-secret"}}],
         "quicParams": {"congestion": "brutal", "debug": false, "brutalUp": "50 mbps", "brutalDown": "200 mbps",
           "udpHop": {"ports": "20000-30000, 40000", "interval": "5-10"}, "maxIdleTimeout": 30}}}},
    {"tag": "direct", "protocol": "freedom"},
    {"tag": "block", "protocol": "blackhole"}
  ]
}]`

func TestParseRemnawaveHysteria2(t *testing.T) {
	res, err := Parse([]byte(remnawaveHysteria))
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatXray || len(res.Nodes) != 2 || len(res.Skipped) != 0 {
		t.Fatalf("format %s, nodes %+v, skipped %v", res.Format, res.Nodes, res.Skipped)
	}
	vless, hy := res.Nodes[0], res.Nodes[1]
	if vless.Protocol != node.VLESS || vless.TLS == nil || vless.TLS.Reality == nil || vless.Flow != "xtls-rprx-vision" {
		t.Errorf("vless node = %+v", vless)
	}
	want := node.Node{
		Name: "proxy-2", Protocol: node.Hysteria2, Server: "hy.example.com", Port: 443, Password: "hy2-user-secret",
		TLS: &node.TLS{ServerName: "hy.example.com", ALPN: []string{"h3"}, PinSHA256: testPin},
		Hysteria2: &node.Hysteria2Options{Obfs: "salamander", ObfsPassword: "obfs-secret", UpMbps: 50, DownMbps: 200,
			Ports: "20000-30000,40000"},
	}
	if hy.Fingerprint() != want.Fingerprint() {
		t.Errorf("hysteria2 node:\n got %s\nwant %s", describe(hy), describe(want))
	}
	// Both are the panel's automatic selection, in its order.
	if len(res.Auto) != 2 || res.Auto[0] != vless.Fingerprint() || res.Auto[1] != hy.Fingerprint() {
		t.Errorf("group = %v", res.Auto)
	}
}

// hyStream is the least a Hysteria2 outbound's streamSettings hold.
const hyStream = `"network": "hysteria", "hysteriaSettings": {"version": 2, "auth": "pw"}`

// hysteriaOutbound is an Xray Hysteria2 outbound with the given
// streamSettings.
func hysteriaOutbound(stream string) string {
	return `{"outbounds": [{"tag": "hy", "protocol": "hysteria", "settings": {"version": 2, "address": "203.0.113.9", "port": 8443},
	  "streamSettings": {` + stream + `}}]}`
}

func parseOne(t *testing.T, body string) (node.Node, error) {
	t.Helper()
	res, err := Parse([]byte(body))
	if err != nil {
		if len(res.Skipped) > 0 {
			return node.Node{}, &skipError{res.Skipped[0]}
		}
		return node.Node{}, err
	}
	return res.Nodes[0], nil
}

type skipError struct{ s Skipped }

func (e *skipError) Error() string { return e.s.Reason }

func TestParseXrayHysteria2Options(t *testing.T) {
	// The fewest fields: TLS by default, no options.
	n, err := parseOne(t, hysteriaOutbound(hyStream))
	if err != nil || n.Protocol != node.Hysteria2 || n.Server != "203.0.113.9" || n.Port != 8443 || n.Password != "pw" ||
		n.TLS == nil || n.Hysteria2 != nil {
		t.Errorf("minimal: %s, %v", describe(n), err)
	}

	// BBR: the Brutal rates are not used.
	n, err = parseOne(t, hysteriaOutbound(hyStream+`, "security": "tls", "finalmask": {"quicParams": {"congestion": "bbr", "brutalUp": "100 mbps"}}`))
	if err != nil || n.Hysteria2 != nil {
		t.Errorf("bbr: %s, %v", describe(n), err)
	}

	// The certificate is not checked: kept, so the cores that can skip it run it.
	n, err = parseOne(t, hysteriaOutbound(hyStream+`, "security": "tls", "tlsSettings": {"serverName": "s.example", "allowInsecure": true}`))
	if err != nil || !n.TLS.Insecure || n.TLS.ServerName != "s.example" {
		t.Errorf("insecure: %s, %v", describe(n), err)
	}

	for name, stream := range map[string]string{
		"unknown mask":       hyStream + `, "finalmask": {"udp": [{"type": "noise", "settings": {"noise": [{"type": "rand", "packet": "10-20"}]}}]}`,
		"salamander, no pw":  hyStream + `, "finalmask": {"udp": [{"type": "salamander", "settings": {}}]}`,
		"two salamanders":    hyStream + `, "finalmask": {"udp": [{"type": "salamander", "settings": {"password": "a"}}, {"type": "salamander", "settings": {"password": "b"}}]}`,
		"bad pin":            hyStream + `, "security": "tls", "tlsSettings": {"pinnedPeerCertSha256": "abcd"}`,
		"reality":            hyStream + `, "security": "reality", "realitySettings": {"publicKey": "k"}`,
		"bad hop ports":      hyStream + `, "finalmask": {"quicParams": {"udpHop": {"ports": "env:HOP"}}}`,
		"reversed hop range": hyStream + `, "finalmask": {"quicParams": {"udpHop": {"ports": "3000-2000"}}}`,
		"unknown congestion": hyStream + `, "finalmask": {"quicParams": {"congestion": "cubic"}}`,
		"bad bandwidth":      hyStream + `, "finalmask": {"quicParams": {"brutalUp": "fast"}}`,
		"another transport":  `"network": "tcp", "hysteriaSettings": {"version": 2, "auth": "pw"}`,
		"hysteria version 1": `"network": "hysteria", "hysteriaSettings": {"version": 1, "auth": "pw"}`,
		"no auth":            `"network": "hysteria", "hysteriaSettings": {"version": 2}`,
	} {
		if n, err := parseOne(t, hysteriaOutbound(stream)); err == nil {
			t.Errorf("%s: accepted as %s", name, describe(n))
		}
	}
}

func TestXrayMbps(t *testing.T) {
	for in, want := range map[string]int{
		"": 0, "100 mbps": 100, "100Mbps": 100, "100m": 100, "1 gbps": 1024, "1.5g": 1536, "512 kbps": 1,
		"104857600": 100, "100 MB": 100,
	} {
		if got, err := xrayMbps(in); err != nil || got != want {
			t.Errorf("xrayMbps(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"fast", "-5 mbps", "100 furlongs", "1e9 tbps"} {
		if got, err := xrayMbps(bad); err == nil {
			t.Errorf("xrayMbps(%q) = %d, want an error", bad, got)
		}
	}
}

func TestHysteria2LinkPin(t *testing.T) {
	n, err := ParseLink("hysteria2://pw@hy.example.com:443/?sni=hy.example.com&pinSHA256=" +
		"21:14:0E:7C:D8:91:35:E9:7D:3F:9C:4B:89:A1:54:06:33:51:B2:77:E0:3F:D6:96:46:9F:DF:ED:12:FD:43:D0#Pinned")
	if err != nil || n.TLS.PinSHA256 != testPin {
		t.Errorf("pinned link: %s, %v", describe(n), err)
	}
	if _, err := ParseLink("hysteria2://pw@hy.example.com:443/?pinSHA256=zz#Bad"); err == nil {
		t.Error("a link with a pin that is not a SHA-256 was accepted")
	}
}
