package sysproxy

import (
	"context"
	"strings"
	"testing"
	"unsafe"
)

// The structs WinINet takes, laid out as in wininet.h.
func TestPerConnLayout(t *testing.T) {
	ptr := unsafe.Sizeof(uintptr(0))
	if got, want := unsafe.Sizeof(perConnOption{}), 4+4*(ptr/8)+8; got != want {
		t.Errorf("INTERNET_PER_CONN_OPTIONW is %d bytes, want %d", got, want)
	}
	if ptr == 8 {
		if got := unsafe.Sizeof(perConnList{}); got != 32 {
			t.Errorf("INTERNET_PER_CONN_OPTION_LISTW is %d bytes, want 32", got)
		}
	}
}

// What the proxy looks like, without setting it: the tests never change
// the computer's proxy.
func TestWinINetSettings(t *testing.T) {
	w := WinINet{}
	ours := w.Proxy(port)
	if ours["server"] != "127.0.0.1:17890" || ours["flags"] != "3" || ours["autoconfig"] != "" {
		t.Errorf("proxy %v", ours)
	}
	for _, want := range []string{"<local>", "localhost", "127.*", "10.*", "172.16.*", "172.31.*", "192.168.*"} {
		if !strings.Contains(";"+ours["bypass"]+";", ";"+want+";") {
			t.Errorf("bypass lacks %s: %s", want, ours["bypass"])
		}
	}
	if !w.Owns(ours, ours) {
		t.Error("ours not owned")
	}
	// Windows turning automatic detection on by itself leaves it ours.
	auto := Settings{"flags": "11", "server": ours["server"]}
	if !w.Owns(auto, ours) {
		t.Error("ours with automatic detection not owned")
	}
	for _, cur := range []Settings{
		w.Direct(),
		{"flags": "1", "server": ours["server"]},
		{"flags": "3", "server": "proxy.corp:3128"},
	} {
		if w.Owns(cur, ours) {
			t.Errorf("%v taken for ours", cur)
		}
	}
}

// Reading changes nothing, so it runs against the real setting.
func TestWinINetRead(t *testing.T) {
	s, err := WinINet{}.Read(context.Background())
	if err != nil {
		t.Skipf("WinINet: %v", err)
	}
	for _, k := range []string{"flags", "server", "bypass", "autoconfig"} {
		if _, ok := s[k]; !ok {
			t.Errorf("no %s in %v", k, s)
		}
	}
}
