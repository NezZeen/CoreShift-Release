package tunlayer

import "testing"

func TestUpstreamCredentials(t *testing.T) {
	o := baseOptions()
	o.UpstreamUser, o.UpstreamPass = "u-1", "p-2"
	out := find(list(render(t, o), "outbounds"), map[string]any{"tag": tagProxy})
	if out == nil || out["type"] != "socks" || out["username"] != "u-1" || out["password"] != "p-2" {
		t.Errorf("proxy outbound = %v", out)
	}
	out = find(list(render(t, baseOptions()), "outbounds"), map[string]any{"tag": tagProxy})
	if _, ok := out["username"]; ok {
		t.Errorf("credentials without any: %v", out)
	}
}
