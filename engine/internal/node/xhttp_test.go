package node

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeXHTTPExtraDropsFilesAndSockets(t *testing.T) {
	evil := `{
		"xPaddingBytes": "100-1000",
		"noGRPCHeader": true,
		"headers": {"X-A": "b", "X-Bad": {"nested": 1}},
		"xmux": {"maxConcurrency": "16-32", "hKeepAlivePeriod": 30, "evil": "x"},
		"sockopt": {"interface": "eth0"},
		"downloadSettings": {
			"address": "cdn.example.com",
			"port": 443,
			"network": "xhttp",
			"security": "tls",
			"tlsSettings": {
				"serverName": "cdn.example.com",
				"alpn": ["h2", 7],
				"masterKeyLog": "C:\\Windows\\System32\\evil.dll",
				"certificates": [{"certificateFile": "C:\\secret.pem", "keyFile": "C:\\key.pem"}]
			},
			"realitySettings": {"publicKey": "pk", "privateKey": "server-side", "dest": "x:443"},
			"sockopt": {"dialerProxy": "x", "mark": 1},
			"xhttpSettings": {
				"path": "/down",
				"downloadSettings": {"address": "nested.example.com"},
				"xmux": {"cMaxReuseTimes": 64}
			}
		}
	}`
	got, err := SanitizeXHTTPExtra(evil)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"masterKeyLog", "certificates", "certificateFile", "keyFile", "sockopt", "evil", "privateKey",
		"dest", "nested.example.com", "X-Bad", "dialerProxy", ".pem", ".dll"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q kept in %s", bad, got)
		}
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Fatal(err)
	}
	ds := v["downloadSettings"].(map[string]any)
	if ds["address"] != "cdn.example.com" || ds["port"] != float64(443) || ds["security"] != "tls" {
		t.Errorf("downloadSettings = %v", ds)
	}
	tls := ds["tlsSettings"].(map[string]any)
	if tls["serverName"] != "cdn.example.com" || len(tls["alpn"].([]any)) != 1 {
		t.Errorf("tlsSettings = %v", tls)
	}
	if ds["xhttpSettings"].(map[string]any)["path"] != "/down" {
		t.Errorf("xhttpSettings = %v", ds["xhttpSettings"])
	}
	if v["xPaddingBytes"] != "100-1000" || v["noGRPCHeader"] != true || v["headers"].(map[string]any)["X-A"] != "b" {
		t.Errorf("traffic options lost: %s", got)
	}
}

func TestSanitizeXHTTPExtraKeepsHarmlessAsIs(t *testing.T) {
	for _, raw := range []string{
		`{"xPaddingBytes":"100-1000","xmux":{"maxConcurrency":"16-32"}}`,
		`{ "scMaxEachPostBytes": {"from": 500000, "to": 1000000}, "noSSEHeader": false }`,
		`{"downloadSettings":{"address":"a.example","port":443,"network":"xhttp","security":"reality","realitySettings":{"serverName":"a.example","publicKey":"k","shortId":"ab","fingerprint":"chrome"},"xhttpSettings":{"path":"/d","mode":"stream-up"}}}`,
	} {
		got, err := SanitizeXHTTPExtra(raw)
		if err != nil || got != raw {
			t.Errorf("SanitizeXHTTPExtra(%s) = %s, %v; want it unchanged", raw, got, err)
		}
	}
}

func TestSanitizeXHTTPExtraRejectsNonObjects(t *testing.T) {
	for _, raw := range []string{`[1]`, `"x"`, `{`, `{} {}`} {
		if _, err := SanitizeXHTTPExtra(raw); err == nil {
			t.Errorf("%s accepted", raw)
		}
	}
	if got, err := SanitizeXHTTPExtra(`{"sockopt":{"mark":1}}`); err != nil || got != "" {
		t.Errorf("only forbidden keys: %q, %v", got, err)
	}
}
