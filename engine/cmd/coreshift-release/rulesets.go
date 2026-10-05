package main

import (
	"bytes"
	"context"
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
	"time"

	"coreshift/engine/internal/ruleset"
)

// rulesets refreshes the rule sets CoreShift carries
// (internal/ruleset/data) from SagerNet, for packaging\release.ps1.
func rulesets(args []string) error {
	fs := flag.NewFlagSet("rulesets", flag.ExitOnError)
	dir := fs.String("dir", filepath.Join("internal", "ruleset", "data"), "the folder of the built-in copies")
	accept := fs.Bool("accept", false, "take copies that are far from the previous ones (look at why first); damaged ones are never taken")
	fs.Parse(args)
	return refreshRuleSets(*dir, downloadRuleSet, *accept, os.Stdout)
}

// refreshRuleSets downloads each set listed in dir's sets.json and takes
// it if ruleset.Check accepts it next to the copy there (with accept, only
// standing on its own). It writes nothing unless every set is good, then
// all of them and sets.json, dated now.
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
	for _, tag := range slices.Sorted(maps.Keys(m.Sets)) {
		b, err := fetch(ruleset.URL(tag))
		if err != nil {
			return fmt.Errorf("%s: %w", tag, err)
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
		if f := ruleset.Describe(b); f != m.Sets[tag] {
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

func downloadRuleSet(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
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
	if _, err := io.Copy(&buf, io.LimitReader(resp.Body, ruleset.MaxSize+1)); err != nil {
		return nil, err
	}
	if buf.Len() > ruleset.MaxSize {
		return nil, errors.New("rule set too large")
	}
	return buf.Bytes(), nil
}
