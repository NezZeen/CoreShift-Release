package node

import (
	"bytes"
	"encoding/json"
	"errors"
)

// XHTTP's "extra" comes from the subscription, and xray takes in it a whole
// second stream (downloadSettings) with TLS certificate and key files, a TLS
// key log file and socket options. Run as SYSTEM, a core given those would
// read or write any file the panel names. SanitizeXHTTPExtra keeps only the
// options that shape the HTTP traffic.

// rule checks one value; ok false drops it. changed reports that something
// inside was dropped.
type rule func(v any) (out any, ok, changed bool)

func scalar(v any) (any, bool, bool) {
	switch v.(type) {
	case string, json.Number, bool:
		return v, true, false
	}
	return nil, false, true
}

func str(v any) (any, bool, bool) {
	if _, ok := v.(string); ok {
		return v, true, false
	}
	return nil, false, true
}

func boolean(v any) (any, bool, bool) {
	if _, ok := v.(bool); ok {
		return v, true, false
	}
	return nil, false, true
}

// rangeOf is a number, a "from-to" string or {"from": …, "to": …}.
var rangeOf = rule(func(v any) (any, bool, bool) {
	if m, ok := v.(map[string]any); ok {
		return object(map[string]rule{"from": scalar, "to": scalar})(m)
	}
	return scalar(v)
})

func stringMap(v any) (any, bool, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false, true
	}
	out := map[string]any{}
	changed := false
	for k, x := range m {
		if s, ok := x.(string); ok {
			out[k] = s
		} else {
			changed = true
		}
	}
	return out, true, changed
}

func listOf(r rule) rule {
	return func(v any) (any, bool, bool) {
		l, ok := v.([]any)
		if !ok {
			return nil, false, true
		}
		out := []any{}
		changed := false
		for _, x := range l {
			y, ok, ch := r(x)
			changed = changed || ch || !ok
			if ok {
				out = append(out, y)
			}
		}
		return out, true, changed
	}
}

func object(fields map[string]rule) rule {
	return func(v any) (any, bool, bool) {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false, true
		}
		out := map[string]any{}
		changed := false
		for k, x := range m {
			r, known := fields[k]
			if !known {
				changed = true
				continue
			}
			y, ok, ch := r(x)
			changed = changed || ch || !ok
			if ok {
				out[k] = y
			}
		}
		return out, true, changed
	}
}

// xhttpTraffic are the extra options that shape the requests themselves.
func xhttpTraffic() map[string]rule {
	return map[string]rule{
		"headers":              stringMap,
		"xPaddingBytes":        rangeOf,
		"noGRPCHeader":         boolean,
		"noSSEHeader":          boolean,
		"scMaxEachPostBytes":   rangeOf,
		"scMinPostsIntervalMs": rangeOf,
		"scMaxBufferedPosts":   rangeOf,
		"scStreamUpServerSecs": rangeOf,
		"xmux": object(map[string]rule{
			"maxConcurrency":   rangeOf,
			"maxConnections":   rangeOf,
			"cMaxReuseTimes":   rangeOf,
			"hMaxRequestTimes": rangeOf,
			"hMaxReusableSecs": rangeOf,
			"hKeepAlivePeriod": scalar,
		}),
	}
}

// xhttpExtra is what a subscription may put into extra.
var xhttpExtra = func() rule {
	inner := xhttpTraffic()
	inner["host"], inner["path"], inner["mode"] = str, str, str
	top := xhttpTraffic()
	top["downloadSettings"] = object(map[string]rule{
		"address":       str,
		"port":          scalar,
		"network":       str,
		"security":      str,
		"xhttpSettings": object(inner),
		// The client's side of REALITY only.
		"realitySettings": object(map[string]rule{
			"serverName": str, "fingerprint": str, "publicKey": str, "password": str,
			"shortId": str, "spiderX": str, "mldsa65Verify": str,
		}),
		"tlsSettings": object(map[string]rule{
			"serverName": str, "alpn": listOf(str), "fingerprint": str, "allowInsecure": boolean,
		}),
	})
	return object(top)
}()

// SanitizeXHTTPExtra returns raw, a JSON object, with only the options a
// subscription may set: the HTTP side of XHTTP and, for downloadSettings,
// where to connect and how to secure it. Socket options, certificate and
// key files, key logs and anything unknown are left out. When nothing is
// left out raw is returned as it is, so the node's fingerprint stays the
// same; "" when nothing remains.
func SanitizeXHTTPExtra(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	d := json.NewDecoder(bytes.NewReader([]byte(raw)))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return "", errors.New("xhttp extra is not valid JSON")
	}
	if d.More() {
		return "", errors.New("xhttp extra is not valid JSON")
	}
	out, ok, changed := xhttpExtra(v)
	if !ok {
		return "", errors.New("xhttp extra is not a JSON object")
	}
	if len(out.(map[string]any)) == 0 {
		return "", nil
	}
	if !changed {
		return raw, nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
