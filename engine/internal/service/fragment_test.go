package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coreshift/engine/internal/store"
)

func TestFragmentSettingReachesTheCore(t *testing.T) {
	set := store.Defaults()
	set.Cores.Fragment = true
	if o := OptionsFromSettings(set); !o.Fragment || !o.policy().Fragment {
		t.Fatalf("options = %+v", o)
	}

	h := newHarness(t, func(c *Config) { c.Fragment = true })
	if err := h.connect(t, trojanLink); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(h.svc.cfg.DataDir, "work", "xray", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"dialerProxy": "fragment"`) {
		t.Errorf("the core's config does not fragment:\n%s", b)
	}
}
