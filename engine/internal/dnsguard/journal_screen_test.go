package dnsguard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A journal the guard did not write, planted where the service, as SYSTEM,
// reads it: undoing it would delete or set any HKLM key.
func TestWindowsRecoverRefusesAForgedJournal(t *testing.T) {
	victim := `SOFTWARE\Microsoft\Windows Defender\Real-Time Protection`
	for name, journal := range map[string]string{
		"foreign key": `[{"kind":"win.nrpt","data":{"key":"SYSTEM\\CurrentControlSet\\Services\\WinDefend"}}]`,
		"under the base, deeper": `[{"kind":"win.nrpt","data":{"key":"` +
			`SYSTEM\\CurrentControlSet\\Services\\Dnscache\\Parameters\\DnsPolicyConfig\\{0B3C1234-1234-4123-8123-123456789ABC}\\x"}}]`,
		"base itself": `[{"kind":"win.nrpt","data":{"key":"SYSTEM\\CurrentControlSet\\Services\\Dnscache\\Parameters\\DnsPolicyConfig"}}]`,
		"foreign value": `[{"kind":"win.dword","data":{"path":"` + strings.ReplaceAll(victim, `\`, `\\`) +
			`","name":"DisableRealtimeMonitoring","existed":true,"value":1}}]`,
		"our key, other value": `[{"kind":"win.dword","data":{"path":"SYSTEM\\CurrentControlSet\\Services\\Dnscache\\Parameters","name":"Start","existed":true,"value":4}}]`,
		"unknown kind":         `[{"kind":"win.anything","data":{}}]`,
		"extra field":          `[{"kind":"win.nrpt","data":{"key":"SYSTEM\\CurrentControlSet\\Services\\Dnscache\\Parameters\\DnsPolicyConfig\\{0B3C1234-1234-4123-8123-123456789ABC}","also":"x"}}]`,
	} {
		t.Run(name, func(t *testing.T) {
			reg := newFakeRegistry()
			reg.CreateKey(`SYSTEM\CurrentControlSet\Services\WinDefend`)
			reg.CreateKey(victim)
			reg.SetDWORD(victim, "DisableRealtimeMonitoring", 0)
			reg.CreateKey(dnscacheParams)
			reg.SetDWORD(dnscacheParams, "Start", 2)
			path := filepath.Join(t.TempDir(), "dnsguard.json")
			if err := os.WriteFile(path, []byte(journal), 0o600); err != nil {
				t.Fatal(err)
			}
			h := newWinHarness(t, reg, path)
			err := h.g.Recover(context.Background())
			if err == nil || !strings.Contains(err.Error(), "journal refused") {
				t.Errorf("Recover = %v, want the journal refused", err)
			}
			if _, ok := reg.keys[`SYSTEM\CurrentControlSet\Services\WinDefend`]; !ok {
				t.Error("a foreign key was deleted")
			}
			if v, _, _ := reg.GetDWORD(victim, "DisableRealtimeMonitoring"); v != 0 {
				t.Error("a foreign value was changed")
			}
			if v, _, _ := reg.GetDWORD(dnscacheParams, "Start"); v != 2 {
				t.Error("another value of the guard's key was changed")
			}
			if _, err := os.Stat(path); err == nil {
				t.Error("the refused journal stays to be read again")
			}
			if _, err := os.Stat(path + ".rejected"); err != nil {
				t.Errorf("the refused journal is not kept aside: %v", err)
			}
			// The next start has nothing left to refuse.
			if err := h.g.Recover(context.Background()); err != nil {
				t.Errorf("second Recover: %v", err)
			}
		})
	}
}

// What the guard writes itself, as an earlier version left it after a
// crash, is still undone.
func TestWindowsRecoverAcceptsItsOwnJournal(t *testing.T) {
	reg := newFakeRegistry()
	path := filepath.Join(t.TempDir(), "dnsguard.json")
	h := newWinHarness(t, reg, path)
	reg.CreateKey(dnscacheParams)
	reg.SetDWORD(dnscacheParams, "DisableParallelAandAAAA", 0)
	if err := h.g.Apply(context.Background(), Config{Interface: "coreshift", Servers: tunDNS, Strict: true}); err != nil {
		t.Fatal(err)
	}
	// A new process finds the journal.
	h2 := newWinHarness(t, reg, path)
	if h2.g.journal.Len() != 3 {
		t.Fatalf("journal has %d changes", h2.g.journal.Len())
	}
	if err := h2.g.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rules, _ := reg.SubKeys(nrptLocalBase); len(rules) != 0 {
		t.Errorf("rules left: %v", rules)
	}
	if v, _, _ := reg.GetDWORD(dnscacheParams, "DisableParallelAandAAAA"); v != 0 {
		t.Errorf("value not restored: %d", v)
	}
	if _, ok, _ := reg.GetDWORD(dnsClientPolicy, "DisableSmartNameResolution"); ok {
		t.Error("value that did not exist is still set")
	}
}

func TestScreenAcceptsPolicyRules(t *testing.T) {
	id, _ := newGUID()
	for _, key := range []string{nrptLocalBase + `\` + id, nrptPolicyBase + `\` + id} {
		raw := `{"key":"` + strings.ReplaceAll(key, `\`, `\\`) + `"}`
		if err := screenChange(Change{Kind: kindNRPTRule, Data: []byte(raw)}); err != nil {
			t.Errorf("%s: %v", key, err)
		}
	}
}
