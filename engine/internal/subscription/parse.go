// Package subscription downloads subscriptions and parses them into nodes.
//
// Supported formats, detected automatically: base64-encoded link lists, plain
// link lists, Clash / mihomo YAML and sing-box JSON. One bad entry never fails
// the whole subscription; it is reported in Result.Skipped instead.
package subscription

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"coreshift/engine/internal/node"
)

type Format string

const (
	FormatBase64  Format = "base64"
	FormatLinks   Format = "links"
	FormatClash   Format = "clash"
	FormatSingBox Format = "sing-box"
)

type Result struct {
	Format  Format
	Nodes   []node.Node
	Skipped []Skipped
}

// Skipped describes an entry that could not be used. It never contains
// credentials: entries are identified by position and kind only.
type Skipped struct {
	Index  int    // 1-based line or list position
	Kind   string // link scheme or proxy type
	Reason string
}

func (s Skipped) String() string {
	return fmt.Sprintf("#%d (%s): %s", s.Index, s.Kind, s.Reason)
}

var ErrNoNodes = errors.New("subscription contains no usable nodes")

// Parse detects the format of a subscription body and parses it.
func Parse(body []byte) (Result, error) {
	body = bytes.TrimSpace(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf")))
	if len(body) == 0 {
		return Result{}, errors.New("subscription is empty")
	}
	var res Result
	var err error
	switch {
	case body[0] == '{' || body[0] == '[':
		res, err = parseJSON(body)
	case looksLikeLinks(body):
		res = parseLinks(body)
	case looksLikeClash(body):
		res, err = parseClash(body)
	default:
		dec, derr := decodeBase64(string(body))
		if derr != nil || !looksLikeLinks(bytes.TrimSpace(dec)) {
			return Result{}, errors.New("unrecognized subscription format")
		}
		res = parseLinks(bytes.TrimSpace(dec))
		res.Format = FormatBase64
	}
	if err != nil {
		return Result{}, err
	}
	if len(res.Nodes) == 0 {
		if len(res.Skipped) > 0 {
			return res, fmt.Errorf("%w (%d entries skipped, first: %s)", ErrNoNodes, len(res.Skipped), res.Skipped[0])
		}
		return res, ErrNoNodes
	}
	return res, nil
}

// looksLikeLinks reports whether the first line that is not blank or a
// comment is a share link.
func looksLikeLinks(b []byte) bool {
	var line string
	for rest := string(b); rest != ""; {
		line, rest, _ = strings.Cut(rest, "\n")
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			break
		}
	}
	scheme, _, ok := strings.Cut(line, "://")
	if !ok || scheme == "" {
		return false
	}
	for _, r := range scheme {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func looksLikeClash(b []byte) bool {
	return bytes.HasPrefix(b, []byte("proxies:")) || bytes.Contains(b, []byte("\nproxies:"))
}

func parseLinks(b []byte) Result {
	res := Result{Format: FormatLinks}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	idx := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		idx++
		n, err := ParseLink(line)
		if err != nil {
			scheme, _, _ := strings.Cut(line, "://")
			res.Skipped = append(res.Skipped, Skipped{Index: idx, Kind: strings.ToLower(scheme), Reason: unwrapScheme(err)})
			continue
		}
		res.Nodes = append(res.Nodes, n)
	}
	return res
}

// unwrapScheme drops the "scheme: " prefix ParseLink adds, since Skipped.Kind has it.
func unwrapScheme(err error) string {
	_, msg, ok := strings.Cut(err.Error(), ": ")
	if !ok {
		return err.Error()
	}
	return msg
}

func parseClash(b []byte) (Result, error) {
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return Result{}, fmt.Errorf("invalid Clash YAML: %w", err)
	}
	res := Result{Format: FormatClash}
	for i, p := range doc.Proxies {
		f := fields(p)
		n, err := clashProxy(f)
		if err != nil {
			res.Skipped = append(res.Skipped, Skipped{Index: i + 1, Kind: f.str("type"), Reason: err.Error()})
			continue
		}
		res.Nodes = append(res.Nodes, n)
	}
	return res, nil
}

func parseJSON(b []byte) (Result, error) {
	if b[0] == '[' {
		return Result{}, errors.New("Xray JSON subscriptions are not supported yet")
	}
	var doc fields
	if err := json.Unmarshal(b, &doc); err != nil {
		return Result{}, fmt.Errorf("invalid JSON: %w", err)
	}
	outbounds := doc.list("outbounds")
	for _, o := range outbounds {
		if o.str("protocol") != "" {
			return Result{}, errors.New("Xray JSON subscriptions are not supported yet")
		}
	}
	res := Result{Format: FormatSingBox}
	for i, o := range append(outbounds, doc.list("endpoints")...) {
		typ := o.str("type")
		if singBoxGroupTypes[typ] {
			continue
		}
		n, err := singBoxOutbound(o)
		if err != nil {
			res.Skipped = append(res.Skipped, Skipped{Index: i + 1, Kind: typ, Reason: err.Error()})
			continue
		}
		res.Nodes = append(res.Nodes, n)
	}
	return res, nil
}
