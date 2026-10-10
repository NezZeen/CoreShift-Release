// Package msg carries CoreShift's own sentences for the user as a code and
// the values to put in, so that the app says them in its language: the
// engine sends Msg (an event's Code and Args, a checkup step's, a failed
// connection's), the app looks the code up in its dictionary
// (app/lib/l10n/engine_strings.dart) and fills the template in. The Russian
// text goes along too, rendered here from the same templates (catalog_ru.go):
// apps from before the codes show it, and so does the journal. Raw errors
// of the cores and of Go stay as they are: only sentences of CoreShift's own
// become codes.
//
// A template names an argument in braces, «сервер {host} не отвечает», and
// picks a word by a number with the forms after bars: «{n} {n|раз|раза|раз}»
// (Russian: one, few, many; English: one, other). An argument is a string,
// an integer, another Msg or a List of them. The result is trimmed, so an
// argument that says nothing may end a template.
//
// Codes are dotted, lowercase: "area.what", "checkup.server.port_fail".
// Every code used must be in catalog_ru.go and in the app's dictionaries,
// with the same Russian; the tests of both sides check it.
package msg

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Msg is a sentence of CoreShift's own: Code says which, Args fill it in.
type Msg struct {
	Code string         `json:"code"`
	Args map[string]any `json:"args,omitempty"`
}

// List is an argument of several messages, said one after another with Sep
// between them.
type List struct {
	Sep   string `json:"sep"`
	Items []Msg  `json:"items"`
}

// CodeRaw is the code of a text that is not CoreShift's own: a core's error,
// a name. Its argument "text" is said as it is.
const CodeRaw = "raw"

// New makes a message of code and its arguments as name, value pairs. An
// error as a value is said as its own message when it is one (Of), else as
// its raw text.
func New(code string, kv ...any) Msg {
	m := Msg{Code: code}
	if len(kv) > 1 {
		m.Args = make(map[string]any, len(kv)/2)
		for i := 0; i+1 < len(kv); i += 2 {
			name, _ := kv[i].(string)
			m.Args[name] = arg(kv[i+1])
		}
	}
	return m
}

// Raw is a text said as it is, in any language.
func Raw(text string) Msg { return Msg{Code: CodeRaw, Args: map[string]any{"text": text}} }

// Join is a List of items with sep between them.
func Join(sep string, items ...Msg) List { return List{Sep: sep, Items: items} }

func arg(v any) any {
	switch v := v.(type) {
	case string, Msg, List, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return v
	case []Msg:
		return Join(", ", v...)
	case error:
		if m, ok := Of(v); ok {
			return m
		}
		return Raw(v.Error())
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

// IsZero reports a message that says nothing.
func (m Msg) IsZero() bool { return m.Code == "" }

// String is the message in Russian, as the journal and older apps show it.
func (m Msg) String() string { return m.In("ru") }

// In is the message in lang ("ru", "en"): its template there, else the
// Russian one, else the code itself.
func (m Msg) In(lang string) string {
	if m.Code == CodeRaw {
		return text(m.Args["text"], lang)
	}
	t, ok := catalogs[lang][m.Code]
	tlang := lang
	if !ok {
		if t, ok = ru[m.Code]; !ok {
			return m.Code
		}
		tlang = "ru"
	}
	return strings.TrimSpace(fill(t, m.Args, tlang, lang))
}

// Has reports whether code is in the Russian catalog.
func Has(code string) bool {
	_, ok := ru[code]
	return ok || code == CodeRaw
}

// Codes returns the codes of lang's catalog, for tests.
func Codes(lang string) map[string]string {
	out := map[string]string{}
	for k, v := range catalogs[lang] {
		out[k] = v
	}
	return out
}

var catalogs = map[string]map[string]string{"ru": ru, "en": en}

// fill puts args, said in lang, into the template t of tlang.
func fill(t string, args map[string]any, tlang, lang string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(t, '{')
		if i < 0 {
			b.WriteString(t)
			return b.String()
		}
		j := strings.IndexByte(t[i:], '}')
		if j < 0 {
			b.WriteString(t)
			return b.String()
		}
		b.WriteString(t[:i])
		spec := t[i+1 : i+j]
		t = t[i+j+1:]
		name, forms, plural := strings.Cut(spec, "|")
		if plural {
			b.WriteString(pluralForm(tlang, number(args[name]), strings.Split(forms, "|")))
			continue
		}
		b.WriteString(text(args[name], lang))
	}
}

// text says an argument in lang.
func text(v any, lang string) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case Msg:
		return v.In(lang)
	case List:
		parts := make([]string, len(v.Items))
		for i, m := range v.Items {
			parts[i] = m.In(lang)
		}
		return strings.Join(parts, v.Sep)
	}
	return fmt.Sprint(v)
}

func number(v any) int64 {
	switch v := v.(type) {
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}

// pluralForm picks the word for n of forms: Russian one, few, many;
// English one, other.
func pluralForm(lang string, n int64, forms []string) string {
	pick := func(i int) string {
		if i < len(forms) {
			return forms[i]
		}
		return forms[len(forms)-1]
	}
	if n < 0 {
		n = -n
	}
	if lang != "ru" {
		if n == 1 {
			return pick(0)
		}
		return pick(1)
	}
	switch m10, m100 := n%10, n%100; {
	case m10 == 1 && m100 != 11:
		return pick(0)
	case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
		return pick(1)
	}
	return pick(2)
}

// Error is an error that says a message: its text is the Russian one.
type Error struct{ M Msg }

func (e *Error) Error() string { return e.M.String() }

// Message is what the error says.
func (e *Error) Message() Msg { return e.M }

// Err is an error that says the message of code and its arguments.
func Err(code string, kv ...any) error { return &Error{New(code, kv...)} }

// Coded is an error that says a message of CoreShift's own.
type Coded interface {
	error
	Message() Msg
}

// Of returns the message err says, when the whole of its text is one: a
// Coded error, not wrapped in words of another.
func Of(err error) (Msg, bool) {
	var c Coded
	if err == nil || !errors.As(err, &c) || c.Error() != err.Error() {
		return Msg{}, false
	}
	return c.Message(), true
}
