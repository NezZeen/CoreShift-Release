package msg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	for _, c := range []struct {
		m    Msg
		lang string
		want string
	}{
		{New("net.timeout"), "ru", "нет ответа"},
		{New("reach.server_up", "addr", "1.2.3.4:443"), "ru", "сервер 1.2.3.4:443 отвечает напрямую, но соединение через него не проходит"},
		// A nested message, an error as its raw text.
		{New("reach.server_down", "addr", "a:1", "err", New("net.refused")), "ru", "сервер a:1 не отвечает напрямую (соединение отклонено), а интернет работает"},
		{New("dns.revert_failed", "err", errors.New("access denied")), "ru", "не удалось восстановить системный DNS: access denied"},
		// Plurals: one, few, many.
		{New("log.repeats", "line", Raw("x"), "n", 1, "period", New("time.sec", "n", 30)), "ru", "x — ещё 1 раз за 30 с"},
		{New("log.repeats", "line", Raw("x"), "n", 3, "period", New("time.min", "n", 2)), "ru", "x — ещё 3 раза за 2 мин"},
		{New("sub.days.title", "name", "A", "days", 2), "en", "Subscription “A” ends in 2 days"},
		{New("sub.days.title", "name", "A", "days", 1), "en", "Subscription “A” ends in 1 day"},
		{New("sub.days.title", "name", "A", "days", 1), "ru", "Подписка «A» закончится через 1 день"},
		// A list, with its separator.
		{New("reach.offline", "errs", Join("; ", New("net.host_err", "host", "h1", "err", New("net.timeout")), New("net.host_err", "host", "h2", "err", Raw("x")))), "ru",
			"не отвечают ни сервер, ни известные узлы: h1 — нет ответа; h2 — x"},
		// An argument that says nothing ends the text without a space.
		{New("direct.blocked", "n", 5, "advice", New("direct.advice.none")), "ru",
			"Прямые соединения не проходят (5 за минуту), а через VPN всё работает: похоже, сеть пропускает только белый список или российские сайты отсюда недоступны."},
		// Without English, Russian; an unknown code is itself.
		{New("net.timeout"), "en", "нет ответа"},
		{New("no.such.code"), "ru", "no.such.code"},
		{Raw("as is"), "en", "as is"},
	} {
		if got := c.m.In(c.lang); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.m.Code, c.lang, got, c.want)
		}
	}
}

func TestErrorAndOf(t *testing.T) {
	err := Err("net.status", "status", "503 Service Unavailable")
	if err.Error() != "ответ 503 Service Unavailable" {
		t.Errorf("text %q", err.Error())
	}
	if m, ok := Of(err); !ok || m.Code != "net.status" {
		t.Errorf("Of = %+v %v", m, ok)
	}
	// Wrapped in words of another, the code would leave them out.
	if _, ok := Of(fmt.Errorf("connect: %w", err)); ok {
		t.Error("a wrapped coded error taken for its message")
	}
	if _, ok := Of(errors.New("plain")); ok {
		t.Error("a plain error has a code")
	}
}

// The app gets messages as JSON: a code, args that are strings, numbers,
// messages or lists.
func TestJSON(t *testing.T) {
	b, _ := json.Marshal(New("net.changed", "was", Raw("eth0"), "now", Join(" · ", New("net.ipv6.no"), New("net.dns_count", "n", 2))))
	want := `{"code":"net.changed","args":{"now":{"sep":" · ","items":[{"code":"net.ipv6.no"},{"code":"net.dns_count","args":{"n":2}}]},"was":{"code":"raw","args":{"text":"eth0"}}}}`
	if string(b) != want {
		t.Errorf("json\n%s\nwant\n%s", b, want)
	}
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)[|}]`)

func names(t string) []string {
	var out []string
	for _, m := range placeholder.FindAllStringSubmatch(t, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	slices.Sort(out)
	return out
}

// English templates are of Russian codes and take the same arguments.
func TestCatalogs(t *testing.T) {
	for code, tmpl := range en {
		r, ok := ru[code]
		if !ok {
			t.Errorf("%s is English only", code)
			continue
		}
		if !slices.Equal(names(r), names(tmpl)) {
			t.Errorf("%s: arguments %v in Russian, %v in English", code, names(r), names(tmpl))
		}
	}
	for code, tmpl := range ru {
		if strings.Contains(tmpl, `\`) || strings.Count(tmpl, "{") != strings.Count(tmpl, "}") {
			t.Errorf("%s: %q", code, tmpl)
		}
	}
}

// Every code the engine names literally is in the catalog; those it puts
// together are checked by the tests of their parts.
var used = regexp.MustCompile(`msg\.(?:New|Err)\("([^"]+)"[,)]`)

func TestCodesUsedAreKnown(t *testing.T) {
	root := filepath.Join("..", "..")
	n := 0
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range used.FindAllStringSubmatch(string(b), -1) {
			n++
			if !Has(m[1]) {
				t.Errorf("%s: unknown code %q", path, m[1])
			}
		}
		return nil
	})
	if n < 50 {
		t.Errorf("only %d codes found: the scan misses them", n)
	}
}
