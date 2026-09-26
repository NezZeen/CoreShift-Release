package subscription

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"

	"coreshift/engine/internal/node"
)

// linkList is a plain subscription with a comment, blank lines and one
// unsupported entry.
var linkList = strings.Join([]string{
	"# synthetic subscription",
	"vless://" + uuid1 + "@203.0.113.1:443?encryption=none&flow=xtls-rprx-vision&security=reality" +
		"&sni=www.example.com&fp=chrome&pbk=PUBKEYbase64url_-&sid=6ba85179e30d4fc2&type=tcp#Amsterdam",
	"",
	"tuic://" + uuid3 + ":pa%24s@203.0.113.11:443?congestion_control=bbr&udp_relay_mode=native&alpn=h3&sni=tu.example.com#Stockholm",
	"socks://user:pass@203.0.113.99:1080#Socks",
	"anytls://pw@203.0.113.12:443?sni=a.example.com#Tokyo",
}, "\r\n")

func mustParse(t *testing.T, body []byte) Result {
	t.Helper()
	res, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestParseLinkList(t *testing.T) {
	res := mustParse(t, []byte(linkList))
	if res.Format != FormatLinks || len(res.Nodes) != 3 {
		t.Fatalf("format=%s nodes=%d, want links/3", res.Format, len(res.Nodes))
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Index != 3 || res.Skipped[0].Kind != "socks" {
		t.Errorf("skipped = %v, want #3 socks", res.Skipped)
	}
	if strings.Contains(res.Skipped[0].String(), "pass") {
		t.Errorf("skipped entry leaks credentials: %s", res.Skipped[0])
	}
}

func TestParseBase64(t *testing.T) {
	for name, enc := range map[string]*base64.Encoding{
		"std": base64.StdEncoding, "raw url": base64.RawURLEncoding,
	} {
		body := enc.EncodeToString([]byte(linkList))
		// Some panels wrap base64 at 76 columns.
		wrapped := body[:40] + "\n" + body[40:]
		res := mustParse(t, []byte(wrapped))
		if res.Format != FormatBase64 || len(res.Nodes) != 3 {
			t.Errorf("%s: format=%s nodes=%d, want base64/3", name, res.Format, len(res.Nodes))
		}
	}
}

func TestParseClash(t *testing.T) {
	body, err := os.ReadFile("testdata/clash.yaml")
	if err != nil {
		t.Fatal(err)
	}
	res := mustParse(t, body)
	if res.Format != FormatClash {
		t.Fatalf("format = %s", res.Format)
	}
	byName := index(res.Nodes)
	if len(res.Nodes) != 8 {
		t.Errorf("got %d nodes, want 8", len(res.Nodes))
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Kind != "socks5" {
		t.Errorf("skipped = %v, want the socks5 entry", res.Skipped)
	}

	if n := byName["Istanbul"]; n.Transport.Network != node.NetWS || n.Transport.Host != "v.example.com" || n.TLS == nil {
		t.Errorf("vmess ws = %s", describe(n))
	}
	if n := byName["Almaty obfs"]; n.Shadowsocks == nil || n.Shadowsocks.Plugin != "obfs-local" ||
		n.Shadowsocks.PluginOpts != "obfs=http;obfs-host=example.com" {
		t.Errorf("ss plugin = %s", describe(n))
	}
	if n := byName["Helsinki"]; n.Hysteria2 == nil || n.Hysteria2.UpMbps != 50 || n.Hysteria2.DownMbps != 200 ||
		n.Hysteria2.Ports != "20000-30000" {
		t.Errorf("hysteria2 = %s", describe(n))
	}
	if n := byName["Riga"]; n.WireGuard == nil || len(n.WireGuard.Address) != 2 || len(n.WireGuard.Reserved) != 3 {
		t.Errorf("wireguard = %s", describe(n))
	}
	if n := byName["New York"]; n.Password != "p@ss" || n.Transport.ServiceName != "svc" {
		t.Errorf("trojan = %s", describe(n))
	}
}

func TestParseSingBox(t *testing.T) {
	body, err := os.ReadFile("testdata/singbox.json")
	if err != nil {
		t.Fatal(err)
	}
	res := mustParse(t, body)
	if res.Format != FormatSingBox {
		t.Fatalf("format = %s", res.Format)
	}
	if len(res.Nodes) != 6 {
		t.Errorf("got %d nodes, want 6 (groups and direct are ignored)", len(res.Nodes))
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Kind != "naive" {
		t.Errorf("skipped = %v, want the naive entry", res.Skipped)
	}
	byName := index(res.Nodes)
	if n := byName["Helsinki"]; n.Hysteria2 == nil || n.Hysteria2.Ports != "20000-30000" || n.Hysteria2.Obfs != "salamander" {
		t.Errorf("hysteria2 = %s", describe(n))
	}
	if n := byName["Riga"]; n.Server != "203.0.113.13" || n.Port != 51820 || n.WireGuard.PeerPublicKey != "UEVFUg==" {
		t.Errorf("wireguard endpoint = %s", describe(n))
	}
}

// The same server described in any format must become the same node.
func TestFormatsAgree(t *testing.T) {
	clashBody, _ := os.ReadFile("testdata/clash.yaml")
	sbBody, _ := os.ReadFile("testdata/singbox.json")
	links := index(mustParse(t, []byte(linkList)).Nodes)
	clash := index(mustParse(t, clashBody).Nodes)
	sb := index(mustParse(t, sbBody).Nodes)

	pairs := []struct{ link, other string }{
		{"Amsterdam", "🇳🇱 Amsterdam"},
		{"Stockholm", "Stockholm"},
	}
	for _, p := range pairs {
		want, ok := links[p.link]
		if !ok {
			t.Fatalf("link node %q missing", p.link)
		}
		for format, nodes := range map[string]map[string]node.Node{"clash": clash, "sing-box": sb} {
			got, ok := nodes[p.other]
			if !ok {
				t.Fatalf("%s node %q missing", format, p.other)
			}
			if got.Fingerprint() != want.Fingerprint() {
				t.Errorf("%s %s differs from link:\n got  %s\n want %s", format, p.other, describe(got), describe(want))
			}
		}
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"empty":      "  \n ",
		"garbage":    "hello world",
		"xray json":  `{"outbounds":[{"protocol":"vless","settings":{}}]}`,
		"xray array": `[{"outbounds":[]}]`,
		"html":       "<html><body>login</body></html>",
	}
	for name, body := range cases {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	_, err := Parse([]byte("socks://a@203.0.113.1:1\nhttp://b@203.0.113.2:2"))
	if !errors.Is(err, ErrNoNodes) {
		t.Errorf("all-unsupported list: err = %v, want ErrNoNodes", err)
	}

	// A subscription URL pasted where its content is expected.
	_, err = Parse([]byte("https://panel.example.com/sub/TOKEN"))
	if err == nil || !strings.Contains(err.Error(), "subscription URL") || strings.Contains(err.Error(), "TOKEN") {
		t.Errorf("subscription URL: err = %v", err)
	}
}

func index(nodes []node.Node) map[string]node.Node {
	m := map[string]node.Node{}
	for _, n := range nodes {
		m[n.Name] = n
	}
	return m
}
