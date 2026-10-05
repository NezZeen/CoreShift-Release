package core

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

// clashAPI imitates the Clash API's /connections of sing-box and mihomo:
// the totals and the open connections, n of them, as a busy phone has.
func clashAPI(t testing.TB, n int) (netip.AddrPort, *atomic.Int64) {
	var conns []string
	for i := range n {
		conns = append(conns, fmt.Sprintf(`{"id":"%08x-0000-0000-0000-000000000000","metadata":{"network":"tcp","type":"tun",`+
			`"sourceIP":"172.19.0.1","destinationIP":"203.0.113.%d","sourcePort":"%d","destinationPort":"443","host":"cdn%d.example.com",`+
			`"dnsMode":"normal","processPath":""},"upload":%d,"download":%d,"start":"2026-10-05T10:00:00Z","chains":["proxy"],"rule":"final"}`,
			i, i%250, 40000+i, i, i*100, i*1000))
	}
	body := `{"downloadTotal":123456789,"uploadTotal":2345678,"connections":[` + strings.Join(conns, ",") + `],"memory":1234}`
	var dials atomic.Int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			dials.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)
	return netip.MustParseAddrPort(strings.TrimPrefix(srv.URL, "http://")), &dials
}

func TestClashTrafficReusesConnection(t *testing.T) {
	addr, dials := clashAPI(t, 3)
	for range 5 {
		tr, err := ReadTraffic(context.Background(), SingBox, addr, "s3cret")
		if err != nil {
			t.Fatal(err)
		}
		if tr != (Traffic{Up: 2345678, Down: 123456789}) {
			t.Fatalf("traffic = %+v", tr)
		}
	}
	// Once a second for as long as the VPN is on: one connection to the
	// core's API, not one per sample.
	if n := dials.Load(); n != 1 {
		t.Errorf("%d connections for 5 samples, want 1", n)
	}
	if _, err := ReadTraffic(context.Background(), SingBox, addr, "wrong"); err == nil {
		t.Error("a wrong secret must fail")
	}
}

func TestClashTotals(t *testing.T) {
	for _, c := range []struct {
		name, body string
		want       Traffic
		bad        bool
	}{
		// mihomo: the totals first.
		{"mihomo", `{"downloadTotal":1000,"uploadTotal":20,"connections":[{"upload":5,"download":6}],"memory":7}`, Traffic{20, 1000}, false},
		// sing-box sorts the keys: the connections come first.
		{"sing-box", `{"connections":[{"chains":["p"],"download":6,"metadata":{"host":"a"},"upload":5}],"downloadTotal":1000,"memory":7,"uploadTotal":20}`,
			Traffic{20, 1000}, false},
		{"spaces", "{ \"uploadTotal\" : 20 ,\n \"downloadTotal\": 1000 }", Traffic{20, 1000}, false},
		{"null connections", `{"connections":null,"downloadTotal":3,"uploadTotal":4}`, Traffic{4, 3}, false},
		// Names in connections that look like the keys are not taken for them.
		{"lookalikes", `{"connections":[{"host":"\"uploadTotal\":9","uploadTotal":8,"x":{"downloadTotal":9}}],"downloadTotal":1,"uploadTotal":2}`,
			Traffic{2, 1}, false},
		{"escaped key", `{"uploadTotal":9,"uploadTotal":2,"downloadTotal":1}`, Traffic{2, 1}, false},
		{"missing totals", `{"connections":[]}`, Traffic{}, false},
		{"empty", ``, Traffic{}, true},
		{"cut short", `{"connections":[{"id":"a`, Traffic{}, true},
		{"not a number", `{"uploadTotal":"x","downloadTotal":1}`, Traffic{}, true},
		{"too big", `{"uploadTotal":99999999999999999999,"downloadTotal":1}`, Traffic{}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := clashTotals(bufio.NewReader(strings.NewReader(c.body)))
			if c.bad {
				if err == nil {
					t.Fatalf("want an error, got %+v", got)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, c.want)
			}
		})
	}
}

func BenchmarkClashTraffic(b *testing.B) {
	for _, n := range []int{0, 50, 300} {
		b.Run(fmt.Sprintf("connections=%d", n), func(b *testing.B) {
			addr, _ := clashAPI(b, n)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ReadTraffic(context.Background(), SingBox, addr, "s3cret"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
