package subscription

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// shapeShown are the keys whose string values are shown as they are: they
// name a kind or a role, never a credential or an address.
var shapeShown = map[string]bool{
	"type": true, "protocol": true, "network": true, "security": true, "strategy": true,
	"flow": true, "tag": true, "remarks": true, "name": true, "fallbacktag": true,
	"selector": true, "outboundtag": true, "balancertag": true, "domainstrategy": true,
	"mode": true, "fingerprint": true, "alpn": true, "encryption": true, "packetencoding": true,
	"outbounds": true, "default": true, "final": true,
}

// shapeKeep is how many elements of a long list are shown.
const shapeKeep = 4

// Shape describes the structure of a JSON subscription for a bug report: the
// keys, the kinds of the values and the names that give it its logic
// (protocols, tags, balancer strategies). Every other string, such as an
// address, an id, a key or a link, is replaced by its length, so the output is
// safe to share after a look.
func Shape(body []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}
	var b strings.Builder
	writeShape(&b, doc, 0, false)
	b.WriteByte('\n')
	return b.String(), nil
}

func writeShape(b *strings.Builder, v any, depth int, shown bool) {
	indent := strings.Repeat("  ", depth)
	switch v := v.(type) {
	case map[string]any:
		if len(v) == 0 {
			b.WriteString("{}")
			return
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("{\n")
		for i, k := range keys {
			kb, _ := json.Marshal(k)
			fmt.Fprintf(b, "%s  %s: ", indent, kb)
			writeShape(b, v[k], depth+1, shapeShown[strings.ToLower(k)])
			if i < len(keys)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(indent + "}")
	case []any:
		if len(v) == 0 {
			b.WriteString("[]")
			return
		}
		items := v
		if len(items) > shapeKeep {
			items = items[:shapeKeep]
		}
		b.WriteString("[\n")
		for i, it := range items {
			b.WriteString(indent + "  ")
			writeShape(b, it, depth+1, shown)
			if i < len(items)-1 || len(v) > len(items) {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		if len(v) > len(items) {
			fmt.Fprintf(b, "%s  \"… %d items in all\"\n", indent, len(v))
		}
		b.WriteString(indent + "]")
	case string:
		if shown && utf8.RuneCountInString(v) <= 60 {
			s, _ := json.Marshal(v)
			b.Write(s)
			return
		}
		fmt.Fprintf(b, "\"<string, %d chars>\"", utf8.RuneCountInString(v))
	case json.Number:
		b.WriteString(v.String())
	case bool:
		fmt.Fprint(b, v)
	case nil:
		b.WriteString("null")
	}
}
