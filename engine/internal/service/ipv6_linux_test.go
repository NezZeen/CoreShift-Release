//go:build linux && !android

package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIPv6DisabledIn(t *testing.T) {
	dir := t.TempDir()
	if !ipv6DisabledIn(filepath.Join(dir, "missing")) {
		t.Error("no /proc/sys/net/ipv6 (ipv6.disable=1) should count as disabled")
	}
	conf := filepath.Join(dir, "conf", "default")
	if err := os.MkdirAll(conf, 0o755); err != nil {
		t.Fatal(err)
	}
	for v, want := range map[string]bool{"1\n": true, "0\n": false} {
		if err := os.WriteFile(filepath.Join(conf, "disable_ipv6"), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := ipv6DisabledIn(dir); got != want {
			t.Errorf("disable_ipv6 = %q: disabled %v, want %v", v, got, want)
		}
	}
}
