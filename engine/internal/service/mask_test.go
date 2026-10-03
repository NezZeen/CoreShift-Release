package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaskURL(t *testing.T) {
	for in, want := range map[string]string{
		"": "",
		"https://Panel.Example.com/sub/AbCdEf1234567890":                            "https://panel.example.com/…7890",
		"https://panel.example.com:8443/api/v1/client/subscribe?token=SECRETSECRET": "https://panel.example.com:8443/…CRET",
		"https://user:pw@panel.example.com/sub/AbCdEf1234567890":                    "https://panel.example.com/…7890",
		"http://panel.example.com/s/abc":                                            "http://panel.example.com/…",
		"https://panel.example.com":                                                 "https://panel.example.com",
		"https://panel.example.com/":                                                "https://panel.example.com",
		"not a url":                                                                 "…",
	} {
		if got := maskURL(in); got != want {
			t.Errorf("maskURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// Lists and every answer about a subscription carry its link without the
// token; only the QR code's endpoint gives the whole link.
func TestAPIMasksSubscriptionURLs(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(trojanLink))
	}))
	defer panel.Close()
	_, srv := newStoreAPI(t, nil)
	full := panel.URL + "/sub/SECRET-TOKEN-1234"

	var sub subscriptionView
	if code := callJSON(t, srv, "POST", "/v1/subscriptions", map[string]string{"url": full}, &sub); code != http.StatusCreated {
		t.Fatalf("add: %d", code)
	}
	if strings.Contains(sub.URL, "SECRET") || !strings.HasPrefix(sub.URL, strings.ToLower(panel.URL)+"/…") {
		t.Errorf("add answers %q", sub.URL)
	}
	// httptest serves plain HTTP: the link is marked.
	if !sub.Insecure {
		t.Error("a plain http link is not marked insecure")
	}
	var list []subscriptionView
	callJSON(t, srv, "GET", "/v1/subscriptions", nil, &list)
	var one subscriptionView
	callJSON(t, srv, "GET", "/v1/subscriptions/"+sub.ID, nil, &one)
	for _, v := range append(list, one) {
		if strings.Contains(v.URL, "SECRET") || v.URL == "" {
			t.Errorf("answers %q", v.URL)
		}
	}
	var got struct{ URL string }
	if code := callJSON(t, srv, "GET", "/v1/subscriptions/"+sub.ID+"/url", nil, &got); code != http.StatusOK || got.URL != full {
		t.Errorf("url endpoint: %d %q", code, got.URL)
	}
	if code := callJSON(t, srv, "GET", "/v1/subscriptions/nope/url", nil, nil); code != http.StatusNotFound {
		t.Errorf("unknown id: %d", code)
	}
}
