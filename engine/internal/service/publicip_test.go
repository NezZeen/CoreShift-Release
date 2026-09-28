package service

import "testing"

func TestParseIPAnswer(t *testing.T) {
	for _, c := range []struct {
		body    string
		ip, loc string
		bad     bool
	}{
		{body: "fl=12f\nh=www.cloudflare.com\nip=203.0.113.7\nts=1.2\nloc=de\ncolo=FRA\n", ip: "203.0.113.7", loc: "DE"},
		{body: "ip=2001:db8::1\nloc=XX\n", ip: "2001:db8::1"},
		{body: "198.51.100.4\n", ip: "198.51.100.4"},
		{body: "<html>blocked</html>", bad: true},
		{body: "ip=\nloc=NL\n", bad: true},
	} {
		info, err := parseIPAnswer(c.body)
		if c.bad {
			if err == nil {
				t.Errorf("%q: got %+v, want an error", c.body, info)
			}
			continue
		}
		if err != nil || info.IP != c.ip || info.Country != c.loc {
			t.Errorf("%q: got %+v, %v; want %s %s", c.body, info, err, c.ip, c.loc)
		}
	}
}
