package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"coreshift/engine/internal/ruleset"
)

// rulesets refreshes the rule sets CoreShift carries
// (internal/ruleset/data) from SagerNet and runetfreedom (ruleset.URL),
// for packaging\release.ps1. A set added to ruleset.Known since is
// downloaded for the first time.
func rulesets(args []string) error {
	fs := flag.NewFlagSet("rulesets", flag.ExitOnError)
	dir := fs.String("dir", filepath.Join("internal", "ruleset", "data"), "the folder of the built-in copies")
	accept := fs.Bool("accept", false, "take copies that are far from the previous ones (look at why first); damaged ones are never taken")
	fs.Parse(args)
	return refreshRuleSets(*dir, downloadRuleSet, *accept, os.Stdout)
}

// refreshRuleSets downloads each set listed in dir's sets.json and takes
// it if ruleset.Check accepts it next to the copy there (with accept, only
// standing on its own); a known set the manifest does not list yet, if
// Check accepts it on its own. It writes nothing unless every set is good,
// then all of them and sets.json, dated now.
func refreshRuleSets(dir string, fetch func(url string) ([]byte, error), accept bool, out io.Writer) error {
	manifestPath := filepath.Join(dir, ruleset.ManifestName)
	mb, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var m ruleset.Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return fmt.Errorf("%s: %w", manifestPath, err)
	}
	if len(m.Sets) == 0 {
		return fmt.Errorf("%s lists no sets", manifestPath)
	}
	got := map[string][]byte{}
	// The sets of the manifest, and those CoreShift has been given since,
	// which have no copy yet.
	tags := slices.Sorted(maps.Keys(m.Sets))
	for _, tag := range ruleset.Known() {
		if _, ok := m.Sets[tag]; !ok {
			tags = append(tags, tag)
		}
	}
	lists := map[string][]byte{} // each v2ray list downloaded once
	for _, tag := range tags {
		b, err := fetchSet(tag, fetch, lists)
		if err != nil {
			return fmt.Errorf("%s (%s): %w", tag, ruleset.URL(tag), err)
		}
		if _, ok := m.Sets[tag]; !ok {
			// New: nothing to compare it with.
			if err := ruleset.Check(tag, b); err != nil {
				return fmt.Errorf("%s (new): %w", tag, err)
			}
			got[tag] = b
			continue
		}
		prev, err := os.ReadFile(filepath.Join(dir, tag+".srs"))
		if err != nil {
			return err
		}
		if err := ruleset.Check(tag, b, prev); err != nil {
			if !accept {
				return fmt.Errorf("%s: %w (if upstream really changed so, look at the set and run again with -accept)", tag, err)
			}
			if err := ruleset.Check(tag, b); err != nil {
				return fmt.Errorf("%s: %w", tag, err)
			}
			fmt.Fprintf(out, "%s: taken despite: %v\n", tag, err)
		}
		got[tag] = b
	}
	for _, tag := range slices.Sorted(maps.Keys(got)) {
		b := got[tag]
		what := "unchanged"
		if prev, ok := m.Sets[tag]; !ok {
			what = fmt.Sprintf("new, %d bytes", len(b))
			m.Sets[tag] = ruleset.Describe(b)
		} else if f := ruleset.Describe(b); f != prev {
			what = fmt.Sprintf("%d -> %d bytes", m.Sets[tag].Size, f.Size)
			m.Sets[tag] = f
		}
		if err := os.WriteFile(filepath.Join(dir, tag+".srs"), b, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: %s\n", tag, what)
	}
	m.Fetched = time.Now().UTC().Truncate(time.Second)
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, append(body, '\n'), 0o644)
}

// rulesetsPublish makes the sets CoreShift publishes itself, for the
// rulesets branch devices refresh them from (ruleset.Published).
func rulesetsPublish(args []string) error {
	fs := flag.NewFlagSet("rulesets-publish", flag.ExitOnError)
	out := fs.String("out", "", "the folder of the rulesets branch")
	accept := fs.Bool("accept", false, "take copies that are far from the published ones (look at why first); damaged ones are never taken")
	fs.Parse(args)
	if *out == "" {
		return errors.New("rulesets-publish: -out is required")
	}
	return publishRuleSets(*out, downloadRuleSet, ruleset.Baseline, *accept, os.Stdout)
}

// publishRuleSets writes into dir each set made out of a v2ray list
// (ruleset.DatSource), if ruleset.Check accepts it next to the copy
// published there before and the built-in one (with accept, on its own),
// and rulesets.json listing them. Devices hold a copy to the same checks,
// so one they would refuse is not published. Nothing is written unless
// every set is good; an unchanged set keeps its file and the manifest its
// date, so the branch gets a commit only when a set changes.
func publishRuleSets(dir string, fetch func(url string) ([]byte, error), baseline func(tag string) ([]byte, time.Time, bool), accept bool, out io.Writer) error {
	manifestPath := filepath.Join(dir, ruleset.PublishedManifest)
	var prev ruleset.Manifest
	if b, err := os.ReadFile(manifestPath); err == nil {
		if err := json.Unmarshal(b, &prev); err != nil {
			return fmt.Errorf("%s: %w", manifestPath, err)
		}
	}
	m := ruleset.Manifest{Fetched: prev.Fetched, Sets: map[string]ruleset.File{}}
	got := map[string][]byte{}
	lists := map[string][]byte{}
	changed := false
	for _, tag := range ruleset.Known() {
		if _, _, ok := ruleset.DatSource(tag); !ok {
			continue
		}
		b, err := fetchSet(tag, fetch, lists)
		if err != nil {
			return fmt.Errorf("%s (%s): %w", tag, ruleset.URL(tag), err)
		}
		var refs [][]byte
		if p, err := os.ReadFile(filepath.Join(dir, tag+".srs")); err == nil {
			refs = append(refs, p)
		}
		if base, _, ok := baseline(tag); ok {
			refs = append(refs, base)
		}
		if err := ruleset.Check(tag, b, refs...); err != nil {
			if !accept {
				return fmt.Errorf("%s: %w (if upstream really changed so, look at the set and run again with -accept)", tag, err)
			}
			if err := ruleset.Check(tag, b); err != nil {
				return fmt.Errorf("%s: %w", tag, err)
			}
			fmt.Fprintf(out, "%s: taken despite: %v\n", tag, err)
		}
		got[tag] = b
		m.Sets[tag] = ruleset.Describe(b)
		what := "unchanged"
		if p, ok := prev.Sets[tag]; !ok || p != m.Sets[tag] {
			what = fmt.Sprintf("%d bytes", len(b))
			changed = true
		}
		fmt.Fprintf(out, "%s: %s\n", tag, what)
	}
	if len(got) == 0 {
		return errors.New("no set to publish")
	}
	if !changed && len(prev.Sets) == len(m.Sets) {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for tag, b := range got {
		if err := os.WriteFile(filepath.Join(dir, tag+".srs"), b, 0o644); err != nil {
			return err
		}
	}
	m.Fetched = time.Now().UTC().Truncate(time.Second)
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, append(body, '\n'), 0o644)
}

// fetchSet downloads the set tag, or makes it out of the v2ray list it
// comes from (ruleset.DatSource): the list is taken only if its sha256 is
// the one published next to it (.sha256sum).
func fetchSet(tag string, fetch func(url string) ([]byte, error), lists map[string][]byte) ([]byte, error) {
	list, category, ok := ruleset.DatSource(tag)
	if !ok {
		return fetch(ruleset.URL(tag))
	}
	dat, ok := lists[list]
	if !ok {
		var err error
		if dat, err = fetch(list); err != nil {
			return nil, err
		}
		sum, err := fetch(list + ".sha256sum")
		if err != nil {
			return nil, fmt.Errorf("checksum: %w", err)
		}
		fields := strings.Fields(string(sum))
		got := sha256.Sum256(dat)
		if len(fields) == 0 || !strings.EqualFold(fields[0], hex.EncodeToString(got[:])) {
			return nil, errors.New("the list does not match its .sha256sum")
		}
		if err := ruleset.CheckDat(dat); err != nil {
			return nil, err
		}
		lists[list] = dat
	}
	return ruleset.FromDat(dat, ruleset.IsIP(tag), category)
}

func downloadRuleSet(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	// A v2ray list holds every category.
	limit := int64(ruleset.MaxSize)
	if strings.HasSuffix(url, ".dat") {
		limit = ruleset.MaxDatSize
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(resp.Body, limit+1)); err != nil {
		return nil, err
	}
	if int64(buf.Len()) > limit {
		return nil, errors.New("too large")
	}
	return buf.Bytes(), nil
}
