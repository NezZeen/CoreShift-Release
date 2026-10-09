package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// A server the user removed from the list is not switched to.
func TestFailoverSkipsHiddenServers(t *testing.T) {
	h, st, sub := switchHarness(t, panelJSON, "proxy")
	if _, err := st.SetHidden(sub.ID, []string{sub.Nodes[1].Fingerprint()}, true); err != nil {
		t.Fatal(err)
	}
	serverNotAnswering(h)
	waitNode(t, h, "proxy-3")
}

func TestAPIHideServers(t *testing.T) {
	h, srv := newStoreAPI(t, nil)
	var sub subscriptionView
	callJSON(t, srv, "POST", "/v1/subscriptions", map[string]string{"content": trojanLink + "\n" + hy2Link}, &sub)
	hy2 := sub.Nodes[1].Fingerprint
	path := "/v1/subscriptions/" + sub.ID + "/hidden"

	if code := callJSON(t, srv, "POST", path, map[string]any{"fingerprints": []string{hy2}}, nil); code != http.StatusBadRequest {
		t.Errorf("without hidden: %d", code)
	}
	if code := callJSON(t, srv, "POST", path, map[string]any{"fingerprints": []string{"nope"}, "hidden": true}, nil); code != http.StatusBadRequest {
		t.Errorf("unknown server: %d", code)
	}
	if code := callJSON(t, srv, "POST", "/v1/subscriptions/nope/hidden", map[string]any{"fingerprints": []string{hy2}, "hidden": true}, nil); code != http.StatusNotFound {
		t.Errorf("unknown subscription: %d", code)
	}
	// The token is needed here as everywhere.
	req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(`{"fingerprints":["x"],"hidden":true}`))
	if resp, err := srv.Client().Do(req); err != nil {
		t.Error(err)
	} else {
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("without the token: %d", resp.StatusCode)
		}
	}

	if code := callJSON(t, srv, "POST", path, map[string]any{"fingerprints": []string{hy2}, "hidden": true}, &sub); code != http.StatusOK {
		t.Fatalf("hide: %d", code)
	}
	if len(sub.Nodes) != 2 || sub.Nodes[0].Hidden || !sub.Nodes[1].Hidden {
		t.Fatalf("nodes after hiding = %+v", sub.Nodes)
	}
	// The latency test leaves it out.
	res, err := h.svc.TestLatency(context.Background(), sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Fingerprint != sub.Nodes[0].Fingerprint {
		t.Errorf("latency of %+v, want the visible server only", res)
	}

	var back subscriptionView
	if code := callJSON(t, srv, "POST", path, map[string]any{"fingerprints": []string{hy2}, "hidden": false}, &back); code != http.StatusOK || len(back.Nodes) != 2 || back.Nodes[1].Hidden {
		t.Errorf("restore: %d %+v", code, back.Nodes)
	}
}
