package dnsguard

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeRegistry is an in-memory HKLM: key path -> value name -> value.
type fakeRegistry struct {
	keys   map[string]map[string]any
	failOn string // value name whose write fails
}

func newFakeRegistry() *fakeRegistry { return &fakeRegistry{keys: map[string]map[string]any{}} }

func (r *fakeRegistry) CreateKey(path string) error {
	if r.keys[path] == nil {
		r.keys[path] = map[string]any{}
	}
	return nil
}

func (r *fakeRegistry) DeleteKey(path string) error { delete(r.keys, path); return nil }

func (r *fakeRegistry) SubKeys(path string) ([]string, error) {
	var out []string
	for k := range r.keys {
		if rest, ok := strings.CutPrefix(k, path+`\`); ok && !strings.Contains(rest, `\`) {
			out = append(out, rest)
		}
	}
	slices.Sort(out)
	return out, nil
}

func (r *fakeRegistry) get(path, name string) (any, bool) {
	v, ok := r.keys[path][name]
	return v, ok
}

func (r *fakeRegistry) set(path, name string, v any) error {
	if name == r.failOn {
		return fmt.Errorf("injected failure writing %s", name)
	}
	if r.keys[path] == nil {
		return errors.New("key does not exist: " + path)
	}
	r.keys[path][name] = v
	return nil
}

func (r *fakeRegistry) GetString(path, name string) (string, bool, error) {
	v, ok := r.get(path, name)
	s, _ := v.(string)
	return s, ok, nil
}

func (r *fakeRegistry) SetString(path, name, val string) error { return r.set(path, name, val) }

func (r *fakeRegistry) SetStrings(path, name string, val []string) error {
	return r.set(path, name, val)
}

func (r *fakeRegistry) GetDWORD(path, name string) (uint32, bool, error) {
	v, ok := r.get(path, name)
	d, _ := v.(uint32)
	return d, ok, nil
}

func (r *fakeRegistry) SetDWORD(path, name string, val uint32) error { return r.set(path, name, val) }

func (r *fakeRegistry) DeleteValue(path, name string) error {
	delete(r.keys[path], name)
	return nil
}

type winHarness struct {
	g                  *windowsGuard
	reg                *fakeRegistry
	flushes, refreshes int
}

func newWinHarness(t *testing.T, reg *fakeRegistry, journalPath string) *winHarness {
	t.Helper()
	j, err := OpenJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	h := &winHarness{reg: reg}
	h.g = &windowsGuard{
		reg:           reg,
		journal:       j,
		comment:       ruleComment,
		flushCache:    func() error { h.flushes++; return nil },
		refreshPolicy: func() error { h.refreshes++; return nil },
		newRuleID:     newGUID,
	}
	return h
}

var tunDNS = []netip.Addr{netip.MustParseAddr("172.19.0.2")}

func TestWindowsApplyAndRevert(t *testing.T) {
	reg := newFakeRegistry()
	reg.CreateKey(dnscacheParams)
	reg.SetDWORD(dnscacheParams, "DisableParallelAandAAAA", 0)
	h := newWinHarness(t, reg, filepath.Join(t.TempDir(), "j.json"))
	ctx := context.Background()

	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS, Strict: true}); err != nil {
		t.Fatal(err)
	}
	rules, _ := reg.SubKeys(nrptLocalBase)
	if len(rules) != 1 {
		t.Fatalf("got %d local NRPT rules, want 1", len(rules))
	}
	rule := nrptLocalBase + `\` + rules[0]
	if v, _ := reg.get(rule, "Name"); !slices.Equal(v.([]string), []string{"."}) {
		t.Errorf("Name = %v, want [.]", v)
	}
	if v, _, _ := reg.GetString(rule, "GenericDNSServers"); v != "172.19.0.2" {
		t.Errorf("GenericDNSServers = %q", v)
	}
	if v, _, _ := reg.GetDWORD(rule, "ConfigOptions"); v != 8 {
		t.Errorf("ConfigOptions = %d, want 8", v)
	}
	if v, _, _ := reg.GetDWORD(dnsClientPolicy, "DisableSmartNameResolution"); v != 1 {
		t.Errorf("DisableSmartNameResolution = %d, want 1", v)
	}
	if v, _, _ := reg.GetDWORD(dnscacheParams, "DisableParallelAandAAAA"); v != 1 {
		t.Errorf("DisableParallelAandAAAA = %d, want 1", v)
	}
	if h.flushes == 0 || h.refreshes == 0 {
		t.Errorf("flushes=%d refreshes=%d, both should be called", h.flushes, h.refreshes)
	}

	if err := h.g.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if rules, _ := reg.SubKeys(nrptLocalBase); len(rules) != 0 {
		t.Errorf("NRPT rules left after revert: %v", rules)
	}
	if v, ok, _ := reg.GetDWORD(dnscacheParams, "DisableParallelAandAAAA"); !ok || v != 0 {
		t.Errorf("DisableParallelAandAAAA = %d (exists=%v), want restored 0", v, ok)
	}
	if _, ok, _ := reg.GetDWORD(dnsClientPolicy, "DisableSmartNameResolution"); ok {
		t.Error("DisableSmartNameResolution should be deleted, it did not exist before")
	}
	if h.g.journal.Len() != 0 {
		t.Errorf("journal has %d changes after revert", h.g.journal.Len())
	}
}

func TestWindowsWritesPolicyRuleWhenGroupPolicyRulesExist(t *testing.T) {
	reg := newFakeRegistry()
	foreign := nrptPolicyBase + `\{CORP}`
	reg.CreateKey(foreign)
	reg.SetString(foreign, "Comment", "corp.example split DNS")
	h := newWinHarness(t, reg, filepath.Join(t.TempDir(), "j.json"))
	ctx := context.Background()

	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	if rules, _ := reg.SubKeys(nrptLocalBase); len(rules) != 1 {
		t.Errorf("local rules = %v, want 1", rules)
	}
	if rules, _ := reg.SubKeys(nrptPolicyBase); len(rules) != 2 {
		t.Errorf("policy rules = %v, want foreign + ours", rules)
	}
	if h.refreshes == 0 {
		t.Error("policy refresh should run after writing a policy rule")
	}

	if err := h.g.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if rules, _ := reg.SubKeys(nrptPolicyBase); !slices.Equal(rules, []string{"{CORP}"}) {
		t.Errorf("policy rules after revert = %v, want only the foreign one", rules)
	}
}

func TestWindowsRecoverAfterCrash(t *testing.T) {
	reg := newFakeRegistry()
	journal := filepath.Join(t.TempDir(), "j.json")
	ctx := context.Background()

	crashed := newWinHarness(t, reg, journal)
	if err := crashed.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS, Strict: true}); err != nil {
		t.Fatal(err)
	}

	restarted := newWinHarness(t, reg, journal)
	if err := restarted.g.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if rules, _ := reg.SubKeys(nrptLocalBase); len(rules) != 0 {
		t.Errorf("rules left after recover: %v", rules)
	}
	if _, ok, _ := reg.GetDWORD(dnsClientPolicy, "DisableSmartNameResolution"); ok {
		t.Error("DisableSmartNameResolution left after recover")
	}
}

func TestWindowsRecoverSweepsOrphanRulesOnly(t *testing.T) {
	reg := newFakeRegistry()
	ours := nrptLocalBase + `\{OURS}`
	theirs := nrptLocalBase + `\{THEIRS}`
	reg.CreateKey(ours)
	reg.SetString(ours, "Comment", ruleComment)
	reg.CreateKey(theirs)
	reg.SetString(theirs, "Comment", "something else")
	h := newWinHarness(t, reg, filepath.Join(t.TempDir(), "j.json")) // journal lost

	if err := h.g.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rules, _ := reg.SubKeys(nrptLocalBase); !slices.Equal(rules, []string{"{THEIRS}"}) {
		t.Errorf("rules after sweep = %v, want only {THEIRS}", rules)
	}
}

func TestWindowsRecoverLeavesAnotherDaemonsRules(t *testing.T) {
	reg := newFakeRegistry()
	service := nrptLocalBase + `\{SERVICE}`
	reg.CreateKey(service)
	reg.SetString(service, "Comment", ruleComment)
	h := newWinHarness(t, reg, filepath.Join(t.TempDir(), "j.json"))
	h.g.comment = ruleCommentFor(filepath.Join(t.TempDir(), "dnsguard.json"))

	// A test daemon with its own data directory starts while the service
	// is connected: the service's rule is not an orphan of this daemon.
	if err := h.g.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rules, _ := reg.SubKeys(nrptLocalBase); !slices.Equal(rules, []string{"{SERVICE}"}) {
		t.Errorf("rules after sweep = %v, want the service's {SERVICE} kept", rules)
	}
	if got := ruleCommentFor(filepath.Join(os.Getenv("ProgramData"), "CoreShift", "dnsguard.json")); os.Getenv("ProgramData") != "" && got != ruleComment {
		t.Errorf("the service's tag = %q, want %q as older versions wrote", got, ruleComment)
	}
}

func TestWindowsApplyFailureLeavesNothingBehind(t *testing.T) {
	reg := newFakeRegistry()
	reg.failOn = "GenericDNSServers"
	h := newWinHarness(t, reg, filepath.Join(t.TempDir(), "j.json"))

	err := h.g.Apply(context.Background(), Config{Interface: "coreshift", Servers: tunDNS, Strict: true})
	if err == nil {
		t.Fatal("Apply should fail")
	}
	if rules, _ := reg.SubKeys(nrptLocalBase); len(rules) != 0 {
		t.Errorf("partial rule left behind: %v", rules)
	}
	if _, ok, _ := reg.GetDWORD(dnsClientPolicy, "DisableSmartNameResolution"); ok {
		t.Error("DisableSmartNameResolution left behind")
	}
	if h.g.journal.Len() != 0 {
		t.Errorf("journal has %d changes", h.g.journal.Len())
	}
}

func TestWindowsReapplyReplacesRule(t *testing.T) {
	reg := newFakeRegistry()
	h := newWinHarness(t, reg, filepath.Join(t.TempDir(), "j.json"))
	ctx := context.Background()
	other := []netip.Addr{netip.MustParseAddr("172.19.0.2"), netip.MustParseAddr("fdfe:dcba:9876::2")}

	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: tunDNS}); err != nil {
		t.Fatal(err)
	}
	if err := h.g.Apply(ctx, Config{Interface: "coreshift", Servers: other}); err != nil {
		t.Fatal(err)
	}
	rules, _ := reg.SubKeys(nrptLocalBase)
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	if v, _, _ := reg.GetString(nrptLocalBase+`\`+rules[0], "GenericDNSServers"); v != "172.19.0.2;fdfe:dcba:9876::2" {
		t.Errorf("GenericDNSServers = %q", v)
	}
}

func TestNewGUIDFormat(t *testing.T) {
	id, err := newGUID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 38 || id[0] != '{' || id[37] != '}' || strings.Count(id, "-") != 4 {
		t.Errorf("malformed GUID %q", id)
	}
}
