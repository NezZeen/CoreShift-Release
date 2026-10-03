package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/subscription"
)

// NewAPI serves the desktop UI over HTTP on a loopback address:
//
//	GET  /v1/status
//	POST /v1/connect             {} for the selected node, {"subscription": id,
//	                             "fingerprint": …, "name": …} to select and connect,
//	                             {"link": "vless://…"} or {"node": {…}} for a one-off
//	POST /v1/reconnect           the last node again, applying changed settings
//	POST /v1/disconnect
//	GET  /v1/events[?replay=1]   server-sent events; with app=1 the stream
//	                             is the app's (Service.AttachApp)
//	POST /v1/subscription/parse  {"url": "…"} or {"content": "…"}, without saving
//	GET  /v1/ip                  the address sites see (IPInfo)
//	GET  /v1/stats?days=30       traffic per day through the VPN (Stats)
//	POST /v1/speedtest           measure the connection (SpeedResult); progress
//	                             arrives as "speedtest" events
//
// plus the store endpoints in api_store.go.
//
// Every request needs "Authorization: Bearer <token>". The Host header must
// name the loopback listener, which stops web pages from reaching the API
// through DNS rebinding.
func NewAPI(svc *Service, token string, listen netip.AddrPort) http.Handler {
	a := &api{svc: svc, token: token, port: fmt.Sprint(listen.Port())}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", a.status)
	mux.HandleFunc("POST /v1/connect", a.connect)
	mux.HandleFunc("POST /v1/reconnect", a.reconnect)
	mux.HandleFunc("POST /v1/disconnect", a.disconnect)
	mux.HandleFunc("GET /v1/events", a.events)
	mux.HandleFunc("POST /v1/subscription/parse", a.parse)
	mux.HandleFunc("GET /v1/ip", a.publicIP)
	mux.HandleFunc("GET /v1/stats", a.stats)
	mux.HandleFunc("POST /v1/speedtest", a.speedTest)
	a.routeStore(mux)
	return a.guard(mux)
}

type api struct {
	svc   *Service
	token string
	port  string
}

const maxBody = 1 << 20

func (a *api) guard(next http.Handler) http.Handler {
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
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r)
	})
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

func (a *api) stats(w http.ResponseWriter, r *http.Request) {
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil {
		days = 30
	}
	writeJSON(w, http.StatusOK, a.svc.Stats(days))
}

func (a *api) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	events, unsubscribe := a.svc.Subscribe(r.URL.Query().Get("replay") == "1")
	defer unsubscribe()
	if r.URL.Query().Get("app") == "1" {
		defer a.svc.AttachApp()()
	}
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		case e := <-events:
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		flusher.Flush()
	}
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
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request: %w", err))
		return
	}
	var f subscription.Fetched
	var err error
	if req.URL != "" {
		f, err = subscription.Fetch(r.Context(), &http.Client{Timeout: 30 * time.Second}, req.URL, req.UserAgent)
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

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": strings.TrimSpace(err.Error())})
}
