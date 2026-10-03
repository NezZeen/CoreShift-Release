package node

import (
	"net/netip"
	"testing"
)

func sample() Node {
	return Node{
		Name: "Amsterdam", Protocol: VLESS, Server: "203.0.113.1", Port: 443,
		UUID: "11111111-2222-3333-4444-555555555555", Flow: "xtls-rprx-vision",
		TLS: &TLS{ServerName: "www.example.com", Fingerprint: "chrome", Reality: &Reality{PublicKey: "pk", ShortID: "ab"}},
	}
}

func TestFingerprintIgnoresName(t *testing.T) {
	a, b := sample(), sample()
	b.Name = "Amsterdam (renamed)"
	if a.Fingerprint() != b.Fingerprint() {
		t.Error("renaming must not change the fingerprint")
	}
	b.Port = 8443
	if a.Fingerprint() == b.Fingerprint() {
		t.Error("changing the port must change the fingerprint")
	}
}

func TestValidate(t *testing.T) {
	ok := sample()
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid node rejected: %v", err)
	}
	cases := map[string]func(*Node){
		"no server":      func(n *Node) { n.Server = "" },
		"no port":        func(n *Node) { n.Port = 0 },
		"no uuid":        func(n *Node) { n.UUID = "" },
		"reality no key": func(n *Node) { n.TLS.Reality.PublicKey = "" },
		"unknown proto":  func(n *Node) { n.Protocol = "socks" },
		"ss no cipher":   func(n *Node) { n.Protocol = Shadowsocks; n.Password = "x" },
		"tuic no pass":   func(n *Node) { n.Protocol = TUIC },
		"wg no address": func(n *Node) {
			n.Protocol = WireGuard
			n.WireGuard = &WireGuardOptions{PrivateKey: "a", PeerPublicKey: "b"}
		},
	}
	for name, mutate := range cases {
		n := sample()
		mutate(&n)
		if n.Validate() == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	wg := Node{Protocol: WireGuard, Server: "203.0.113.9", Port: 51820, WireGuard: &WireGuardOptions{
		PrivateKey: "a", PeerPublicKey: "b", Address: []netip.Prefix{netip.MustParsePrefix("10.0.0.2/32")},
	}}
	if err := wg.Validate(); err != nil {
		t.Errorf("valid wireguard node rejected: %v", err)
	}
}

func TestNormalizePorts(t *testing.T) {
	for in, want := range map[string]string{"443": "443", " 20000 - 30000 , 443 ": "20000-30000,443", "1-65535": "1-65535", "443,": "443"} {
		if got, err := NormalizePorts(in); err != nil || got != want {
			t.Errorf("NormalizePorts(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", ",", "0", "65536", "30000-20000", "env:PORTS", "1-2-3", "443;444", "-5"} {
		if got, err := NormalizePorts(bad); err == nil {
			t.Errorf("NormalizePorts(%q) = %q, want an error", bad, got)
		}
	}
}

func TestNormalizePin(t *testing.T) {
	const want = "21140e7cd89135e97d3f9c4b89a154063351b277e03fd696469fdfed12fd43d0"
	for _, in := range []string{want, " 21:14:0E:7C:D8:91:35:E9:7D:3F:9C:4B:89:A1:54:06:33:51:B2:77:E0:3F:D6:96:46:9F:DF:ED:12:FD:43:D0 "} {
		if got, err := NormalizePin(in); err != nil || got != want {
			t.Errorf("NormalizePin(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "abcd", want + "," + want, want[:62] + "zz"} {
		if got, err := NormalizePin(bad); err == nil {
			t.Errorf("NormalizePin(%q) = %q, want an error", bad, got)
		}
	}
}
