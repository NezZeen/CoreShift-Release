package subscription

import (
	"fmt"
	"strconv"
	"strings"
)

// fields reads loosely typed maps decoded from JSON or YAML, where the same
// value may arrive as a number, a string or a bool depending on the generator.
type fields map[string]any

// str returns the first present key as a string.
func (f fields) str(keys ...string) string {
	for _, k := range keys {
		switch v := f[k].(type) {
		case nil:
			continue
		case string:
			return v
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		default:
			return fmt.Sprint(v)
		}
	}
	return ""
}

func (f fields) int(keys ...string) int {
	s := f.str(keys...)
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	// Bandwidths like "100 Mbps".
	s = strings.TrimSpace(s)
	end := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' })
	if end > 0 {
		n, _ := strconv.Atoi(s[:end])
		return n
	}
	return 0
}

func (f fields) bool(keys ...string) bool {
	switch strings.ToLower(f.str(keys...)) {
	case "true", "1", "yes":
		return true
	}
	return false
}

func (f fields) sub(key string) fields {
	switch v := f[key].(type) {
	case map[string]any:
		return v
	case fields:
		return v
	}
	return fields{}
}

// strs returns a list value; a scalar becomes a one-element (or comma-split) list.
func (f fields) strs(key string) []string {
	switch v := f[key].(type) {
	case []any:
		var out []string
		for _, e := range v {
			if s := (fields{"v": e}).str("v"); s != "" {
				out = append(out, s)
			}
		}
		return out
	case nil:
		return nil
	}
	return splitList(f.str(key))
}

// list returns a list of objects.
func (f fields) list(key string) []fields {
	raw, _ := f[key].([]any)
	var out []fields
	for _, e := range raw {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
