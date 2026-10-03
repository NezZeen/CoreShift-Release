package tunlayer

import (
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSingBoxAcceptsConfig validates generated configs with a real sing-box.
// Set SINGBOX_BIN to the executable to run it, e.g.
//
//	SINGBOX_BIN=testdata/bin/sing-box.exe go test ./internal/tunlayer -run SingBox
func TestSingBoxAcceptsConfig(t *testing.T) {
	bin := os.Getenv("SINGBOX_BIN")
	if bin == "" {
		t.Skip("SINGBOX_BIN not set")
	}
	geosite := compileRuleSet(t, bin, `{"domain_suffix":["example.ru"]}`)
	geoip := compileRuleSet(t, bin, `{"ip_cidr":["203.0.113.0/24"]}`)
	variants := map[string]func(*Options){
		"defaults": func(*Options) {},
		"upstream credentials": func(o *Options) {
			o.UpstreamUser, o.UpstreamPass = "0123456789abcdef", "0123456789abcdef0123456789abcdef"
		},
		"no fakeip": func(o *Options) {
			o.DNS.FakeIP = false
		},
		"leak protection": func(o *Options) {
			o.DNS.BlockBrowserDoH = true
			o.DNS.BlockDoT = true
		},
		"bypass": func(o *Options) {
			o.BypassProcesses = []string{`C:\Program Files\CoreShift\cores\xray.exe`}
			o.BypassAddresses = []netip.Prefix{netip.MustParsePrefix("203.0.113.10/32")}
		},
		"direct apps": func(o *Options) {
			o.DirectApps = []string{"qbittorrent.exe", "Steam.exe"}
		},
		"direct apps on an IPv4-only host": func(o *Options) {
			o.DirectApps = []string{"qbittorrent.exe"}
			o.Address6 = DefaultAddress6
			o.DNS.DirectIPv4Only = true
		},
		"own lists": func(o *Options) {
			o.DNS.DirectSuffixes = []string{"ru"}
			o.DNS.ProxySuffixes = []string{"blocked.ru"}
			o.DNS.BlockSuffixes = []string{"ads.example"}
			o.DirectIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
			o.ProxyIPs = []netip.Prefix{netip.MustParsePrefix("91.108.4.0/22"), netip.MustParsePrefix("2001:67c:4e8::/48")}
			o.ProxyApps = []string{"Telegram.exe"}
		},
		"selective": func(o *Options) {
			o.Selective = true
			o.Address6 = DefaultAddress6
			o.DNS.DirectIPv4Only = true
			o.DNS.ProxySuffixes = []string{"youtube.com"}
			o.ProxyIPs = []netip.Prefix{netip.MustParsePrefix("91.108.4.0/22")}
			o.ProxyApps = []string{"Telegram.exe"}
		},
		"selective without fakeip": func(o *Options) {
			o.Selective = true
			o.DNS.FakeIP = false
			o.DNS.ProxySuffixes = []string{"youtube.com"}
		},
		"direct domains": func(o *Options) {
			o.DNS.DirectSuffixes = []string{"ru", "lan"}
			o.DNS.DirectRuleSets = []RuleSet{{Tag: "geosite-ru", URL: "https://example.org/geosite-ru.srs"}}
		},
		"local geo rule sets": func(o *Options) {
			o.DNS.DirectSuffixes = []string{"ru"}
			o.DNS.DirectRuleSets = []RuleSet{{Tag: "geosite-ru", Path: geosite}}
			o.DNS.DirectIPRuleSets = []RuleSet{{Tag: "geoip-ru", Path: geoip}}
			o.DNS.ProxyRuleSets = []RuleSet{{Tag: "geosite-media-ru-blocked", Path: geosite}, {Tag: "geosite-google", Path: geosite}}
			o.DNS.ProxySuffixes = []string{"google.com", "googlevideo.com"}
			o.DNS.DirectFirst = []string{"maps.google.com"}
		},
		"proxy rule set in selective mode": func(o *Options) {
			o.Selective = true
			o.DNS.ProxyRuleSets = []RuleSet{{Tag: "geosite-media-ru-blocked", Path: geosite}}
		},
		"ipv6": func(o *Options) {
			o.Address6 = netip.MustParsePrefix("fdfe:dcba:9876::1/126")
		},
		"ipv6 with presets on an IPv4-only host": func(o *Options) {
			o.Address6 = DefaultAddress6
			o.DNS.DirectIPv4Only = true
			o.DNS.DirectSuffixes = []string{"ru"}
			o.DNS.DirectRuleSets = []RuleSet{{Tag: "geosite-ru", Path: geosite}}
			o.DNS.DirectIPRuleSets = []RuleSet{{Tag: "geoip-ru", Path: geoip}}
		},
		"remote hostname": func(o *Options) {
			o.DNS.Remote = "tls://dns.example:853"
		},
		"local network kept out": func(o *Options) {
			o.Address6 = DefaultAddress6
			o.ExcludeLAN = true
			o.LANResolvers = []netip.Addr{netip.MustParseAddr("192.168.1.1"), netip.MustParseAddr("172.25.192.1")}
			o.BypassAddresses = []netip.Prefix{netip.MustParsePrefix("203.0.113.10/32")}
		},
		"cache file": func(o *Options) {
			o.CacheFile = filepath.Join(t.TempDir(), "cache.db")
		},
	}
	for name, mutate := range variants {
		t.Run(name, func(t *testing.T) {
			o := baseOptions()
			mutate(&o)
			b, err := Build(o)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(bin, "check", "-c", path).CombinedOutput()
			if err != nil || len(out) > 0 {
				t.Errorf("sing-box check: %v\n%s\nconfig:\n%s", err, out, b)
			}
		})
	}
}

// compileRuleSet builds a binary rule set with one rule, using sing-box.
func compileRuleSet(t *testing.T, bin, rule string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(src, []byte(`{"version":2,"rules":[`+rule+`]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "rules.srs")
	if b, err := exec.Command(bin, "rule-set", "compile", "-o", out, src).CombinedOutput(); err != nil {
		t.Fatalf("compile rule set: %v\n%s", err, b)
	}
	return out
}
