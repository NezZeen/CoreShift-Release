package store

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

// keepUnknown carries into cur, the file about to be written, the fields of
// old, the file as read, that type t does not know: those a later version
// of CoreShift wrote. Without it, running an earlier version to compare
// would silently erase the settings only the later one has, and going back
// to the later version would find them gone.
//
// Only fields t does not declare are carried: a known field missing from
// cur was cleared on purpose (omitempty). Objects are merged field by
// field, and lists of objects with an "id" (subscriptions) item by item.
// It returns cur unchanged when old has nothing unknown.
func keepUnknown(cur, old []byte, t reflect.Type) ([]byte, error) {
	if len(old) == 0 {
		return cur, nil
	}
	var o any
	if err := decode(old, &o); err != nil {
		return cur, nil // an unreadable old file has nothing to keep
	}
	var c any
	if err := decode(cur, &c); err != nil {
		return nil, err
	}
	if !merge(c, o, t) {
		return cur, nil
	}
	return json.MarshalIndent(c, "", "  ")
}

// decode keeps numbers as written, so carried values do not change.
func decode(b []byte, v *any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}

// merge adds old's unknown fields to cur in place and reports whether it
// added any.
func merge(cur, old any, t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		c, ok1 := cur.(map[string]any)
		o, ok2 := old.(map[string]any)
		if !ok1 || !ok2 {
			return false
		}
		known := jsonFields(t)
		added := false
		for k, ov := range o {
			ft, isKnown := known[k]
			cv, inCur := c[k]
			switch {
			case !isKnown && !inCur:
				c[k] = ov
				added = true
			case isKnown && inCur:
				added = merge(cv, ov, ft) || added
			}
		}
		return added
	case reflect.Slice, reflect.Array:
		c, ok1 := cur.([]any)
		o, ok2 := old.([]any)
		if !ok1 || !ok2 {
			return false
		}
		et := t.Elem()
		for et.Kind() == reflect.Pointer {
			et = et.Elem()
		}
		if et.Kind() != reflect.Struct {
			return false
		}
		if _, ok := jsonFields(et)["id"]; !ok {
			return false
		}
		byID := map[any]any{}
		for _, ov := range o {
			if m, ok := ov.(map[string]any); ok && m["id"] != nil {
				byID[m["id"]] = ov
			}
		}
		added := false
		for _, cv := range c {
			if m, ok := cv.(map[string]any); ok {
				if ov, ok := byID[m["id"]]; ok {
					added = merge(cv, ov, et) || added
				}
			}
		}
		return added
	}
	return false
}

// jsonFields maps the JSON names of t's fields to their types, the way
// encoding/json names them, embedded structs included.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			et := f.Type
			for et.Kind() == reflect.Pointer {
				et = et.Elem()
			}
			if et.Kind() == reflect.Struct {
				for k, v := range jsonFields(et) {
					out[k] = v
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = f.Type
	}
	return out
}
