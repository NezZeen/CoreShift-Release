package dnsguard

import (
	"fmt"
	"testing"
)

func TestCleanSearchDomains(t *testing.T) {
	got := CleanSearchDomains([]string{
		" Corp.Example. ", "corp.example", "lan", "ru", "com", "", "bad domain", "-x.example", "xn--80ak6aa92e.example", "home.arpa", "a..b",
	})
	want := "[corp.example lan xn--80ak6aa92e.example home.arpa]"
	if fmt.Sprint(got) != want {
		t.Errorf("got %v, want %s", got, want)
	}
	var many []string
	for i := range 40 {
		many = append(many, fmt.Sprintf("d%d.example", i))
	}
	if n := len(CleanSearchDomains(many)); n != maxSearchDomains {
		t.Errorf("%d domains kept, want %d", n, maxSearchDomains)
	}
}

func TestResolvConfSearch(t *testing.T) {
	conf := "# by NetworkManager\nnameserver 10.0.0.1\ndomain office.example\nsearch corp.example lan\noptions edns0\n"
	if got := fmt.Sprint(resolvConfSearch([]byte(conf))); got != "[office.example corp.example lan]" {
		t.Errorf("got %s", got)
	}
}
