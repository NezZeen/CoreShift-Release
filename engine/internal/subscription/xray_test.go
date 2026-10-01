package subscription

import (
	"strings"
	"testing"

	"coreshift/engine/internal/node"
)

// What Remnawave serves to an Xray-based app: a list of complete configs, one
// per server, whose proxy outbound is tagged "proxy" and whose name is the
// config's "remarks".
const xrayList = `[
  {"remarks": "🇳🇱 Нидерланды",
   "inbounds": [{"tag": "socks", "port": 10808, "protocol": "socks"}],
   "outbounds": [
     {"tag": "proxy", "protocol": "vless",
      "settings": {"vnext": [{"address": "nl.example", "port": 443,
        "users": [{"id": "11111111-2222-3333-4444-555555555555", "flow": "xtls-rprx-vision", "encryption": "none"}]}]},
      "streamSettings": {"network": "tcp", "security": "reality",
        "realitySettings": {"serverName": "sni.example", "fingerprint": "chrome", "publicKey": "PBK", "shortId": "ab", "spiderX": "/"}}},
     {"tag": "direct", "protocol": "freedom"},
     {"tag": "block", "protocol": "blackhole"}]},
  {"remarks": "🇩🇪 Германия",
   "outbounds": [
     {"tag": "proxy", "protocol": "vmess",
      "settings": {"vnext": [{"address": "de.example", "port": 8443,
        "users": [{"id": "66666666-7777-8888-9999-000000000000", "alterId": 0, "security": "auto"}]}]},
      "streamSettings": {"network": "ws", "security": "tls",
        "tlsSettings": {"serverName": "de.example", "alpn": ["h2", "http/1.1"], "fingerprint": "firefox", "allowInsecure": false},
        "wsSettings": {"path": "/ws?ed=2048", "headers": {"Host": "cdn.example"}}}},
     {"tag": "direct", "protocol": "freedom"}]}
]`

// One config holding every server, behind a balancer: the servers are the
// outbounds whose tags start with "proxy", in the order listed.
const xrayBalanced = `{
  "remarks": "Автовыбор",
  "routing": {"balancers": [{"tag": "Super_Balancer", "selector": ["proxy"], "strategy": {"type": "leastLoad"}, "fallbackTag": "direct"}]},
  "burstObservatory": {"subjectSelector": ["proxy"]},
  "outbounds": [
    {"tag": "proxy", "protocol": "vless",
     "settings": {"vnext": [{"address": "a.example", "port": 443, "users": [{"id": "aaaaaaaa-0000-0000-0000-000000000001"}]}]},
     "streamSettings": {"network": "xhttp", "security": "tls", "tlsSettings": {"serverName": "a.example"},
       "xhttpSettings": {"path": "/x", "host": "a.example", "mode": "packet-up", "extra": {"xPaddingBytes": "100-1000"}}}},
    {"tag": "proxy-2", "protocol": "trojan",
     "settings": {"servers": [{"address": "b.example", "port": 443, "password": "secret"}]},
     "streamSettings": {"network": "grpc", "security": "tls", "tlsSettings": {"serverName": "b.example"},
       "grpcSettings": {"serviceName": "svc", "multiMode": true}}},
    {"tag": "proxy-3", "protocol": "shadowsocks",
     "settings": {"servers": [{"address": "c.example", "port": 8388, "method": "chacha20-ietf-poly1305", "password": "pw"}]}},
    {"tag": "direct", "protocol": "freedom"},
    {"tag": "block", "protocol": "blackhole"}
  ]
}`

func TestParseXrayList(t *testing.T) {
	res, err := Parse([]byte(xrayList))
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatXray || len(res.Nodes) != 2 || len(res.Skipped) != 0 {
		t.Fatalf("format %s, %d nodes, skipped %v", res.Format, len(res.Nodes), res.Skipped)
	}
	nl, de := res.Nodes[0], res.Nodes[1]
	if nl.Name != "🇳🇱 Нидерланды" || nl.Protocol != node.VLESS || nl.Server != "nl.example" || nl.Port != 443 ||
		nl.UUID != "11111111-2222-3333-4444-555555555555" || nl.Flow != "xtls-rprx-vision" || nl.Encryption != "" {
		t.Errorf("first node = %+v", nl)
	}
	if nl.TLS == nil || nl.TLS.Reality == nil || nl.TLS.Reality.PublicKey != "PBK" || nl.TLS.Reality.ShortID != "ab" ||
		nl.TLS.Reality.SpiderX != "/" || nl.TLS.ServerName != "sni.example" || nl.TLS.Fingerprint != "chrome" {
		t.Errorf("first node's REALITY = %+v", nl.TLS)
	}
	if de.Name != "🇩🇪 Германия" || de.Protocol != node.VMess || de.Port != 8443 || de.Cipher != "auto" {
		t.Errorf("second node = %+v", de)
	}
	if de.Transport.Network != node.NetWS || de.Transport.Path != "/ws" || de.Transport.EarlyData != 2048 || de.Transport.Host != "cdn.example" {
		t.Errorf("second node's transport = %+v", de.Transport)
	}
	if de.TLS == nil || de.TLS.ServerName != "de.example" || len(de.TLS.ALPN) != 2 || de.TLS.Fingerprint != "firefox" || de.TLS.Insecure {
		t.Errorf("second node's TLS = %+v", de.TLS)
	}
}

func TestParseXrayBalancedConfigKeepsTheServerOrder(t *testing.T) {
	res, err := Parse([]byte(xrayBalanced))
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatXray || len(res.Nodes) != 3 {
		t.Fatalf("format %s, %d nodes, skipped %v", res.Format, len(res.Nodes), res.Skipped)
	}
	var names []string
	for _, n := range res.Nodes {
		names = append(names, n.Name)
	}
	if got := strings.Join(names, ","); got != "proxy,proxy-2,proxy-3" {
		t.Errorf("servers in the order %q, want the outbounds' order", got)
	}
	a, b, c := res.Nodes[0], res.Nodes[1], res.Nodes[2]
	if a.Transport.Network != node.NetXHTTP || a.Transport.Path != "/x" || a.Transport.Mode != "packet-up" ||
		a.Transport.Extra != `{"xPaddingBytes":"100-1000"}` {
		t.Errorf("xhttp transport = %+v", a.Transport)
	}
	if b.Protocol != node.Trojan || b.Password != "secret" || b.Transport.Network != node.NetGRPC ||
		b.Transport.ServiceName != "svc" || b.Transport.Mode != "multi" {
		t.Errorf("trojan node = %+v", b)
	}
	if c.Protocol != node.Shadowsocks || c.Cipher != "chacha20-ietf-poly1305" || c.Password != "pw" || c.Port != 8388 || c.TLS != nil {
		t.Errorf("shadowsocks node = %+v", c)
	}
}

func TestParseXrayServerNamedByItsConfigWhenTheTagIsGeneric(t *testing.T) {
	res, err := Parse([]byte(`{"remarks": "Home", "outbounds": [{"tag": "proxy", "protocol": "trojan",
	  "settings": {"servers": [{"address": "h.example", "port": 443, "password": "p"}]},
	  "streamSettings": {"security": "tls"}}]}`))
	if err != nil || len(res.Nodes) != 1 || res.Nodes[0].Name != "Home" {
		t.Fatalf("nodes %+v, err %v", res.Nodes, err)
	}
}

func TestParseXrayKeepsWhatItCanAndReportsTheRest(t *testing.T) {
	res, err := Parse([]byte(`{"outbounds": [
	  {"tag": "ok", "protocol": "trojan", "settings": {"servers": [{"address": "h.example", "port": 443, "password": "p"}]}},
	  {"tag": "kcp", "protocol": "vless", "settings": {"vnext": [{"address": "k.example", "port": 1, "users": [{"id": "u"}]}]},
	   "streamSettings": {"network": "kcp"}},
	  {"tag": "hy", "protocol": "hysteria", "settings": {"address": "x", "port": 1}},
	  {"tag": "direct", "protocol": "freedom"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes) != 1 || res.Nodes[0].Name != "ok" {
		t.Errorf("nodes = %+v", res.Nodes)
	}
	if len(res.Skipped) != 2 || res.Skipped[0].Kind != "vless" || !strings.Contains(res.Skipped[0].Reason, "kcp") ||
		res.Skipped[1].Kind != "hysteria" {
		t.Errorf("skipped = %v", res.Skipped)
	}
}

func TestParseXrayWireGuard(t *testing.T) {
	res, err := Parse([]byte(`[{"outbounds": [{"tag": "wg", "protocol": "wireguard", "settings": {
	  "secretKey": "SECRET", "address": ["10.0.0.2/32"], "mtu": 1280, "reserved": [1, 2, 3],
	  "peers": [{"publicKey": "PEER", "endpoint": "wg.example:51820", "preSharedKey": "PSK"}]}}]}]`))
	if err != nil || len(res.Nodes) != 1 {
		t.Fatalf("nodes %+v, err %v", res.Nodes, err)
	}
	n := res.Nodes[0]
	if n.Protocol != node.WireGuard || n.Server != "wg.example" || n.Port != 51820 || n.WireGuard.PrivateKey != "SECRET" ||
		n.WireGuard.PeerPublicKey != "PEER" || n.WireGuard.PreSharedKey != "PSK" || n.WireGuard.MTU != 1280 ||
		len(n.WireGuard.Address) != 1 || len(n.WireGuard.Reserved) != 3 {
		t.Errorf("wireguard node = %+v", n)
	}
}

// The template a panel admin edits is not what an app receives: it has no
// servers yet, only the place where the panel will put them.
func TestParseRemnawaveTemplate(t *testing.T) {
	_, err := Parse([]byte(`{"outbounds": [{"tag": "direct", "protocol": "freedom"}],
	  "remnawave": {"injectHosts": [{"selector": {"type": "uuids", "values": ["x"]}, "tagPrefix": "proxy", "selectFrom": "ALL"}]}}`))
	if err == nil || !strings.Contains(err.Error(), "Remnawave template") {
		t.Errorf("err = %v, want it to say this is a template", err)
	}
}

func TestSingBoxJSONStillParses(t *testing.T) {
	// The Xray check must not catch sing-box configs, whose outbounds say "type".
	res, err := Parse([]byte(`{"outbounds": [{"type": "selector", "tag": "sel", "outbounds": ["a"]},
	  {"type": "trojan", "tag": "a", "server": "a.example", "server_port": 443, "password": "p", "tls": {"enabled": true}}]}`))
	if err != nil || res.Format != FormatSingBox || len(res.Nodes) != 1 {
		t.Fatalf("format %s, nodes %+v, err %v", res.Format, res.Nodes, err)
	}
}
