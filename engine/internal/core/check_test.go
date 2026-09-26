package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCoresAcceptConfigs validates every fixture a core claims to support
// with the real binary. Point the variables at the executables to run it:
//
//	XRAY_BIN, SINGBOX_BIN, MIHOMO_BIN
//
// sing-box checks strictly. Xray and mihomo reject unknown protocols and bad
// keys but ignore unknown fields (mihomo even unknown "network" values), so
// for them this proves the structure, not every option.
func TestCoresAcceptConfigs(t *testing.T) {
	bins := map[Kind]string{
		Xray:    os.Getenv("XRAY_BIN"),
		SingBox: os.Getenv("SINGBOX_BIN"),
		Mihomo:  os.Getenv("MIHOMO_BIN"),
	}
	fx := fixtures(t)
	names := make([]string, 0, len(fx))
	for name := range fx {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, a := range Adapters() {
		bin := bins[a.Kind()]
		t.Run(string(a.Kind()), func(t *testing.T) {
			if bin == "" {
				t.Skipf("binary not set")
			}
			for _, name := range names {
				n := fx[name]
				if a.Supports(&n) != nil {
					continue
				}
				t.Run(name, func(t *testing.T) {
					dir := t.TempDir()
					cfg, err := a.Render(&n, Options{})
					if err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(dir, a.ConfigName())
					if err := os.WriteFile(path, cfg, 0o600); err != nil {
						t.Fatal(err)
					}
					out, err := exec.Command(bin, a.CheckArgs(path, dir)...).CombinedOutput()
					if err != nil {
						t.Fatalf("check failed: %v\n%s\nconfig:\n%s", err, out, cfg)
					}
					if strings.Contains(strings.ToLower(string(out)), "deprecat") {
						t.Errorf("deprecation warning:\n%s", out)
					}
				})
			}
		})
	}
}
