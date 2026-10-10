package service

import (
	"strings"
	"testing"
	"time"

	"coreshift/engine/internal/msg"
)

func TestNetworkKind(t *testing.T) {
	for name, want := range map[string]string{
		"wlan0":             "Wi-Fi",
		"wlp3s0":            "Wi-Fi",
		"Беспроводная сеть": "Wi-Fi",
		"Wi-Fi 2":           "Wi-Fi",
		"rmnet_data2":       "мобильная сеть",
		"ccmni1":            "мобильная сеть",
		"Сотовая связь":     "мобильная сеть",
		"eth0":              "кабель",
		"enp4s0":            "кабель",
		"Ethernet 2":        "кабель",
		"usb0":              "USB-модем",
		"tailscale0":        "",
	} {
		if got := networkKind(name).String(); got != want {
			t.Errorf("networkKind(%q) = %q, want %q", name, got, want)
		}
	}
	if got := networkKind("wlan0").Code; got != "net.kind.wifi" {
		t.Errorf("code = %q", got)
	}
	if got := networkLabel("tailscale0"); got.Code != msg.CodeRaw || got.String() != "tailscale0" {
		t.Errorf("label = %+v", got)
	}
}

// A connection tells the journal which network it runs over, and when the
// device moves to another one; the same network again says nothing.
func TestNetworkInfoAndChange(t *testing.T) {
	h := newHarness(t, nil)
	h.network.Store("wlan0")
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	if e := waitEvent(t, h.events, "netinfo"); e.Reason != "" || !strings.HasPrefix(e.Line, "сеть: Wi-Fi (wlan0)") || e.Code != "net.info" {
		t.Fatalf("info = %+v", e)
	}
	h.network.Store("rmnet_data2")
	e := waitEvent(t, h.events, "netinfo")
	if e.Reason != "changed" || !strings.HasPrefix(e.Line, "сеть сменилась: Wi-Fi (wlan0) → мобильная сеть (rmnet_data2)") || e.Code != "net.changed" {
		t.Fatalf("change = %+v", e)
	}
	if was, _ := e.Args["was"].(msg.Msg); was.Code != "net.label" || was.Args["name"] != "wlan0" {
		t.Errorf("was = %+v", e.Args["was"])
	}
	for _, e := range eventsFor(h, 300*time.Millisecond) {
		if e.Kind == "netinfo" {
			t.Errorf("told again: %+v", e)
		}
	}
}
