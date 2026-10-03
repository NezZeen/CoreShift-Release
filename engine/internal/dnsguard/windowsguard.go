package dnsguard

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// This file holds the Windows guard's logic. It talks to the registry through
// an interface so it can be tested on any OS; guard_windows.go wires it to the
// real HKLM and system calls.

const (
	nrptLocalBase   = `SYSTEM\CurrentControlSet\Services\Dnscache\Parameters\DnsPolicyConfig`
	nrptPolicyBase  = `SOFTWARE\Policies\Microsoft\Windows NT\DNSClient\DnsPolicyConfig`
	dnsClientPolicy = `SOFTWARE\Policies\Microsoft\Windows NT\DNSClient`
	dnscacheParams  = `SYSTEM\CurrentControlSet\Services\Dnscache\Parameters`
	policyPrefix    = `SOFTWARE\Policies\`

	// ruleComment tags NRPT rules we create, so they can be found and removed
	// even if the journal is lost. See ruleCommentFor.
	ruleComment = "CoreShift"

	kindNRPTRule = "win.nrpt"
	kindRegDWORD = "win.dword"
)

// registry is the subset of HKLM operations the Windows guard needs. Reading
// or deleting something that does not exist is not an error.
type registry interface {
	CreateKey(path string) error
	DeleteKey(path string) error
	SubKeys(path string) ([]string, error)
	GetString(path, name string) (val string, ok bool, err error)
	SetString(path, name, val string) error
	SetStrings(path, name string, val []string) error
	GetDWORD(path, name string) (val uint32, ok bool, err error)
	SetDWORD(path, name string, val uint32) error
	DeleteValue(path, name string) error
}

type nrptRuleChange struct {
	Key string `json:"key"`
}

type regDWORDChange struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Existed bool   `json:"existed"`
	Value   uint32 `json:"value,omitempty"`
}

type windowsGuard struct {
	reg           registry
	journal       *Journal
	comment       string // the tag of our NRPT rules, from ruleCommentFor
	flushCache    func() error
	refreshPolicy func() error
	newRuleID     func() (string, error)
}

func (g *windowsGuard) Apply(ctx context.Context, cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	if err := g.Revert(ctx); err != nil {
		return fmt.Errorf("dnsguard: revert previous state: %w", err)
	}
	if err := g.apply(cfg); err != nil {
		return errors.Join(err, g.Revert(ctx))
	}
	return g.notify(touchesPolicy(g.journal.Changes()))
}

func (g *windowsGuard) apply(cfg Config) error {
	if cfg.Strict {
		// Smart multi-homed resolution sends each query to the DNS servers of
		// every adapter in parallel, so the physical one would see it too.
		if err := g.setDWORD(dnsClientPolicy, "DisableSmartNameResolution", 1); err != nil {
			return err
		}
		if err := g.setDWORD(dnscacheParams, "DisableParallelAandAAAA", 1); err != nil {
			return err
		}
	}
	bases := []string{nrptLocalBase}
	// Windows ignores local NRPT rules as soon as any Group Policy rule exists.
	policy, err := g.foreignPolicyRules()
	if err != nil {
		return err
	}
	if policy {
		bases = append(bases, nrptPolicyBase)
	}
	for _, base := range bases {
		if err := g.addRule(base, cfg.Servers); err != nil {
			return err
		}
	}
	return nil
}

func (g *windowsGuard) Revert(ctx context.Context) error {
	changes := g.journal.Changes()
	if len(changes) == 0 {
		return nil
	}
	if err := screenJournal(changes); err != nil {
		// Not what this guard writes: someone else's file, made to have
		// SYSTEM change the registry. Nothing of it is undone; the sweep
		// still removes our own rules.
		return errors.Join(fmt.Errorf("dnsguard: journal refused, nothing in it was undone: %w", err), g.journal.Reject())
	}
	err := g.journal.Undo(g.undo)
	return errors.Join(err, g.notify(touchesPolicy(changes)))
}

// guidRE is a rule id as newGUID makes it, {8-4-4-4-12} hex digits.
var guidRE = regexp.MustCompile(`^\{[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\}$`)

// guardDWORDs are the only values setDWORD changes.
var guardDWORDs = [][2]string{
	{dnsClientPolicy, "DisableSmartNameResolution"},
	{dnscacheParams, "DisableParallelAandAAAA"},
}

// screenJournal checks every change against what this guard records
// itself: NRPT rules directly under the two NRPT keys, named by a GUID,
// and the two values it sets. The journal lives in a file, and undoing it
// deletes registry keys and sets values as SYSTEM: a journal with anything
// else is refused as a whole.
func screenJournal(changes []Change) error {
	for i, c := range changes {
		if err := screenChange(c); err != nil {
			return fmt.Errorf("change %d (%s): %w", i+1, c.Kind, err)
		}
	}
	return nil
}

func screenChange(c Change) error {
	dec := json.NewDecoder(bytes.NewReader(c.Data))
	dec.DisallowUnknownFields()
	switch c.Kind {
	case kindNRPTRule:
		var ch nrptRuleChange
		if err := dec.Decode(&ch); err != nil {
			return err
		}
		for _, base := range []string{nrptLocalBase, nrptPolicyBase} {
			if id, ok := strings.CutPrefix(ch.Key, base+`\`); ok && guidRE.MatchString(id) {
				return nil
			}
		}
		return errors.New("not an NRPT rule of this guard")
	case kindRegDWORD:
		var ch regDWORDChange
		if err := dec.Decode(&ch); err != nil {
			return err
		}
		for _, v := range guardDWORDs {
			if ch.Path == v[0] && ch.Name == v[1] {
				return nil
			}
		}
		return errors.New("not a value this guard sets")
	}
	return errors.New("unknown kind")
}

func (g *windowsGuard) Recover(ctx context.Context) error {
	err := g.Revert(ctx)
	n, serr := g.sweep()
	if n > 0 {
		serr = errors.Join(serr, g.notify(true))
	}
	return errors.Join(err, serr)
}

// notify makes the DNS client pick up registry changes.
func (g *windowsGuard) notify(policy bool) error {
	var errs []error
	if policy {
		if err := g.refreshPolicy(); err != nil {
			errs = append(errs, fmt.Errorf("dnsguard: refresh policy: %w", err))
		}
	}
	if err := g.flushCache(); err != nil {
		errs = append(errs, fmt.Errorf("dnsguard: flush DNS cache: %w", err))
	}
	return errors.Join(errs...)
}

// addRule creates an NRPT rule sending every name (namespace ".") to servers,
// with the same layout Add-DnsClientNrptRule produces.
func (g *windowsGuard) addRule(base string, servers []netip.Addr) error {
	id, err := g.newRuleID()
	if err != nil {
		return fmt.Errorf("dnsguard: generate rule id: %w", err)
	}
	key := base + `\` + id
	if err := g.journal.Record(kindNRPTRule, nrptRuleChange{Key: key}); err != nil {
		return err
	}
	steps := []func() error{
		func() error { return g.reg.CreateKey(key) },
		func() error { return g.reg.SetDWORD(key, "Version", 2) },
		func() error { return g.reg.SetStrings(key, "Name", []string{"."}) },
		func() error { return g.reg.SetString(key, "GenericDNSServers", joinAddrs(servers, ";")) },
		func() error { return g.reg.SetDWORD(key, "ConfigOptions", 0x8) }, // use GenericDNSServers
		func() error { return g.reg.SetString(key, "IPSECCARestriction", "") },
		func() error { return g.reg.SetString(key, "DisplayName", "CoreShift tunnel DNS") },
		func() error { return g.reg.SetString(key, "Comment", g.comment) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return fmt.Errorf("dnsguard: write NRPT rule %s: %w", key, err)
		}
	}
	return nil
}

// setDWORD sets a registry value, journaling the previous one.
func (g *windowsGuard) setDWORD(path, name string, val uint32) error {
	prev, ok, err := g.reg.GetDWORD(path, name)
	if err != nil {
		return fmt.Errorf(`dnsguard: read %s\%s: %w`, path, name, err)
	}
	if ok && prev == val {
		return nil
	}
	if err := g.journal.Record(kindRegDWORD, regDWORDChange{Path: path, Name: name, Existed: ok, Value: prev}); err != nil {
		return err
	}
	if err := g.reg.CreateKey(path); err != nil {
		return fmt.Errorf(`dnsguard: create %s: %w`, path, err)
	}
	if err := g.reg.SetDWORD(path, name, val); err != nil {
		return fmt.Errorf(`dnsguard: write %s\%s: %w`, path, name, err)
	}
	return nil
}

func (g *windowsGuard) undo(c Change) error {
	switch c.Kind {
	case kindNRPTRule:
		var ch nrptRuleChange
		if err := json.Unmarshal(c.Data, &ch); err != nil {
			return err
		}
		return g.reg.DeleteKey(ch.Key)
	case kindRegDWORD:
		var ch regDWORDChange
		if err := json.Unmarshal(c.Data, &ch); err != nil {
			return err
		}
		if ch.Existed {
			return g.reg.SetDWORD(ch.Path, ch.Name, ch.Value)
		}
		return g.reg.DeleteValue(ch.Path, ch.Name)
	}
	// screenJournal lets no other kind through.
	return nil
}

// foreignPolicyRules reports whether Group Policy NRPT rules other than ours exist.
func (g *windowsGuard) foreignPolicyRules() (bool, error) {
	keys, err := g.reg.SubKeys(nrptPolicyBase)
	if err != nil {
		return false, fmt.Errorf("dnsguard: list policy NRPT rules: %w", err)
	}
	for _, k := range keys {
		c, _, err := g.reg.GetString(nrptPolicyBase+`\`+k, "Comment")
		if err != nil {
			return false, fmt.Errorf("dnsguard: read policy NRPT rule %s: %w", k, err)
		}
		if !strings.HasPrefix(c, ruleComment) {
			return true, nil
		}
	}
	return false, nil
}

// sweep deletes NRPT rules tagged as ours that the journal no longer knows about.
func (g *windowsGuard) sweep() (int, error) {
	var n int
	var errs []error
	for _, base := range []string{nrptLocalBase, nrptPolicyBase} {
		keys, err := g.reg.SubKeys(base)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, k := range keys {
			key := base + `\` + k
			c, _, err := g.reg.GetString(key, "Comment")
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if c != g.comment {
				continue
			}
			if err := g.reg.DeleteKey(key); err != nil {
				errs = append(errs, err)
				continue
			}
			n++
		}
	}
	return n, errors.Join(errs...)
}

func touchesPolicy(changes []Change) bool {
	for _, c := range changes {
		var p struct{ Key, Path string }
		if json.Unmarshal(c.Data, &p) != nil {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(p.Key+p.Path), strings.ToUpper(policyPrefix)) {
			return true
		}
	}
	return false
}

// newGUID returns a random registry-style GUID such as {0B3C…}.
func newGUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("{%X-%X-%X-%X-%X}", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// ruleCommentFor tags NRPT rules by the data directory whose journal made
// them. The service's own rules keep the plain tag older versions wrote; a
// second daemon with another -data-dir, such as a developer's test run,
// adds its directory, so neither sweeps away the other's live rule.
func ruleCommentFor(journalPath string) string {
	dir := filepath.Clean(filepath.Dir(journalPath))
	if pd := os.Getenv("ProgramData"); pd != "" && strings.EqualFold(dir, filepath.Join(pd, "CoreShift")) {
		return ruleComment
	}
	return ruleComment + " " + dir
}
