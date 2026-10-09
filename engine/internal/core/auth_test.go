package core

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestSOCKSAuthInConfigs(t *testing.T) {
	n := fixtures(t)["trojan-tcp"]
	auth := SOCKSAuth{User: "u-1234", Pass: "p-5678"}
	listen := netip.MustParseAddrPort("127.0.0.1:17890")

	x := decodeJSON(t, render(t, xray{}, n, Options{Listen: listen, Auth: auth}))
	if dig(x, "inbounds", 0, "settings", "auth") != "password" ||
		dig(x, "inbounds", 0, "settings", "accounts", 0, "user") != "u-1234" ||
		dig(x, "inbounds", 0, "settings", "accounts", 0, "pass") != "p-5678" {
		t.Errorf("xray inbound = %v", dig(x, "inbounds", 0))
	}
	s := decodeJSON(t, render(t, singBox{}, n, Options{Listen: listen, Auth: auth}))
	if dig(s, "inbounds", 0, "users", 0, "username") != "u-1234" || dig(s, "inbounds", 0, "users", 0, "password") != "p-5678" {
		t.Errorf("sing-box inbound = %v", dig(s, "inbounds", 0))
	}
	var m map[string]any
	if err := yaml.Unmarshal(render(t, mihomo{}, n, Options{Listen: listen, Auth: auth}), &m); err != nil {
		t.Fatal(err)
	}
	l := dig(m, "listeners", 0)
	if dig(l, "type") != "socks" || dig(l, "port") != 17890 || dig(l, "listen") != "127.0.0.1" ||
		dig(l, "users", 0, "username") != "u-1234" || dig(l, "users", 0, "password") != "p-5678" {
		t.Errorf("mihomo listener = %v", l)
	}
	if _, open := m["socks-port"]; open {
		t.Error("mihomo also opens the port without credentials")
	}

	// Without credentials, as before.
	x = decodeJSON(t, render(t, xray{}, n, Options{Listen: listen}))
	if dig(x, "inbounds", 0, "settings", "auth") != "noauth" {
		t.Errorf("xray inbound = %v", dig(x, "inbounds", 0))
	}
}

func TestSOCKSAuthStaysOutOfPrints(t *testing.T) {
	a := NewSOCKSAuth()
	if len(a.User) != 16 || len(a.Pass) != 32 || a == NewSOCKSAuth() {
		t.Fatalf("credentials %d/%d characters", len(a.User), len(a.Pass))
	}
	for _, s := range []string{fmt.Sprint(a), fmt.Sprintf("%+v", a), fmt.Sprintf("%#v", a), fmt.Sprintf("%v", Options{Auth: a})} {
		if strings.Contains(s, a.Pass) || strings.Contains(s, a.User) {
			t.Errorf("printed: %s", s)
		}
	}
	u := a.ProxyURL(netip.MustParseAddrPort("127.0.0.1:1"))
	if u.User.Username() != a.User || u.Host != "127.0.0.1:1" || u.Scheme != "socks5" {
		t.Errorf("proxy URL %s", u.Redacted())
	}
	if u := (SOCKSAuth{}).ProxyURL(netip.MustParseAddrPort("127.0.0.1:1")); u.User != nil {
		t.Error("credentials without any")
	}
}

// No core opens an API of its own: Xray's stats service takes no secret,
// and any program on the device could read it. The traffic is counted in
// front of the cores (socksgate).
func TestCoresOpenNoAPI(t *testing.T) {
	o := Options{Listen: netip.MustParseAddrPort("127.0.0.1:23456"), Auth: NewSOCKSAuth()}
	for name, n := range fixtures(t) {
		for _, a := range Adapters() {
			if a.Supports(&n) != nil {
				continue
			}
			cfg := string(render(t, a, n, o))
			for _, key := range []string{`"api"`, `"stats"`, "clash_api", "external-controller", "external_controller"} {
				if strings.Contains(cfg, key) {
					t.Errorf("%s, %s: %s in the config", name, a.Kind(), key)
				}
			}
		}
	}
}
