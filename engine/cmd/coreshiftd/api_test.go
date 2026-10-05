package main

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

// The API gets a port the system picks, and api.json names it: a file
// left by a crashed run is replaced, never trusted.
func TestOpenAPIPicksAFreePort(t *testing.T) {
	addr, err := parseAddrPort(defaultAPIAddr)
	if err != nil || !addr.Addr().IsLoopback() || addr.Port() != 0 {
		t.Fatalf("default API address %q", defaultAPIAddr)
	}
	path := filepath.Join(t.TempDir(), "api.json")
	if err := os.WriteFile(path, []byte(`{"address":"http://127.0.0.1:17900","token":"stale"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	read := func() apiInfo {
		var info apiInfo
		b, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(b, &info) != nil {
			t.Fatalf("api.json: %s %v", b, err)
		}
		return info
	}

	ln1, bound1, token1, err := openAPI(addr, path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln1.Close()
	if bound1.Port() == 0 || bound1.Addr() != netip.MustParseAddr("127.0.0.1") {
		t.Errorf("bound to %v", bound1)
	}
	if info := read(); info.Address != "http://"+bound1.String() || info.Token != token1 || len(token1) != 64 {
		t.Errorf("api.json %+v, want %v", info, bound1)
	}

	ln2, bound2, token2, err := openAPI(addr, path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()
	if bound2 == bound1 || token2 == token1 {
		t.Error("a second start got the same port or token")
	}
	if info := read(); info.Address != "http://"+bound2.String() {
		t.Errorf("api.json names %s, want %v", info.Address, bound2)
	}
}
