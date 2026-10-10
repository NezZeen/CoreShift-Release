package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"coreshift/engine/internal/msg"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/supervisor"
)

// Users write "it doesn't work" and nothing more. The checkup answers what
// is wrong in one go, from the bottom up: whether the device has a network,
// whether the internet answers around the tunnel, whether the network's DNS
// does, whether the server's name resolves and its port answers, and while
// connected whether traffic and DNS go through the tunnel, whether DNS
// leaks, how fast it is and whether direct connections get through. The
// steps run at once where they do not depend on each other, each reports as
// it finishes ("checkup" events), and the verdict names the most likely
// cause and what to do. It reuses the probes of the rest of the service:
// those of checkReach, of the latency test, of the leak test and of the
// speed test. Nothing in it names a server but by its display name, nor any
// subscription: the result goes to support.

// checkupTimeout bounds a whole checkup.
const checkupTimeout = 30 * time.Second

// The steps' own bounds, within checkupTimeout.
const (
	checkupInternetFor = 8 * time.Second
	checkupDNSFor      = 6 * time.Second
	checkupServerFor   = 12 * time.Second
	checkupLookupFor   = 5 * time.Second
	checkupTunnelFor   = 10 * time.Second
	checkupLeakFor     = 25 * time.Second
	// checkupSlow is the delay through the tunnel that reads as slow.
	checkupSlow = 1500 * time.Millisecond
	// checkupSlowBps: below a megabit a second pages load for ages.
	checkupSlowBps = 125_000
)

// The speed sample: one download of up to speedSampleBytes for up to
// speedSampleFor, a few megabytes rather than the speed test's dozens.
var (
	speedSampleFor   = 5 * time.Second
	speedSampleBytes = 10 << 20
)

// ErrCheckupRunning means another checkup has not finished yet.
var ErrCheckupRunning = errors.New("a checkup is already running")

// How a step went.
const (
	CheckOK      = "ok"
	CheckWarn    = "warn"
	CheckFail    = "fail"
	CheckSkipped = "skipped"
)

// The steps, in the order they are shown.
const (
	stepNetwork   = "network"
	stepInternet  = "internet"
	stepDNS       = "dns"
	stepServer    = "server"
	stepTunnel    = "tunnel"
	stepTunnelDNS = "tunnel-dns"
	stepLeak      = "leak"
	stepSpeed     = "speed"
	stepDirect    = "direct"
)

var checkupOrder = []string{stepNetwork, stepInternet, stepDNS, stepServer, stepTunnel, stepTunnelDNS, stepLeak, stepSpeed, stepDirect}

// checkupTitle is a step's name in Russian; the app has its own,
// "checkup.step." and the id.
func checkupTitle(id string) string { return msg.New("checkup.step." + id).String() }

// What the actions of a verdict do, in the app.
const (
	actionServers   = "servers"   // pick another server
	actionReconnect = "reconnect" // reconnect
	actionConnect   = "connect"   // connect
	actionRouting   = "routing"   // send everything through the VPN
	actionLeak      = "leak"      // the leak test, with its advice
)

// CheckStep is one step of a checkup.
type CheckStep struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Status is CheckOK, CheckWarn, CheckFail or CheckSkipped.
	Status string `json:"status"`
	// Detail says what was seen, in Russian, for the user and support;
	// Code and Args say it in the app's language (internal/msg).
	Detail      string         `json:"detail"`
	Code        string         `json:"code,omitempty"`
	Args        map[string]any `json:"args,omitempty"`
	LatencyMS   int64          `json:"latency_ms,omitempty"`
	DownloadBps int64          `json:"download_bps,omitempty"`
	// cause tells failures of one step apart for the verdict: the server's
	// "resolve" or "port", "none" when no server is selected.
	cause string
}

// detail is what the step says.
func (c CheckStep) detail() msg.Msg { return msg.Msg{Code: c.Code, Args: c.Args} }

// CheckVerdict is what a checkup found: the most likely cause, in words,
// and what the app offers to do about it.
type CheckVerdict struct {
	// Cause: no-network, offline, server-dns, server-down, server-blocked,
	// tunnel, tunnel-dns, direct-blocked, dns, leak, slow, whitelist,
	// no-server, ready, connect-failed or ok.
	Cause string `json:"cause"`
	// Status is CheckOK, CheckWarn or CheckFail.
	Status string `json:"status"`
	// Title and Advice in Russian; Code+".title" and Code+".advice" with
	// Args say them in the app's language (internal/msg).
	Title  string         `json:"title"`
	Advice string         `json:"advice"`
	Code   string         `json:"code,omitempty"`
	Args   map[string]any `json:"args,omitempty"`
	// Actions are the app's buttons, the first the main one: "servers",
	// "reconnect", "connect", "routing", "leak".
	Actions []string `json:"actions,omitempty"`
}

// CheckupResult is a whole checkup.
type CheckupResult struct {
	// State is the connection's when the checkup started; Server the
	// display name of its server, or of the selected one.
	State      State        `json:"state"`
	Server     string       `json:"server,omitempty"`
	Steps      []CheckStep  `json:"steps"`
	Verdict    CheckVerdict `json:"verdict"`
	DurationMS int64        `json:"duration_ms"`
}

// CheckupOptions choose the optional steps.
type CheckupOptions struct {
	// Speed adds a short sample of the download speed.
	Speed bool `json:"speed"`
}

// Checkup runs every check at once and says what is wrong; see the top of
// this file. ctx cancels it.
func (s *Service) Checkup(ctx context.Context, o CheckupOptions) (CheckupResult, error) {
	if !s.checkupMu.TryLock() {
		return CheckupResult{}, ErrCheckupRunning
	}
	defer s.checkupMu.Unlock()
	start := time.Now()
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, checkupTimeout)
	defer cancel()

	st := s.Status()
	connected := st.State == Connected
	s.mu.Lock()
	n, hasNode, serverIP, routes := s.lastNode, s.hasLast, s.serverIP, directRoutes(s.connOpts)
	s.mu.Unlock()
	if !connected {
		// The server the next connection would take, and its settings.
		n, hasNode, serverIP, routes = node.Node{}, false, netip.Addr{}, directRoutes(s.Options())
		if s.cfg.Store != nil {
			if _, sel, ok := s.cfg.Store.Selected(); ok {
				n, hasNode = sel, true
			}
		}
	}
	ids := slices.DeleteFunc(slices.Clone(checkupOrder), func(id string) bool { return id == stepSpeed && !o.Speed })
	s.hub.publish(Event{Kind: "checkup", Reason: "started", Line: strings.Join(ids, ",")})

	var mu sync.Mutex
	steps := map[string]CheckStep{}
	done := func(id string, step CheckStep) CheckStep {
		step.ID, step.Title = id, checkupTitle(id)
		mu.Lock()
		steps[id] = step
		mu.Unlock()
		s.hub.publish(Event{Kind: "checkup", Reason: "step", Step: id, Status: step.Status, LatencyMS: step.LatencyMS}.withLine(step.detail()))
		return step
	}

	var wg sync.WaitGroup
	var inet internetSeen
	inetDone, tunnelDone := make(chan struct{}), make(chan struct{})
	var tunnel CheckStep
	wg.Go(func() { done(stepNetwork, s.checkNetwork()) })
	wg.Go(func() {
		defer close(inetDone)
		var step CheckStep
		step, inet = s.checkInternet(ctx, connected)
		done(stepInternet, step)
	})
	wg.Go(func() { done(stepDNS, s.checkDNS(ctx)) })
	wg.Go(func() { done(stepServer, s.checkServer(ctx, n, hasNode, serverIP)) })
	wg.Go(func() {
		defer close(tunnelDone)
		tunnel = done(stepTunnel, s.checkTunnel(ctx, st))
	})
	wg.Go(func() { done(stepTunnelDNS, s.checkTunnelDNS(ctx, st)) })
	wg.Go(func() { done(stepLeak, s.checkLeak(ctx, st)) })
	if o.Speed {
		// After the delay is measured, so the download does not swell it.
		wg.Go(func() {
			<-tunnelDone
			done(stepSpeed, s.checkSpeed(ctx, st))
		})
	}
	wg.Go(func() {
		<-inetDone
		<-tunnelDone
		done(stepDirect, s.checkDirect(connected, s.Status().DirectBlocked, routes, inet, tunnel))
	})
	wg.Wait()
	if err := parent.Err(); err != nil {
		return CheckupResult{}, err
	}

	res := CheckupResult{State: st.State, Server: n.Name, DurationMS: time.Since(start).Milliseconds()}
	for _, id := range ids {
		res.Steps = append(res.Steps, steps[id])
	}
	res.Verdict = checkupVerdict(st.State, n.Name, steps, routes)
	s.hub.publish(Event{Kind: "checkup", Reason: "done", Status: res.Verdict.Status}.withLine(msg.Msg{Code: res.Verdict.Code + ".title", Args: res.Verdict.Args}))
	return res, nil
}

func (a *api) checkup(w http.ResponseWriter, r *http.Request) {
	var o CheckupOptions
	if !decode(w, r, &o) {
		return
	}
	// The request's context: a UI that goes away ends the checkup.
	res, err := a.svc.Checkup(r.Context(), o)
	switch {
	case errors.Is(err, ErrCheckupRunning):
		writeError(w, http.StatusConflict, err)
	case err != nil:
		writeError(w, http.StatusBadGateway, err)
	default:
		writeJSON(w, http.StatusOK, res)
	}
}

// step is a step of status that says code with its arguments.
func step(status, code string, kv ...any) CheckStep {
	m := msg.New(code, kv...)
	return CheckStep{Status: status, Detail: m.String(), Code: m.Code, Args: m.Args}
}

func okStep(code string, kv ...any) CheckStep   { return step(CheckOK, code, kv...) }
func warnStep(code string, kv ...any) CheckStep { return step(CheckWarn, code, kv...) }
func failStep(code string, kv ...any) CheckStep { return step(CheckFail, code, kv...) }
func skipStep(code string, kv ...any) CheckStep { return step(CheckSkipped, code, kv...) }

const notConnected = "checkup.not_connected"

// checkNetwork: the device has a network at all (netwatch.go).
func (s *Service) checkNetwork() CheckStep {
	if s.noNetwork() {
		return failStep("checkup.network.none")
	}
	if name := s.cfg.netName(); name != "" {
		return okStep("checkup.network.ok_named", "net", networkLabel(name))
	}
	return okStep("checkup.network.ok")
}

// reachNames name the well-known hosts of checkReach.
var reachNames = map[netip.AddrPort]msg.Msg{
	netip.MustParseAddrPort("1.1.1.1:443"):   msg.Raw("Cloudflare"),
	netip.MustParseAddrPort("8.8.8.8:443"):   msg.Raw("Google"),
	netip.MustParseAddrPort("77.88.8.8:443"): msg.New("checkup.host.yandex"),
}

// reachHome is the host of reachHosts that answers where nothing foreign
// does: answering alone, it suggests a network with a white list.
var reachHome = netip.MustParseAddrPort("77.88.8.8:443")

// internetSeen is what checkInternet saw, for the step on direct
// connections.
type internetSeen struct {
	known, online, onlyHome bool
}

// checkInternet: the well-known hosts of checkReach answer a handshake on
// 443 from the physical network, around the tunnel.
func (s *Service) checkInternet(ctx context.Context, connected bool) (CheckStep, internetSeen) {
	ctx, cancel := context.WithTimeout(ctx, checkupInternetFor)
	defer cancel()
	bind, err := s.cfg.physical()
	if err != nil {
		// Without the tunnel the default route is the network's anyway.
		if connected && !s.cfg.AppOutsideVPN {
			return skipStep("checkup.no_bypass"), internetSeen{}
		}
		bind = ping.Bind{}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var answered []netip.AddrPort
	var best time.Duration
	var errs []error
	for _, h := range reachHosts {
		wg.Go(func() {
			rtt, err := s.cfg.tcpPing(ctx, h, bindFor(bind, h.Addr()))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", h, err))
				return
			}
			answered = append(answered, h)
			if best == 0 || rtt < best {
				best = rtt
			}
		})
	}
	wg.Wait()
	seen := internetSeen{known: true, online: len(answered) > 0}
	if !seen.online {
		return failStep("checkup.internet.none", "errs", netErrsText(errs)), seen
	}
	var names []msg.Msg
	for _, h := range reachHosts {
		if slices.Contains(answered, h) {
			names = append(names, reachNames[h])
		}
	}
	st := okStep("checkup.internet.ok", "hosts", msg.Join(", ", names...), "ms", latencyMS(best))
	if len(answered) == 1 && answered[0] == reachHome {
		seen.onlyHome = true
		st = warnStep("checkup.internet.only_home", "ms", latencyMS(best))
	}
	st.LatencyMS = latencyMS(best)
	return st, seen
}

// checkupNames are looked up to test DNS: one foreign, one Russian, so a
// network that answers only for its own country still counts as working.
var checkupNames = []string{"www.google.com", "ya.ru"}

// checkDNS: the network's DNS finds addresses, as the service's own lookups
// of servers ask it (serverAddr): the system's resolver, or while
// connected on the desktop the TUN layer's resolver for direct names.
func (s *Service) checkDNS(ctx context.Context) CheckStep {
	ctx, cancel := context.WithTimeout(ctx, checkupDNSFor)
	defer cancel()
	type result struct {
		rtt time.Duration
		err error
	}
	results := make(chan result, len(checkupNames))
	for _, name := range checkupNames {
		go func() {
			t := time.Now()
			_, err := s.serverAddr(ctx, name)
			results <- result{time.Since(t), err}
		}()
	}
	var firstErr error
	for range checkupNames {
		r := <-results
		if r.err == nil {
			st := okStep("checkup.dns.ok", "ms", latencyMS(r.rtt))
			st.LatencyMS = latencyMS(r.rtt)
			return st
		}
		if firstErr == nil {
			firstErr = r.err
		}
	}
	return failStep("checkup.dns.fail", "err", dnsErrText(firstErr))
}

// dnsErrText says in a few words why a lookup failed.
func dnsErrText(err error) msg.Msg {
	var de *net.DNSError
	switch {
	case errors.As(err, &de) && de.IsNotFound:
		return msg.New("dns.not_found")
	case errors.As(err, &de) && de.IsTimeout:
		return msg.New("net.timeout")
	}
	var re *resolveError
	if errors.As(err, &re) {
		err = re.err // its own text names the server
	}
	return netErrText(err)
}

// quoted is a server's display name in quotes, or "сервер" without one.
func quoted(name string) msg.Msg {
	if name == "" {
		return msg.New("checkup.who_none")
	}
	return msg.New("checkup.who", "name", name)
}

// checkServer: the server's name resolves, as for connecting and, failing
// that, over DNS over HTTPS as the latency test does; and its port answers
// a handshake, or its address a ping over UDP. known is its address while
// connected.
func (s *Service) checkServer(ctx context.Context, n node.Node, has bool, known netip.Addr) CheckStep {
	if !has {
		st := skipStep("checkup.server.none")
		st.cause = "none"
		return st
	}
	ctx, cancel := context.WithTimeout(ctx, checkupServerFor)
	defer cancel()
	who := quoted(n.Name)
	bind, err := s.cfg.physical()
	if err != nil {
		bind = ping.Bind{}
	}
	ip, viaDoH := known, false
	if a, err := netip.ParseAddr(n.Server); err == nil {
		ip = a
	}
	if !ip.IsValid() {
		lctx, lcancel := context.WithTimeout(ctx, checkupLookupFor)
		a, err := s.serverAddr(lctx, n.Server)
		lcancel()
		if err != nil {
			ips, derr := s.cfg.dohLookup(ctx, n.Server, bindFor(bind, dohProbe))
			if a, derr = preferIPv4(ips, derr); derr != nil {
				st := failStep("checkup.server.resolve_fail", "who", who, "err", dnsErrText(err))
				st.cause = "resolve"
				return st
			}
			viaDoH = true
		}
		ip = a
	}
	if overUDP(&n) {
		rtt, err := checkPing(ip)(s.cfg.icmpPing(ctx, ip, bindFor(bind, ip)))
		switch {
		case errors.Is(err, ping.ErrUnsupported):
			return skipStep("checkup.server.udp_no_ping", "who", who)
		case errors.Is(err, errLocalAnswer):
			return warnStep("checkup.server.local_answer", "who", who)
		case err != nil:
			return warnStep("checkup.server.udp_silent", "who", who, "err", netErrText(err))
		}
		return serverAnswered(who, msg.New("checkup.server.ping_ok"), rtt, viaDoH)
	}
	rtt, err := checkPing(ip)(s.cfg.tcpPing(ctx, netip.AddrPortFrom(ip, n.Port), bindFor(bind, ip)))
	switch {
	case errors.Is(err, errLocalAnswer):
		return warnStep("checkup.server.local_answer", "who", who)
	case err != nil:
		st := failStep("checkup.server.port_fail", "who", who, "err", netErrText(err))
		st.cause = "port"
		return st
	}
	return serverAnswered(who, msg.New("checkup.server.port_ok"), rtt, viaDoH)
}

// serverAnswered: a warning when only DNS over HTTPS found the address.
func serverAnswered(who, what msg.Msg, rtt time.Duration, viaDoH bool) CheckStep {
	st := okStep("checkup.server.ok", "who", who, "what", what, "ms", latencyMS(rtt))
	if viaDoH {
		st = warnStep("checkup.server.ok_doh", "who", who, "what", what, "ms", latencyMS(rtt))
	}
	st.LatencyMS = latencyMS(rtt)
	return st
}

// checkupHealthURLs are fetched through the tunnel: the settings' check,
// then the supervisor's fallbacks, of other companies.
func checkupHealthURLs(primary string) []string {
	urls := []string{}
	for _, u := range []string{primary, "http://cp.cloudflare.com/generate_204", "http://www.gstatic.com/generate_204"} {
		if u != "" && !slices.Contains(urls, u) {
			urls = append(urls, u)
		}
	}
	return urls
}

// checkTunnel: traffic gets through the active core, as its health check
// does, and how long it takes.
func (s *Service) checkTunnel(ctx context.Context, st Status) CheckStep {
	if st.State != Connected {
		return skipStep(notConnected)
	}
	ctx, cancel := context.WithTimeout(ctx, checkupTunnelFor)
	defer cancel()
	s.mu.Lock()
	primary := s.connOpts.Health.URL
	s.mu.Unlock()
	lat, err := s.throughTunnel(ctx, checkupHealthURLs(primary))
	if err != nil {
		return failStep("checkup.tunnel.fail", "err", netErrText(err))
	}
	ms := latencyMS(lat)
	out := okStep("checkup.tunnel.ok", "ms", ms)
	if lat > checkupSlow {
		out = warnStep("checkup.tunnel.slow", "ms", ms)
	}
	out.LatencyMS = ms
	return out
}

// throughTunnel fetches the urls through the core's SOCKS inbound, all at
// once, and returns the first that answered: the time of a second request
// over the connection the first set up, as delays through a proxy are
// told (the protocol's handshake is not the delay).
func (s *Service) throughTunnel(ctx context.Context, urls []string) (time.Duration, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		lat time.Duration
		err error
	}
	results := make(chan result, len(urls))
	for _, u := range urls {
		go func() {
			// The core's SOCKS inbound, credentials included: never print it.
			tr := newTransport(s.proxyURL())
			defer tr.CloseIdleConnections()
			client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			first, err := timedGet(ctx, client, u)
			if err != nil {
				results <- result{0, err}
				return
			}
			if second, err := timedGet(ctx, client, u); err == nil {
				first = second
			}
			results <- result{first, nil}
		}()
	}
	var firstErr error
	for range urls {
		r := <-results
		if r.err == nil {
			return r.lat, nil
		}
		if firstErr == nil {
			firstErr = r.err
		}
	}
	return 0, firstErr
}

// timedGet requests u and times it; any answer but 200 or 204 fails. The
// error is the transport's own, without the URL.
func timedGet(ctx context.Context, client *http.Client, u string) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	lat := time.Since(start)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return 0, msg.Err("net.status", "status", resp.Status)
	}
	return lat, nil
}

// checkupTunnelName is looked up through the tunnel.
const checkupTunnelName = "cp.cloudflare.com"

// checkTunnelDNS: apps' lookups get answers through the TUN layer's DNS,
// as the leak test asks it.
func (s *Service) checkTunnelDNS(ctx context.Context, st Status) CheckStep {
	switch {
	case st.State != Connected:
		return skipStep(notConnected)
	case !st.TUN:
		return skipStep("checkup.tunnel_dns.proxy")
	}
	ctx, cancel := context.WithTimeout(ctx, checkupDNSFor)
	defer cancel()
	lookup := s.cfg.TUNLookup
	if lookup == nil {
		lookup = systemLookup
	}
	t := time.Now()
	addrs, err := lookup(ctx, checkupTunnelName)
	rtt := time.Since(t)
	if err == nil && len(addrs) == 0 {
		err = msg.Err("net.empty_answer")
	}
	if err != nil {
		return failStep("checkup.tunnel_dns.fail", "err", dnsErrText(err))
	}
	fake := true
	for _, a := range addrs {
		fake = fake && isFakeIP(a)
	}
	out := okStep("checkup.tunnel_dns.ok", "ms", latencyMS(rtt))
	if fake {
		out = okStep("checkup.tunnel_dns.fakeip")
	}
	out.LatencyMS = latencyMS(rtt)
	return out
}

// checkLeak runs the DNS leak test and sums it up as the app does.
func (s *Service) checkLeak(ctx context.Context, st Status) CheckStep {
	if st.State != Connected {
		return skipStep(notConnected)
	}
	ctx, cancel := context.WithTimeout(ctx, checkupLeakFor)
	defer cancel()
	res, err := s.LeakTest(ctx)
	if err != nil {
		if errors.Is(err, supervisor.ErrNotConnected) {
			return skipStep(notConnected)
		}
		return warnStep("checkup.leak.no_service")
	}
	switch v, isp := leakVerdict(res); v {
	case leakOK:
		return okStep("checkup.leak.ok")
	case leakFound:
		if isp {
			return failStep("checkup.leak.isp")
		}
		return failStep("checkup.leak.foreign")
	case leakBypass:
		return warnStep("checkup.leak.bypass")
	default:
		return warnStep("checkup.leak.unknown")
	}
}

// The verdicts of a leak test, as the app tells them (app/lib/state/leak.dart).
const (
	leakOK      = "ok"
	leakFound   = "leak"
	leakBypass  = "bypass"
	leakUnknown = "unknown"
)

// leakPublic are public resolvers, which answer from near wherever the
// query left: not the ISP's.
var leakPublic = []string{"google", "cloudflare", "quad9", "opendns", "cisco", "adguard", "nextdns", "cleanbrowsing", "control d", "mullvad"}

// leakVerdict sums up a leak test as LeakReport.verdict does in the app;
// isp tells that a suspicious resolver is in the device's own country.
func leakVerdict(r LeakResult) (verdict string, isp bool) {
	if r.Exit == nil {
		return leakUnknown, false
	}
	home := ""
	if r.Home != nil {
		home = strings.ToLower(r.Home.Country)
		if r.Home.IP != "" {
			e, h := strings.Split(r.Exit.IP, "."), strings.Split(r.Home.IP, ".")
			if r.Exit.IP == r.Home.IP || (len(e) == 4 && len(h) == 4 && slices.Equal(e[:3], h[:3])) {
				return leakBypass, false
			}
		}
	}
	exit := r.Exit.Country
	suspicious := false
	for _, d := range r.DNS {
		org := strings.ToLower(d.Org)
		if d.Country == "" || slices.ContainsFunc(leakPublic, func(p string) bool { return strings.Contains(org, p) }) {
			continue
		}
		switch {
		case home != "" && d.Country == home && exit != home:
			suspicious, isp = true, true
		case d.Path != "proxy" && exit != "" && d.Country != exit:
			suspicious = true
		}
	}
	switch {
	case suspicious:
		return leakFound, isp
	case r.Exit.IP != r.ServerIP && r.Home == nil:
		return leakUnknown, false
	}
	return leakOK, false
}

// checkSpeed takes a short sample of the download: through the tunnel while
// connected, else of the network itself.
func (s *Service) checkSpeed(ctx context.Context, st Status) CheckStep {
	if !s.speedMu.TryLock() {
		return skipStep("checkup.speed.busy")
	}
	defer s.speedMu.Unlock()
	var proxy *url.URL
	if st.State == Connected {
		// The core's SOCKS inbound, credentials included: never print it.
		proxy = s.proxyURL()
	}
	tr := newTransport(proxy)
	tr.ForceAttemptHTTP2 = false
	defer tr.CloseIdleConnections()
	base := s.cfg.speedURL
	if base == "" {
		base = speedServer
	}
	ctx, cancel := context.WithTimeout(ctx, speedSampleFor)
	defer cancel()
	var moved atomic.Int64
	start := time.Now()
	err := speedSample(ctx, &http.Client{Transport: tr}, base, &moved)
	elapsed := max(time.Since(start), time.Millisecond)
	if moved.Load() == 0 {
		if err == nil {
			err = msg.Err("net.nothing_downloaded")
		}
		return warnStep("checkup.speed.fail", "err", netErrText(err))
	}
	bps := int64(float64(moved.Load()) / elapsed.Seconds())
	mbit := fmt.Sprintf("%.1f", float64(bps)*8/1e6)
	if bps*8 >= 100e6 {
		mbit = fmt.Sprintf("%.0f", float64(bps)*8/1e6)
	}
	out := okStep("checkup.speed.ok", "mbit", mbit)
	if bps < checkupSlowBps {
		out = warnStep("checkup.speed.slow", "mbit", mbit)
	}
	out.DownloadBps = bps
	return out
}

// speedSample downloads up to speedSampleBytes from the speed test's server,
// counting into moved, until done or ctx ends; an end by ctx is no error.
func speedSample(ctx context.Context, client *http.Client, base string, moved *atomic.Int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/__down?bytes=%d", base, speedSampleBytes), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return msg.Err("net.status", "status", resp.Status)
	}
	_, err = io.Copy(io.Discard, &countingReader{r: resp.Body, n: moved})
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// checkDirect: direct connections get through the network while the tunnel
// works. The engine's own count of refused direct connections tells
// (direct.go, blocked), and so does the internet not answering around a
// tunnel that works, or only Yandex answering: a white list.
func (s *Service) checkDirect(connected, blocked bool, routes []string, inet internetSeen, tunnel CheckStep) CheckStep {
	works := tunnel.Status == CheckOK || tunnel.Status == CheckWarn
	switch {
	case !connected && inet.onlyHome:
		return warnStep("checkup.direct.whitelist")
	case !connected && inet.known && !inet.online:
		return skipStep("checkup.direct.offline")
	case !connected:
		return okStep("checkup.direct.ok")
	case len(routes) == 0:
		return okStep("checkup.direct.unused")
	case blocked:
		return failStep("checkup.direct.blocked")
	case works && inet.known && !inet.online:
		return failStep("checkup.direct.whitelist_vpn")
	case works && inet.onlyHome:
		return warnStep("checkup.direct.whitelist")
	case !inet.known:
		return skipStep("checkup.no_bypass")
	}
	return okStep("checkup.direct.ok")
}

// checkupVerdict names the most likely cause of what the steps found, from
// the bottom up: the device's network, the internet, the server, the
// tunnel, then what goes wrong with a tunnel that works. state is the
// connection's, server the display name of its server.
func checkupVerdict(state State, server string, steps map[string]CheckStep, routes []string) CheckVerdict {
	is := func(id, status string) bool { return steps[id].Status == status }
	connected := state == Connected
	works := connected && (is(stepTunnel, CheckOK) || is(stepTunnel, CheckWarn))
	verdict := pickVerdict(state, steps, routes, connected, works, is)
	if verdict.Args == nil {
		verdict.Args = map[string]any{}
	}
	// The server's name for the titles that name it, what to change for the
	// advice on direct connections.
	verdict.Args["srv"] = msg.New("checkup.srv_none")
	if server != "" {
		verdict.Args["srv"] = msg.New("checkup.srv", "name", server)
	}
	verdict.Args["advice"] = directAdvice(routes)
	verdict.Title = msg.Msg{Code: verdict.Code + ".title", Args: verdict.Args}.String()
	verdict.Advice = msg.Msg{Code: verdict.Code + ".advice", Args: verdict.Args}.String()
	return verdict
}

// pickVerdict is checkupVerdict's choice: the cause, how bad, the code of
// the words ("checkup.verdict." and a name) and the actions.
func pickVerdict(state State, steps map[string]CheckStep, routes []string, connected, works bool, is func(id, status string) bool) CheckVerdict {
	v := func(cause, status, code string, actions ...string) CheckVerdict {
		return CheckVerdict{Cause: cause, Status: status, Code: "checkup.verdict." + code, Actions: actions}
	}
	servers := []string{actionServers}
	if connected {
		servers = append(servers, actionReconnect)
	}
	switch {
	case is(stepNetwork, CheckFail):
		return v("no-network", CheckFail, "no_network")
	case works:
		switch {
		case is(stepTunnelDNS, CheckFail):
			return v("tunnel-dns", CheckFail, "tunnel_dns", actionReconnect, actionServers)
		case is(stepDirect, CheckFail):
			return v("direct-blocked", CheckWarn, "direct_blocked", actionRouting)
		case is(stepDNS, CheckFail) && len(routes) > 0:
			return v("dns", CheckWarn, "dns_direct", actionRouting)
		case is(stepLeak, CheckFail):
			return v("leak", CheckWarn, "leak", actionLeak)
		case is(stepDirect, CheckWarn):
			return v("whitelist", CheckWarn, "whitelist_vpn", actionRouting)
		case is(stepTunnel, CheckWarn) || is(stepSpeed, CheckWarn):
			return v("slow", CheckWarn, "slow", actionServers)
		}
		return v("ok", CheckOK, "ok")
	case is(stepInternet, CheckFail):
		return v("offline", CheckFail, "offline")
	case is(stepServer, CheckFail) && steps[stepServer].cause == "resolve":
		return v("server-dns", CheckFail, "server_dns", servers...)
	case is(stepServer, CheckFail):
		return v("server-down", CheckFail, "server_down", servers...)
	case connected && is(stepServer, CheckOK):
		return v("server-blocked", CheckFail, "server_blocked", actionServers, actionReconnect)
	case connected:
		return v("tunnel", CheckFail, "tunnel", actionReconnect, actionServers)
	case is(stepDNS, CheckFail):
		return v("dns", CheckFail, "dns", actionConnect)
	case is(stepServer, CheckSkipped) && steps[stepServer].cause == "none":
		return v("no-server", CheckWarn, "no_server", actionServers)
	case is(stepDirect, CheckWarn):
		return v("whitelist", CheckWarn, "whitelist", actionConnect, actionServers)
	case state == Failed:
		return v("connect-failed", CheckWarn, "connect_failed", actionConnect, actionServers)
	}
	return v("ready", CheckOK, "ready", actionConnect)
}
