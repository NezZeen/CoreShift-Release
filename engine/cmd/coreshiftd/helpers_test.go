package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"coreshift/engine/internal/core"
)

func TestSplitList(t *testing.T) {
	got := splitList(" xray, sing-box ,, mihomo ,")
	if want := []string{"xray", "sing-box", "mihomo"}; !slices.Equal(got, want) {
		t.Errorf("splitList = %q, want %q", got, want)
	}
	if got := splitList(""); len(got) != 0 {
		t.Errorf("splitList of nothing = %q", got)
	}
}

func TestInstalledKeepsThePriorityOrder(t *testing.T) {
	bins := map[core.Kind]string{core.Mihomo: "m", core.Xray: "x"}
	got := installed([]core.Kind{core.SingBox, core.Mihomo, core.Xray}, bins)
	if want := []core.Kind{core.Mihomo, core.Xray}; !slices.Equal(got, want) {
		t.Errorf("installed = %v, want %v", got, want)
	}
}

func TestIsURL(t *testing.T) {
	for s, want := range map[string]bool{
		"https://panel.example/sub/abc":          true,
		"http://panel.example/sub/abc":           true,
		"vless://id@host:443":                    false,
		"https://a.example/x\nhttps://b.example": false, // a list, not one link
		"panel.example/sub":                      false,
	} {
		if isURL(s) != want {
			t.Errorf("isURL(%q) = %v, want %v", s, !want, want)
		}
	}
}

// A core update leaves the replaced version as *.old (and a rejected one as
// *.failed) beside the core: those must not be taken for the core.
func TestFindCoresSkipsLeftovers(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"xray.exe", "xray.exe.old", "sing-box.exe", "sing-box.exe.failed",
		"mihomo-windows-amd64.exe", ".mihomo.tmp", "geoip.dat",
	} {
		sub := dir
		if name == "sing-box.exe" {
			sub = filepath.Join(dir, "sing-box-1.14.2") // releases unpack into folders
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(sub, name), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bins, err := findCores(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[core.Kind]string{
		core.Xray:    filepath.Join(dir, "xray.exe"),
		core.SingBox: filepath.Join(dir, "sing-box-1.14.2", "sing-box.exe"),
		core.Mihomo:  filepath.Join(dir, "mihomo-windows-amd64.exe"),
	}
	for k, path := range want {
		if bins[k] != path {
			t.Errorf("%s = %q, want %q", k, bins[k], path)
		}
	}
	if len(bins) != len(want) {
		t.Errorf("found %v, want only %v", bins, want)
	}
}

func TestFindCoresWithoutCores(t *testing.T) {
	if _, err := findCores(t.TempDir()); err == nil {
		t.Error("an empty folder is not a folder of cores")
	}
}
