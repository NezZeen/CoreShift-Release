package store

import (
	"reflect"
	"slices"
)

// cloneData copies what modify callbacks may change in place; nodes are
// never modified in place, only replaced.
func cloneData(d fileData) fileData {
	d.Settings = cloneSettings(d.Settings)
	d.Subscriptions = slices.Clone(d.Subscriptions)
	if d.Selection != nil {
		sel := *d.Selection
		d.Selection = &sel
	}
	return d
}

// cloneSettings copies every slice and map of s, so that the copy can be
// changed, decoded into among others, without touching s.
func cloneSettings(s Settings) Settings {
	cloneDeep(reflect.ValueOf(&s).Elem())
	return s
}

// cloneDeep replaces the slices, maps and pointers reachable from v with
// copies. Nil stays nil and empty stays empty, as JSON tells them apart.
func cloneDeep(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				cloneDeep(f)
			}
		}
	case reflect.Slice:
		if v.IsNil() {
			return
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		reflect.Copy(c, v)
		for i := range c.Len() {
			cloneDeep(c.Index(i))
		}
		v.Set(c)
	case reflect.Map:
		if v.IsNil() {
			return
		}
		c := reflect.MakeMapWithSize(v.Type(), v.Len())
		for it := v.MapRange(); it.Next(); {
			e := reflect.New(v.Type().Elem()).Elem()
			e.Set(it.Value())
			cloneDeep(e)
			c.SetMapIndex(it.Key(), e)
		}
		v.Set(c)
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		c := reflect.New(v.Type().Elem())
		c.Elem().Set(v.Elem())
		cloneDeep(c.Elem())
		v.Set(c)
	}
}
