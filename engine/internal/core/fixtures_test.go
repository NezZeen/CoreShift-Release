package core

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/url"
	"testing"

	"coreshift/engine/internal/node"
	"coreshift/engine/internal/subscription"
)

const testUUID = "11111111-2222-3333-4444-555555555555"

// fixtures returns one node per protocol/transport combination worth
// rendering, parsed from share links so the whole pipeline is exercised.
// Keys are freshly generated and valid, because cores validate them.
func fixtures(t *testing.T) map[string]node.Node {
	t.Helper()
	x25519 := func() *ecdh.PrivateKey {
		k, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	realityKey := base64.RawURLEncoding.EncodeToString(x25519().PublicKey().Bytes())
	wgPriv := x25519()
	wgPrivB64 := base64.StdEncoding.EncodeToString(wgPriv.Bytes())
	wgPeerB64 := base64.StdEncoding.EncodeToString(x25519().PublicKey().Bytes())
	ss2022Key := base64.StdEncoding.EncodeToString(make([]byte, 16))
	vmess := func(json string) string { return "vmess://" + base64.StdEncoding.EncodeToString([]byte(json)) }
	ssUser := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret"))

	links := map[string]string{
		"vless-reality-vision": "vless://" + testUUID + "@203.0.113.1:443?encryption=none&flow=xtls-rprx-vision" +
			"&security=reality&sni=www.example.com&fp=chrome&pbk=" + realityKey + "&sid=6ba85179e30d4fc2&type=tcp",
		"vless-xhttp-reality": "vless://" + testUUID + "@203.0.113.1:443?type=xhttp&path=%2Fxh&mode=packet-up" +
			"&security=reality&sni=www.example.com&pbk=" + realityKey + "&sid=ab",
		"vless-ws-tls-ed": "vless://" + testUUID + "@edge.example.com:443?type=ws&path=%2Fws%3Fed%3D2048" +
			"&host=edge.example.com&security=tls&sni=edge.example.com&alpn=http%2F1.1",
		"vless-grpc-tls":    "vless://" + testUUID + "@203.0.113.2:443?type=grpc&serviceName=svc&security=tls&sni=g.example.com",
		"vless-httpupgrade": "vless://" + testUUID + "@203.0.113.2:443?type=httpupgrade&path=%2Fup&host=u.example.com&security=tls",
		"vless-h2":          "vless://" + testUUID + "@203.0.113.2:443?type=http&path=%2Fh2&host=h.example.com&security=tls",
		"vmess-ws-tls": vmess(`{"v":"2","ps":"ws","add":"203.0.113.6","port":443,"id":"` + testUUID +
			`","aid":0,"scy":"auto","net":"ws","host":"v.example.com","path":"/vm","tls":"tls","sni":"v.example.com"}`),
		"vmess-tcp-http": vmess(`{"v":"2","ps":"http","add":"203.0.113.6","port":80,"id":"` + testUUID +
			`","aid":0,"net":"tcp","type":"http","host":"www.example.com","path":"/"}`),
		"vmess-alterid": vmess(`{"v":"2","ps":"legacy","add":"203.0.113.6","port":443,"id":"` + testUUID +
			`","aid":64,"net":"tcp","tls":"tls"}`),
		"trojan-tcp":  "trojan://pw@203.0.113.5:443?sni=t.example.com",
		"trojan-grpc": "trojan://pw@203.0.113.5:443?type=grpc&serviceName=svc&sni=t.example.com",
		"ss-aes":      "ss://" + ssUser + "@203.0.113.7:8388",
		"ss-2022":     "ss://2022-blake3-aes-128-gcm:" + url.PathEscape(ss2022Key) + "@203.0.113.8:443",
		"ss-obfs": "ss://" + ssUser + "@203.0.113.7:8388/?plugin=" +
			url.QueryEscape("obfs-local;obfs=http;obfs-host=example.com"),
		"ss-v2ray": "ss://" + ssUser + "@203.0.113.7:443/?plugin=" +
			url.QueryEscape("v2ray-plugin;mode=websocket;tls;host=example.com;path=/v"),
		"hy2": "hy2://auth@203.0.113.10:443,20000-30000/?sni=h.example.com&obfs=salamander&obfs-password=ob",
		"tuic": "tuic://" + testUUID + ":pw@203.0.113.11:443?congestion_control=bbr&udp_relay_mode=native" +
			"&alpn=h3&sni=tu.example.com",
		"anytls": "anytls://pw@203.0.113.12:443?sni=a.example.com",
		"wireguard": "wg://" + url.PathEscape(wgPrivB64) + "@203.0.113.13:51820?publickey=" + url.QueryEscape(wgPeerB64) +
			"&address=10.7.0.2/32,fd00::2/128&reserved=1,2,3&mtu=1280",
	}
	out := map[string]node.Node{}
	for name, link := range links {
		n, err := subscription.ParseLink(link)
		if err != nil {
			t.Fatalf("fixture %s: %v", name, err)
		}
		n.Name = name
		out[name] = n
	}
	return out
}
