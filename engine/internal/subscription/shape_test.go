package subscription

import (
	"strings"
	"testing"
)

func TestShapeKeepsTheLogicAndHidesTheSecrets(t *testing.T) {
	doc := `{
	  "remarks": "Автовыбор",
	  "outbounds": [
	    {"tag": "nl", "protocol": "vless", "settings": {"vnext": [{"address": "nl.secret.example", "port": 443,
	      "users": [{"id": "11111111-2222-3333-4444-555555555555", "flow": "xtls-rprx-vision", "encryption": "none"}]}]},
	     "streamSettings": {"network": "tcp", "security": "reality", "realitySettings": {"publicKey": "PUBLICKEYSECRET", "shortId": "abcd", "serverName": "sni.secret.example"}}},
	    {"tag": "a"}, {"tag": "b"}, {"tag": "c"}, {"tag": "d"}, {"tag": "e"}
	  ],
	  "routing": {"balancers": [{"tag": "auto", "selector": ["nl", "de"], "strategy": {"type": "leastPing"}, "fallbackTag": "direct"}]}
	}`
	out, err := Shape([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"11111111-2222", "nl.secret.example", "PUBLICKEYSECRET", "sni.secret.example", "abcd"} {
		if strings.Contains(out, secret) {
			t.Errorf("the shape leaks %q:\n%s", secret, out)
		}
	}
	for _, want := range []string{`"vless"`, `"reality"`, `"leastPing"`, `"auto"`, `"fallbackTag": "direct"`, `"xtls-rprx-vision"`, `"selector": [`, `"nl"`, `"Автовыбор"`, "443", "… 6 items in all"} {
		if !strings.Contains(out, want) {
			t.Errorf("the shape lost %s:\n%s", want, out)
		}
	}
	if !strings.Contains(out, `"address": "<string, 17 chars>"`) {
		t.Errorf("an address is not reduced to its length:\n%s", out)
	}
}

func TestShapeRejectsInvalidJSON(t *testing.T) {
	if _, err := Shape([]byte("vless://x@y:1")); err == nil {
		t.Error("a link is not JSON")
	}
}
