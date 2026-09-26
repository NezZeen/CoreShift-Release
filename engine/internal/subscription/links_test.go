package subscription

import (
	"encoding/base64"
	"encoding/json"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"coreshift/engine/internal/node"
)

const (
	uuid1 = "11111111-2222-3333-4444-555555555555"
	uuid2 = "22222222-2222-3333-4444-555555555555"
	uuid3 = "33333333-2222-3333-4444-555555555555"
)

func TestParseLink(t *testing.T) {
	vmessJSON := base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"Istanbul","add":"203.0.113.6","port":"443",
		"id":"` + uuid2 + `","aid":0,"scy":"auto","net":"ws","type":"none","host":"v.example.com","path":"/vm",
		"tls":"tls","sni":"v.example.com","alpn":"","fp":""}`))
	ssUser := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret"))
	ssLegacy := base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pw@203.0.113.9:8388"))

	cases := []struct {
		name string
		link string
		want node.Node
	}{
		{
			name: "vless reality vision",
			link: "vless://" + uuid1 + "@203.0.113.1:443?encryption=none&flow=xtls-rprx-vision&security=reality" +
				"&sni=www.example.com&fp=chrome&pbk=PUBKEYbase64url_-&sid=6ba85179e30d4fc2&spx=%2F&type=tcp&headerType=none" +
				"#%F0%9F%87%B3%F0%9F%87%B1%20Amsterdam",
			want: node.Node{
				Name: "🇳🇱 Amsterdam", Protocol: node.VLESS, Server: "203.0.113.1", Port: 443,
				UUID: uuid1, Flow: "xtls-rprx-vision",
				TLS: &node.TLS{ServerName: "www.example.com", Fingerprint: "chrome",
					Reality: &node.Reality{PublicKey: "PUBKEYbase64url_-", ShortID: "6ba85179e30d4fc2", SpiderX: "/"}},
			},
		},
		{
			name: "vless xhttp over ipv6",
			link: "vless://" + uuid1 + "@[2001:db8::1]:8443?security=reality&type=xhttp&path=%2Fxh&host=cdn.example.com" +
				"&mode=packet-up&sni=cdn.example.com&pbk=K&fp=firefox#Frankfurt%20XHTTP",
			want: node.Node{
				Name: "Frankfurt XHTTP", Protocol: node.VLESS, Server: "2001:db8::1", Port: 8443, UUID: uuid1,
				Transport: node.Transport{Network: node.NetXHTTP, Path: "/xh", Host: "cdn.example.com", Mode: "packet-up"},
				TLS:       &node.TLS{ServerName: "cdn.example.com", Fingerprint: "firefox", Reality: &node.Reality{PublicKey: "K"}},
			},
		},
		{
			name: "vless ws with early data",
			link: "vless://" + uuid1 + "@edge.example.com:443?security=tls&type=ws&path=%2Fws%3Fed%3D2048" +
				"&host=edge.example.com&alpn=h2,http/1.1&allowInsecure=1#WS",
			want: node.Node{
				Name: "WS", Protocol: node.VLESS, Server: "edge.example.com", Port: 443, UUID: uuid1,
				Transport: node.Transport{Network: node.NetWS, Path: "/ws", Host: "edge.example.com", EarlyData: 2048},
				TLS:       &node.TLS{ALPN: []string{"h2", "http/1.1"}, Insecure: true},
			},
		},
		{
			name: "trojan grpc defaults to tls",
			link: "trojan://p%40ss@203.0.113.5:443?type=grpc&serviceName=svc&sni=t.example.com#NY",
			want: node.Node{
				Name: "NY", Protocol: node.Trojan, Server: "203.0.113.5", Port: 443, Password: "p@ss",
				Transport: node.Transport{Network: node.NetGRPC, ServiceName: "svc"},
				TLS:       &node.TLS{ServerName: "t.example.com"},
			},
		},
		{
			name: "vmess v2rayN json",
			link: "vmess://" + vmessJSON,
			want: node.Node{
				Name: "Istanbul", Protocol: node.VMess, Server: "203.0.113.6", Port: 443, UUID: uuid2, Cipher: "auto",
				Transport: node.Transport{Network: node.NetWS, Path: "/vm", Host: "v.example.com"},
				TLS:       &node.TLS{ServerName: "v.example.com"},
			},
		},
		{
			name: "shadowsocks sip002 base64 userinfo",
			link: "ss://" + ssUser + "@203.0.113.7:8388#Almaty",
			want: node.Node{Name: "Almaty", Protocol: node.Shadowsocks, Server: "203.0.113.7", Port: 8388,
				Cipher: "aes-256-gcm", Password: "secret"},
		},
		{
			name: "shadowsocks 2022 percent-encoded",
			link: "ss://2022-blake3-aes-128-gcm:AAAAAAAAAAAAAAAAAAAAAA%3D%3D@203.0.113.8:443#SS2022",
			want: node.Node{Name: "SS2022", Protocol: node.Shadowsocks, Server: "203.0.113.8", Port: 443,
				Cipher: "2022-blake3-aes-128-gcm", Password: "AAAAAAAAAAAAAAAAAAAAAA=="},
		},
		{
			name: "shadowsocks legacy base64",
			link: "ss://" + ssLegacy + "#Legacy",
			want: node.Node{Name: "Legacy", Protocol: node.Shadowsocks, Server: "203.0.113.9", Port: 8388,
				Cipher: "chacha20-ietf-poly1305", Password: "pw"},
		},
		{
			name: "shadowsocks plugin",
			link: "ss://" + ssUser + "@203.0.113.7:8388/?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dexample.com#Obfs",
			want: node.Node{Name: "Obfs", Protocol: node.Shadowsocks, Server: "203.0.113.7", Port: 8388,
				Cipher: "aes-256-gcm", Password: "secret",
				Shadowsocks: &node.ShadowsocksOptions{Plugin: "obfs-local", PluginOpts: "obfs=http;obfs-host=example.com"}},
		},
		{
			name: "hysteria2 port hopping",
			link: "hy2://auth%3Apass@203.0.113.10:443,20000-30000/?sni=h.example.com&obfs=salamander&obfs-password=ob&insecure=1#Helsinki",
			want: node.Node{
				Name: "Helsinki", Protocol: node.Hysteria2, Server: "203.0.113.10", Port: 443, Password: "auth:pass",
				TLS:       &node.TLS{ServerName: "h.example.com", Insecure: true},
				Hysteria2: &node.Hysteria2Options{Obfs: "salamander", ObfsPassword: "ob", Ports: "443,20000-30000"},
			},
		},
		{
			name: "tuic",
			link: "tuic://" + uuid3 + ":pa%24s@203.0.113.11:443?congestion_control=bbr&udp_relay_mode=native&alpn=h3&sni=tu.example.com#Stockholm",
			want: node.Node{
				Name: "Stockholm", Protocol: node.TUIC, Server: "203.0.113.11", Port: 443, UUID: uuid3, Password: "pa$s",
				TLS:  &node.TLS{ServerName: "tu.example.com", ALPN: []string{"h3"}},
				TUIC: &node.TUICOptions{CongestionControl: "bbr", UDPRelayMode: "native"},
			},
		},
		{
			name: "anytls",
			link: "anytls://pw@203.0.113.12:443?sni=a.example.com&insecure=1#Tokyo",
			want: node.Node{Name: "Tokyo", Protocol: node.AnyTLS, Server: "203.0.113.12", Port: 443, Password: "pw",
				TLS: &node.TLS{ServerName: "a.example.com", Insecure: true}},
		},
		{
			name: "wireguard with unescaped base64 key",
			link: "wg://aGVsbG8/d29y+bGQ=@203.0.113.13:51820?publickey=UEVFUg%3D%3D&address=10.7.0.2/32,fd00::2&reserved=1,2,3&mtu=1280#Riga",
			want: node.Node{
				Name: "Riga", Protocol: node.WireGuard, Server: "203.0.113.13", Port: 51820,
				WireGuard: &node.WireGuardOptions{
					PrivateKey: "aGVsbG8/d29y+bGQ=", PeerPublicKey: "UEVFUg==",
					Address:  []netip.Prefix{netip.MustParsePrefix("10.7.0.2/32"), netip.MustParsePrefix("fd00::2/128")},
					Reserved: []uint8{1, 2, 3}, MTU: 1280,
				},
			},
		},
		{
			name: "name defaults to address",
			link: "anytls://pw@203.0.113.12:443",
			want: node.Node{Name: "203.0.113.12:443", Protocol: node.AnyTLS, Server: "203.0.113.12", Port: 443,
				Password: "pw", TLS: &node.TLS{}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseLink(c.link)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got  %+v\nwant %+v", describe(got), describe(c.want))
			}
		})
	}
}

func TestParseLinkErrors(t *testing.T) {
	const secret = "0badc0de-secret-uuid"
	cases := map[string]string{
		"missing port":      "vless://" + secret + "@203.0.113.1?security=tls",
		"unsupported":       "socks://" + secret + "@203.0.113.1:1080",
		"missing uuid":      "vless://@203.0.113.1:443",
		"unknown transport": "vless://" + secret + "@203.0.113.1:443?type=kcp",
		"bad security":      "vless://" + secret + "@203.0.113.1:443?security=xtls2",
		"ss garbage":        "ss://!!!" + secret,
		"vmess garbage":     "vmess://!!!" + secret,
		"reality no key":    "vless://" + secret + "@203.0.113.1:443?security=reality",
		"not a link":        secret,
	}
	for name, link := range cases {
		_, err := ParseLink(link)
		if err == nil {
			t.Errorf("%s: expected an error", name)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: error leaks credentials: %v", name, err)
		}
	}
}

// describe renders a node with its pointer fields expanded, for readable failures.
func describe(n node.Node) string {
	b, _ := json.Marshal(n)
	return string(b)
}
