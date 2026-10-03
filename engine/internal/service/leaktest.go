package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"time"

	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/supervisor"
	"coreshift/engine/internal/tunlayer"
)

// The DNS leak test asks bash.ws, whose zone has a name for everything: a
// test gets an id, names <n>.<id>.bash.ws are looked up, and bash.ws lists
// the DNS servers that asked its name servers for them, and the address
// the list was fetched from.
//
// What users mean by a leak is: while connected, does the ISP see what the
// device looks up? So the test looks names up the way apps inside the VPN
// do. It runs in the service rather than the app, because on Android the
// app, and the engine inside it, is outside its own VPN: its lookups and
// requests would go around the tunnel and test the ISP instead.
//
//   - System: names resolved through the TUN layer's DNS, which takes
//     every app's lookups. On Android the layer runs in the app and is
//     asked directly (Config.TUNLookup); on the desktop the system's
//     resolver is used as other programs use it, which the DNS guard
//     points at the tunnel (systemLookup). With fake IP the tunnel answers
//     these names itself, and no DNS server sees them at all.
//   - Proxy: requests to the names through the core's SOCKS inbound, name
//     and all, as the TUN layer sends apps' connections: the VPN server
//     resolves them, and bash.ws sees its DNS.
//
// Every request to bash.ws goes through the SOCKS inbound as well, so it
// sees the VPN server's address, unless the server's own routing sends it
// direct. To tell, the address outside the VPN is looked up too, around
// the tunnel, and the app compares the two.

const (
	leakServer = "https://bash.ws"
	leakZone   = "bash.ws"
	// leakNames are looked up on each path.
	leakNames   = 3
	leakTimeout = 45 * time.Second
	// leakNameTimeout bounds each lookup and request of a test name.
	leakNameTimeout = 8 * time.Second
)

// LeakHost is an address bash.ws reports.
type LeakHost struct {
	IP string `json:"ip"`
	// Country is the ISO 3166 code in lower case, as bash.ws gives it.
	Country     string `json:"country,omitempty"`
	CountryName string `json:"country_name,omitempty"`
	Org         string `json:"org,omitempty"`
	// Path, of a DNS server: which names it looked up, "system" (apps'
	// lookups, through the TUN layer's DNS) or "proxy" (names of
	// connections, resolved by the VPN server).
	Path string `json:"path,omitempty"`
}

// LeakSystem is what the apps' path gave.
type LeakSystem struct {
	// Checked: the TUN layer's DNS answered. Not in proxy-only mode, where
	// apps' lookups are none of the tunnel's business.
	Checked bool `json:"checked"`
	// FakeIP: the tunnel answered every name itself with a fake address,
	// so no DNS server saw it; the name goes to the VPN server with the
	// connection.
	FakeIP bool   `json:"fake_ip,omitempty"`
	Error  string `json:"error,omitempty"`
}

// LeakResult is a DNS leak test.
type LeakResult struct {
	// Server is the node connected, ServerIP its address.
	Server   string `json:"server,omitempty"`
	ServerIP string `json:"server_ip,omitempty"`
	// Exit is where bash.ws saw the test come from: the VPN server's
	// address, unless the test went around it.
	Exit *LeakHost `json:"exit,omitempty"`
	// DNS are the servers that looked up the test names.
	DNS    []LeakHost `json:"dns"`
	System LeakSystem `json:"system"`
	// Home is the device's address outside the VPN; nil when it could not
	// be learned.
	Home *IPInfo `json:"home,omitempty"`
}

// LeakTest runs the DNS leak test through the connection.
func (s *Service) LeakTest(ctx context.Context) (LeakResult, error) {
	ctx, cancel := context.WithTimeout(ctx, leakTimeout)
	defer cancel()
	st := s.Status()
	if st.State != Connected {
		return LeakResult{}, supervisor.ErrNotConnected
	}
	s.mu.Lock()
	server := s.lastNode.Server
	s.mu.Unlock()

	// Both addresses come from elsewhere; they are looked up meanwhile.
	var wg sync.WaitGroup
	var home *IPInfo
	var serverIP string
	wg.Go(func() {
		if info, err := s.homeAddress(ctx); err == nil {
			home = &info
		}
	})
	if server != "" {
		wg.Go(func() {
			if ip, err := s.serverAddr(ctx, server); err == nil {
				serverIP = ip.String()
			}
		})
	}

	// The core's SOCKS inbound, credentials included: never print it.
	tr := newTransport(s.proxyURL())
	defer tr.CloseIdleConnections()
	p := leakProbe{base: s.cfg.leakBase, zone: s.cfg.leakDomain, client: &http.Client{Transport: tr}}
	if p.base == "" {
		p.base, p.zone = leakServer, leakZone
	}
	if st.TUN {
		p.system = s.cfg.TUNLookup
		if p.system == nil {
			p.system = systemLookup
		}
	}
	res, err := p.run(ctx)
	wg.Wait()
	if err != nil {
		return LeakResult{}, err
	}
	res.Server, res.ServerIP, res.Home = st.Node, serverIP, home
	return res, nil
}

// homeLookups answer with the caller's address; by IP, so that no lookup
// is needed outside the tunnel.
var homeLookups = []string{"https://1.1.1.1/cdn-cgi/trace", "https://1.0.0.1/cdn-cgi/trace"}

// homeAddress is the device's address outside the VPN, through the
// physical network. Where that cannot be had it fails rather than go
// through the tunnel, whose address would pass for the device's.
func (s *Service) homeAddress(ctx context.Context) (IPInfo, error) {
	if s.cfg.leakHome != nil {
		return s.cfg.leakHome(ctx)
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	if !s.cfg.AppOutsideVPN {
		bind, err := s.cfg.physical()
		if err == nil {
			bind = bindFor(bind, netip.MustParseAddr("1.1.1.1"))
		}
		if err != nil || bind == (ping.Bind{}) {
			return IPInfo{}, errors.New("no way around the tunnel")
		}
		dialer = bind.Dialer(8 * time.Second)
	}
	tr := newTransport(nil)
	tr.DialContext = dialer.DialContext
	tr.DisableKeepAlives = true
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	var err error
	for _, u := range homeLookups {
		var info IPInfo
		if info, err = lookupIP(ctx, client, u); err == nil {
			return info, nil
		}
	}
	return IPInfo{}, err
}

// leakProbe runs one test against bash.ws (or a stand-in).
type leakProbe struct {
	base string // "https://bash.ws"
	zone string // "bash.ws": the test names are <n>.<id>.<zone>
	// client goes through the core's SOCKS inbound.
	client *http.Client
	// system resolves a name as apps do; nil leaves that path out.
	system func(ctx context.Context, host string) ([]netip.Addr, error)
}

// leakEntry is one entry of bash.ws's answer.
type leakEntry struct {
	IP          string `json:"ip"`
	Country     string `json:"country"`
	CountryName string `json:"country_name"`
	ASN         string `json:"asn"`
	Org         string `json:"org"`
	Type        string `json:"type"` // "ip", "dns" or "conclusion"
}

func (e leakEntry) host(path string) LeakHost {
	org := e.Org
	if org == "" {
		org = e.ASN
	}
	return LeakHost{IP: e.IP, Country: strings.ToLower(e.Country), CountryName: e.CountryName, Org: org, Path: path}
}

var leakID = regexp.MustCompile(`^[a-z0-9]{4,64}$`)

func (p leakProbe) run(ctx context.Context) (LeakResult, error) {
	// A test per path, so each DNS server is known by the names it asked.
	proxyID, err := p.newID(ctx)
	if err != nil {
		return LeakResult{}, err
	}
	var systemID string
	if p.system != nil {
		if systemID, err = p.newID(ctx); err != nil {
			return LeakResult{}, err
		}
	}

	var wg sync.WaitGroup
	for i := 1; i <= leakNames; i++ {
		// Each name is new, so no cache can answer it: some DNS server has
		// to ask bash.ws. The requests themselves need not succeed.
		wg.Go(func() { p.visit(ctx, fmt.Sprintf("http://%d.%s.%s/", i, proxyID, p.zone)) })
	}
	answers := make([][]netip.Addr, leakNames)
	errs := make([]error, leakNames)
	if p.system != nil {
		for i := range leakNames {
			wg.Go(func() {
				lctx, cancel := context.WithTimeout(ctx, leakNameTimeout)
				defer cancel()
				answers[i], errs[i] = p.system(lctx, fmt.Sprintf("%d.%s.%s", i+1, systemID, p.zone))
			})
		}
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return LeakResult{}, err
	}

	res := LeakResult{DNS: []LeakHost{}}
	seen := map[string]bool{}
	add := func(entries []leakEntry, path string) {
		for _, e := range entries {
			if e.Type == "dns" && e.IP != "" && !seen[e.IP] {
				seen[e.IP] = true
				res.DNS = append(res.DNS, e.host(path))
			}
		}
	}
	if p.system != nil {
		res.System = systemVerdict(answers, errs)
		entries, err := p.results(ctx, systemID)
		if err != nil {
			return LeakResult{}, err
		}
		add(entries, "system")
	}
	entries, err := p.results(ctx, proxyID)
	if err != nil {
		return LeakResult{}, err
	}
	add(entries, "proxy")
	for _, e := range entries {
		if e.Type == "ip" && e.IP != "" {
			h := e.host("")
			res.Exit = &h
			break
		}
	}
	if res.Exit == nil {
		return LeakResult{}, errors.New("bash.ws: no test requests reached it")
	}
	return res, nil
}

// systemVerdict sums up the apps' path from the answers to each name.
func systemVerdict(answers [][]netip.Addr, errs []error) LeakSystem {
	var sys LeakSystem
	fake := true
	for i, addrs := range answers {
		if errs[i] != nil || len(addrs) == 0 {
			if sys.Error == "" && errs[i] != nil {
				sys.Error = errs[i].Error()
			}
			continue
		}
		sys.Checked = true
		for _, a := range addrs {
			if !isFakeIP(a) {
				fake = false
			}
		}
	}
	if sys.Checked {
		sys.FakeIP, sys.Error = fake, ""
	} else if sys.Error == "" {
		sys.Error = "no answer"
	}
	return sys
}

func isFakeIP(a netip.Addr) bool {
	a = a.Unmap()
	return tunlayer.DefaultFakeIPRange.Contains(a) || tunlayer.DefaultFakeIPRange6.Contains(a)
}

func (p leakProbe) newID(ctx context.Context) (string, error) {
	body, err := p.get(ctx, p.base+"/id")
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(body)
	if !leakID.MatchString(id) {
		return "", errors.New("bash.ws: unexpected test id")
	}
	return id, nil
}

// visit requests url and throws the answer away.
func (p leakProbe) visit(ctx context.Context, url string) {
	ctx, cancel := context.WithTimeout(ctx, leakNameTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	if resp, err := p.client.Do(req); err == nil {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
	}
}

// results fetches what bash.ws saw of test id: none of its names looked
// up is an empty list, not an error.
func (p leakProbe) results(ctx context.Context, id string) ([]leakEntry, error) {
	body, err := p.get(ctx, p.base+"/dnsleak/test/"+id+"?json")
	if err != nil {
		return nil, err
	}
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "{") {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(body), &e) == nil && strings.Contains(strings.ToLower(e.Error), "no dns servers") {
			return nil, nil
		}
		return nil, fmt.Errorf("bash.ws: %s", strings.TrimSpace(e.Error))
	}
	var entries []leakEntry
	if err := json.Unmarshal([]byte(body), &entries); err != nil {
		return nil, fmt.Errorf("bash.ws: %w", err)
	}
	return entries, nil
}

func (p leakProbe) get(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "CoreShift")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bash.ws: %s", resp.Status)
	}
	return string(body), nil
}
