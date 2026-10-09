package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/subscription"
	"coreshift/engine/internal/supervisor"
)

// NewAPI serves the desktop UI over HTTP on a loopback address:
//
//	GET  /v1/hello               nothing but the proof below, for the app to
//	                             check whom it talks to before it sends more
//	GET  /v1/status
//	POST /v1/connect            {} for the selected node, {"subscription": id,
//	                             "fingerprint": …, "name": …} to select and connect,
//	                             {"link": "vless://…"} or {"node": {…}} for a one-off
//	POST /v1/reconnect           the last node again, applying changed settings
//	POST /v1/disconnect
//	GET  /v1/events[?replay=1]   server-sent events; with app=1 the stream
//	                             is the app's (Service.AttachApp); with
//	                             view=<id> it is a window's (AttachView)
//	POST /v1/view                {"view": id, "hidden": true}: the window is
//	                             in the tray or minimized (SetViewHidden)
//	POST /v1/subscription/parse  {"url": "…"} or {"content": "…"}, without saving
//	GET  /v1/ip                  the address sites see (IPInfo)
//	GET  /v1/stats?days=30       traffic per day through the VPN (Stats)
//	POST /v1/speedtest           measure the connection (SpeedResult); progress
//	                             arrives as "speedtest" events
//	POST /v1/leaktest            the DNS leak test, through the connection
//	                             (LeakResult)
//
// plus the store endpoints in api_store.go.
//
// Every request needs "Authorization: Bearer <token>". The Host header must
// name the loopback listener, which stops web pages from reaching the API
// through DNS rebinding.
//
// A request with an X-CoreShift-Nonce header gets X-CoreShift-Proof back,
// HMAC-SHA256 of the nonce keyed with the token (APIProof): the service
// proves it knows the token. Whoever took the port of a service that
// crashed, leaving its api.json behind, cannot, and the app stops there.
func NewAPI(svc *Service, token string, listen netip.AddrPort) http.Handler {
	a := &api{svc: svc, token: token, port: fmt.Sprint(listen.Port()), streams: &streams{max: maxStreams}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/hello", a.hello)
	mux.HandleFunc("GET /v1/status", a.status)
	mux.HandleFunc("POST /v1/connect", a.connect)
	mux.HandleFunc("POST /v1/reconnect", a.reconnect)
	mux.HandleFunc("POST /v1/disconnect", a.disconnect)
	mux.HandleFunc("GET /v1/events", a.events)
	mux.HandleFunc("POST /v1/view", a.view)
	mux.HandleFunc("POST /v1/subscription/parse", a.parse)
	mux.HandleFunc("GET /v1/ip", a.publicIP)
	mux.HandleFunc("GET /v1/stats", a.stats)
	mux.HandleFunc("POST /v1/speedtest", a.speedTest)
	mux.HandleFunc("POST /v1/leaktest", a.leakTest)
	a.routeStore(mux)
	return a.guard(mux)
}

type api struct {
	svc     *Service
	token   string
	port    string
	streams *streams
}

// Request bodies: a subscription's content, a link or the settings may be
// large; everything else is a few fields.
const (
	maxBody   = 1 << 20
	smallBody = 64 << 10
)

// largeBodies are the routes that take up to maxBody; the rest smallBody.
var largeBodies = map[string]bool{
	"POST /v1/connect":             true,
	"POST /v1/subscription/parse":  true,
	"POST /v1/subscriptions":       true,
	"PATCH /v1/subscriptions/{id}": true,
	"PUT /v1/settings":             true,
}

// bodyLimit is how large a request body the route of pattern takes.
func bodyLimit(pattern string) int64 {
	if largeBodies[pattern] {
		return maxBody
	}
	return smallBody
}

// The headers of the proof that the service knows the token.
const (
	NonceHeader = "X-CoreShift-Nonce"
	ProofHeader = "X-CoreShift-Proof"
	maxNonce    = 128
)

// APIProof is what the service answers to nonce: hex HMAC-SHA256 of
// "coreshift-api-proof", a zero byte and the nonce, keyed with the token.
func APIProof(token, nonce string) string {
	m := hmac.New(sha256.New, []byte(token))
	m.Write([]byte("coreshift-api-proof\x00"))
	m.Write([]byte(nonce))
	return hex.EncodeToString(m.Sum(nil))
}

func (a *api) guard(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, port, err := net.SplitHostPort(r.Host)
		if err != nil || port != a.port || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
			writeError(w, http.StatusForbidden, errors.New("forbidden host"))
			return
		}
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, []byte("Bearer "+a.token)) != 1 {
			writeError(w, http.StatusUnauthorized, errors.New("missing or wrong token"))
			return
		}
		// Only for whoever holds the token, so it tells nobody anything new.
		if n := r.Header.Get(NonceHeader); n != "" && len(n) <= maxNonce {
			w.Header().Set(ProofHeader, APIProof(a.token, n))
		}
		_, pattern := mux.Handler(r)
		r.Body = http.MaxBytesReader(w, r.Body, bodyLimit(pattern))
		mux.ServeHTTP(w, r)
	})
}

// API server limits. Event streams last as long as the app runs: the read
// timeout is lifted for them (events), and a reader that stopped reading
// is dropped after streamWrite instead.
var apiLimits = struct {
	readHeader, read, idle, streamWrite time.Duration
	maxHeader                           int
}{
	readHeader:  10 * time.Second,
	read:        30 * time.Second,
	idle:        2 * time.Minute,
	streamWrite: 30 * time.Second,
	maxHeader:   64 << 10,
}

// NewAPIServer is the HTTP server for the handler of NewAPI, with limits
// on how long a client may take to send a request or keep an idle
// connection, and on the size of its headers.
func NewAPIServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: apiLimits.readHeader,
		ReadTimeout:       apiLimits.read,
		IdleTimeout:       apiLimits.idle,
		MaxHeaderBytes:    apiLimits.maxHeader,
	}
}

// maxStreams caps the event streams open at once: one per running app,
// a few more for tools.
const maxStreams = 16

// streams are the open event streams, oldest first. A new one beyond max
// ends the oldest rather than being refused: an app that reconnects must
// never be locked out by its own stale stream, which the service would
// notice only at its next ping.
type streams struct {
	mu   sync.Mutex
	max  int
	open []*context.CancelFunc
}

// add registers a stream that cancel ends, ending the oldest when full,
// and returns the function that removes it.
func (s *streams) add(cancel context.CancelFunc) (remove func()) {
	c := &cancel
	s.mu.Lock()
	for len(s.open) >= s.max {
		(*s.open[0])()
		s.open = s.open[1:]
	}
	s.open = append(s.open, c)
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, o := range s.open {
			if o == c {
				s.open = append(s.open[:i], s.open[i+1:]...)
				return
			}
		}
	}
}

func (s *streams) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.open)
}

func (a *api) hello(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": Version})
}

func (a *api) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.svc.Status())
}

func (a *api) connect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Link         string     `json:"link"`
		Node         *node.Node `json:"node"`
		Subscription string     `json:"subscription"`
		Fingerprint  string     `json:"fingerprint"`
		Name         string     `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	var n node.Node
	switch {
	case req.Node != nil:
		n = *req.Node
		if err := n.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	case req.Link != "":
		var err error
		if n, err = subscription.ParseLink(req.Link); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	case a.svc.Store() == nil:
		writeError(w, http.StatusBadRequest, errors.New(`need "link" or "node"`))
		return
	case req.Subscription != "":
		if _, err := a.svc.Store().Select(req.Subscription, req.Fingerprint, req.Name); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	// Not the request context: a UI closing mid-request must not abort the
	// connection half-way.
	ctx, cancel := backgroundCtx(2 * time.Minute)
	defer cancel()
	var err error
	if n.Protocol != "" {
		err = a.svc.Connect(ctx, n)
	} else {
		err = a.svc.ConnectSelected(ctx)
	}
	switch {
	case errors.Is(err, ErrNoSelection):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, ErrDisconnected):
		// The user disconnected meanwhile: that is the state to show.
		writeJSON(w, http.StatusOK, a.svc.Status())
	case err != nil:
		writeError(w, http.StatusBadGateway, err)
	default:
		writeJSON(w, http.StatusOK, a.svc.Status())
	}
}

func (a *api) reconnect(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := backgroundCtx(2 * time.Minute)
	defer cancel()
	if err := a.svc.Reconnect(ctx); err != nil && !errors.Is(err, ErrDisconnected) {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, a.svc.Status())
}

func backgroundCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func (a *api) disconnect(w http.ResponseWriter, r *http.Request) {
	a.svc.Disconnect()
	writeJSON(w, http.StatusOK, a.svc.Status())
}

func (a *api) publicIP(w http.ResponseWriter, r *http.Request) {
	info, err := a.svc.PublicIP(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (a *api) speedTest(w http.ResponseWriter, r *http.Request) {
	res, err := a.svc.SpeedTest(r.Context())
	switch {
	case errors.Is(err, ErrSpeedTestRunning):
		writeError(w, http.StatusConflict, err)
	case err != nil:
		writeError(w, http.StatusBadGateway, err)
	default:
		writeJSON(w, http.StatusOK, res)
	}
}

func (a *api) leakTest(w http.ResponseWriter, r *http.Request) {
	res, err := a.svc.LeakTest(r.Context())
	switch {
	case errors.Is(err, supervisor.ErrNotConnected):
		writeError(w, http.StatusConflict, err)
	case err != nil:
		writeError(w, http.StatusBadGateway, err)
	default:
		writeJSON(w, http.StatusOK, res)
	}
}

func (a *api) stats(w http.ResponseWriter, r *http.Request) {
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil {
		days = 30
	}
	writeJSON(w, http.StatusOK, a.svc.Stats(days))
}

func (a *api) events(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	// The stream is endless: the server's read timeout must not end it.
	if err := rc.SetReadDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer a.streams.add(cancel)()

	// Subscribed before the answer: a client that sees the stream open
	// gets every event from then on.
	events, unsubscribe := a.svc.Subscribe(r.URL.Query().Get("replay") == "1")
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if rc.Flush() != nil {
		return
	}
	if r.URL.Query().Get("app") == "1" {
		defer a.svc.AttachApp()()
	}
	if v := r.URL.Query().Get("view"); validView(v) {
		defer a.svc.AttachView(v)()
	}
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		// A reader that stopped reading is dropped rather than kept forever.
		rc.SetWriteDeadline(time.Now().Add(apiLimits.streamWrite))
		var err error
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			_, err = fmt.Fprint(w, ": ping\n\n")
		case e := <-events:
			b, _ := json.Marshal(e)
			_, err = fmt.Fprintf(w, "data: %s\n\n", b)
		}
		if err != nil || rc.Flush() != nil {
			return
		}
	}
}

func (a *api) view(w http.ResponseWriter, r *http.Request) {
	var req struct {
		View   string `json:"view"`
		Hidden bool   `json:"hidden"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyError(w, "invalid request", err)
		return
	}
	if err := a.svc.SetViewHidden(req.View, req.Hidden); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parsedNode is a node plus what the UI shows next to it.
type parsedNode struct {
	Node        node.Node   `json:"node"`
	Fingerprint string      `json:"fingerprint"`
	Cores       []core.Kind `json:"cores"`
}

func (a *api) parse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL       string `json:"url"`
		Content   string `json:"content"`
		UserAgent string `json:"user_agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyError(w, "invalid request", err)
		return
	}
	var f subscription.Fetched
	var err error
	if req.URL != "" {
		f, err = a.svc.fetchSubscription(r.Context(), req.URL, req.UserAgent)
	} else {
		f.Result, err = subscription.Parse([]byte(req.Content))
	}
	if err != nil && len(f.Nodes) == 0 {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	nodes := make([]parsedNode, len(f.Nodes))
	for i := range f.Nodes {
		n := &f.Nodes[i]
		nodes[i] = parsedNode{Node: *n, Fingerprint: n.Fingerprint(), Cores: a.svc.Compatible(n)}
	}
	var expire int64
	if !f.Info.Expire.IsZero() {
		expire = f.Info.Expire.Unix()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"format": f.Format,
		"info": map[string]any{
			"title": f.Info.Title, "upload": f.Info.Upload, "download": f.Info.Download, "total": f.Info.Total,
			"expire": expire, "update_interval_hours": int(f.Info.UpdateInterval.Hours()),
			"support_url": f.Info.SupportURL, "web_page_url": f.Info.WebPageURL,
		},
		"nodes":   nodes,
		"skipped": f.Skipped,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// writeBodyError reports a request body that could not be read: 413 when
// it is larger than the route takes, else 400.
func writeBodyError(w http.ResponseWriter, what string, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("%s: the body is larger than %d bytes", what, tooLarge.Limit))
		return
	}
	writeError(w, http.StatusBadRequest, fmt.Errorf("%s: %w", what, err))
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": strings.TrimSpace(err.Error())})
}
