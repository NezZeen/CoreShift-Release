package core

import (
	"strings"
	"testing"

	"coreshift/engine/internal/node"
)

// A node saved by an earlier version keeps the extra its panel sent; the
// xray config must still not carry file paths or socket options.
func TestXrayDropsDangerousXHTTPExtra(t *testing.T) {
	n := node.Node{Name: "x", Protocol: node.VLESS, Server: "example.com", Port: 443, UUID: "00000000-0000-0000-0000-000000000001",
		Transport: node.Transport{Network: node.NetXHTTP, Path: "/x",
			Extra: `{"xPaddingBytes":"100-1000","downloadSettings":{"address":"d.example.com","port":443,"network":"xhttp","security":"tls",` +
				`"tlsSettings":{"serverName":"d.example.com","masterKeyLog":"C:\\Windows\\evil.log","certificates":[{"certificateFile":"C:\\a.pem"}]},` +
				`"sockopt":{"dialerProxy":"direct"}}}`},
		TLS: &node.TLS{ServerName: "example.com"}}
	b, err := Adapters()[0].Render(&n, Options{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	for _, bad := range []string{"masterKeyLog", "certificates", "certificateFile", "evil.log", "dialerProxy"} {
		if strings.Contains(cfg, bad) {
			t.Errorf("config carries %q:\n%s", bad, cfg)
		}
	}
	if !strings.Contains(cfg, "d.example.com") || !strings.Contains(cfg, "100-1000") {
		t.Errorf("harmless extra lost:\n%s", cfg)
	}
}

func TestSingBoxPluginOptsLeaveOutFiles(t *testing.T) {
	n := node.Node{Name: "ss", Protocol: node.Shadowsocks, Server: "example.com", Port: 443, Cipher: "aes-128-gcm", Password: "p",
		Shadowsocks: &node.ShadowsocksOptions{Plugin: "v2ray-plugin", PluginOpts: "tls;host=example.com;cert=C:\\Windows\\System32\\config\\SAM;path=/ws"}}
	b, err := singBox{}.Render(&n, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "cert=") || strings.Contains(string(b), "SAM") {
		t.Errorf("plugin options carry a file:\n%s", b)
	}
	if !strings.Contains(string(b), `"tls;host=example.com;path=/ws"`) {
		t.Errorf("plugin options lost:\n%s", b)
	}
	if got := sip003Clean("obfs-local", "obfs=http;obfs-host=a.example;x=y"); got != "obfs=http;obfs-host=a.example" {
		t.Errorf("obfs-local options = %q", got)
	}
}
