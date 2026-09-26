package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"coreshift/engine/internal/apps"
	"coreshift/engine/internal/core"
	"coreshift/engine/internal/node"
	"coreshift/engine/internal/store"
)

// Store endpoints:
//
//	GET    /v1/info                            version, installed cores, TUN availability
//	GET    /v1/settings
//	PUT    /v1/settings                        the whole settings object
//	GET    /v1/subscriptions
//	POST   /v1/subscriptions                   {"url": …} or {"content": …}, optional "name", "user_agent"
//	GET    /v1/subscriptions/{id}
//	PATCH  /v1/subscriptions/{id}              {"name"?, "url"?, "user_agent"?, "content"?}
//	DELETE /v1/subscriptions/{id}
//	POST   /v1/subscriptions/{id}/refresh
//	POST   /v1/subscriptions/{id}/move         {"index": n}
//	GET    /v1/selection
//	PUT    /v1/selection                       {"subscription": id, "fingerprint": …, "name": …}
//	POST   /v1/latency                         {"subscription": id} or {} for all; results also arrive as events
//	GET    /v1/apps                            running programs, for routing apps around the tunnel
//	POST   /v1/cores/return                    move back to the primary core now
//	GET    /v1/cores/updates                   the latest release of each installed core
//	POST   /v1/cores/{kind}/update             install the latest release of a core
func (a *api) routeStore(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/apps", a.runningApps)
	mux.HandleFunc("POST /v1/cores/return", a.returnToPrimary)
	mux.HandleFunc("GET /v1/cores/updates", a.coreUpdates)
	mux.HandleFunc("POST /v1/cores/{kind}/update", a.updateCore)
	mux.HandleFunc("GET /v1/info", a.info)
	mux.HandleFunc("GET /v1/settings", a.withStore(a.getSettings))
	mux.HandleFunc("PUT /v1/settings", a.withStore(a.putSettings))
	mux.HandleFunc("GET /v1/subscriptions", a.withStore(a.listSubscriptions))
	mux.HandleFunc("POST /v1/subscriptions", a.withStore(a.addSubscription))
	mux.HandleFunc("GET /v1/subscriptions/{id}", a.withStore(a.getSubscription))
	mux.HandleFunc("PATCH /v1/subscriptions/{id}", a.withStore(a.editSubscription))
	mux.HandleFunc("DELETE /v1/subscriptions/{id}", a.withStore(a.removeSubscription))
	mux.HandleFunc("POST /v1/subscriptions/{id}/refresh", a.withStore(a.refreshSubscription))
	mux.HandleFunc("POST /v1/subscriptions/{id}/move", a.withStore(a.moveSubscription))
	mux.HandleFunc("GET /v1/selection", a.withStore(a.getSelection))
	mux.HandleFunc("PUT /v1/selection", a.withStore(a.putSelection))
	mux.HandleFunc("POST /v1/latency", a.withStore(a.testLatency))
}

func (a *api) withStore(h func(http.ResponseWriter, *http.Request, *store.Store)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := a.svc.Store()
		if st == nil {
			writeError(w, http.StatusServiceUnavailable, errors.New("the daemon runs without a store"))
			return
		}
		h(w, r, st)
	}
}

type coreInfo struct {
	Kind      core.Kind `json:"kind"`
	Installed bool      `json:"installed"`
	Version   string    `json:"version,omitempty"`
	// Features is what the core supports ("protocol:vless", "transport:ws",
	// "reality"…), for the compatibility table.
	Features []core.Feature `json:"features"`
}

func (a *api) info(w http.ResponseWriter, r *http.Request) {
	var cores []coreInfo
	versions := a.svc.CoreVersions(r.Context())
	for _, ad := range core.Adapters() {
		k := ad.Kind()
		cores = append(cores, coreInfo{Kind: k, Installed: a.svc.cfg.Binaries[k] != "", Version: versions[k], Features: core.Features(k)})
	}
	tunReason := a.svc.cfg.TUNUnavailable
	if tunReason == "" && a.svc.cfg.Binaries[core.SingBox] == "" {
		tunReason = "TUN mode needs the sing-box core installed"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":         Version,
		"build":           BuildNumber(),
		"commit":          Commit,
		"cores":           cores,
		"tun_available":   tunReason == "",
		"tun_unavailable": tunReason,
		"store":           a.svc.Store() != nil,
	})
}

func (a *api) runningApps(w http.ResponseWriter, r *http.Request) {
	list, err := apps.Running()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// The daemon and the cores bypass the tunnel anyway.
	own := map[string]bool{}
	for _, bin := range a.svc.cfg.Binaries {
		own[strings.ToLower(filepath.Base(bin))] = true
	}
	if self, err := os.Executable(); err == nil {
		own[strings.ToLower(filepath.Base(self))] = true
	}
	out := list[:0]
	for _, app := range list {
		if !own[strings.ToLower(app.Name)] {
			out = append(out, app)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) returnToPrimary(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.ReturnToPrimary(r.Context()); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, a.svc.Status())
}

func (a *api) coreUpdates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.svc.CheckCoreUpdates(r.Context()))
}

func (a *api) updateCore(w http.ResponseWriter, r *http.Request) {
	k := core.Kind(r.PathValue("kind"))
	if _, ok := core.ByKind(k); !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown core %q", k))
		return
	}
	v, err := a.svc.UpdateCore(r.Context(), k)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"kind": string(k), "version": v})
}

func (a *api) getSettings(w http.ResponseWriter, r *http.Request, st *store.Store) {
	writeJSON(w, http.StatusOK, st.Settings())
}

func (a *api) putSettings(w http.ResponseWriter, r *http.Request, st *store.Store) {
	// Unknown fields are rejected: a typo would otherwise silently reset
	// a setting to its zero value.
	set := st.Settings()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&set); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid settings: %w", err))
		return
	}
	saved, err := st.SetSettings(set)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// subscriptionView is a subscription as the UI shows it: nodes without
// their credentials, with the cores able to run each.
type subscriptionView struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	DisplayName string     `json:"display_name"`
	URL         string     `json:"url,omitempty"`
	UserAgent   string     `json:"user_agent,omitempty"`
	Info        store.Info `json:"info"`
	Format      string     `json:"format"`
	Nodes       []nodeView `json:"nodes"`
	Skipped     []string   `json:"skipped,omitempty"`
	AddedAt     time.Time  `json:"added_at"`
	UpdatedAt   time.Time  `json:"updated_at,omitzero"`
	CheckedAt   time.Time  `json:"checked_at,omitzero"`
	NextUpdate  time.Time  `json:"next_update,omitzero"`
	LastError   string     `json:"last_error,omitempty"`
}

type nodeView struct {
	Fingerprint string        `json:"fingerprint"`
	Name        string        `json:"name"`
	Protocol    node.Protocol `json:"protocol"`
	Transport   string        `json:"transport"`
	Security    string        `json:"security"`
	Server      string        `json:"server"`
	Port        uint16        `json:"port"`
	// Cores is the auto-swap chain for this node; empty means no installed
	// core can run it.
	Cores []core.Kind `json:"cores"`
	// The last latency test, if the node was tested since the daemon
	// started.
	LatencyMS     int64  `json:"latency_ms,omitempty"`
	LatencyError  string `json:"latency_error,omitempty"`
	LatencyCore   string `json:"latency_core,omitempty"`
	LatencyMethod string `json:"latency_method,omitempty"`
}

func (a *api) nodeView(subID string, n *node.Node) nodeView {
	cores := a.svc.Compatible(n)
	if cores == nil {
		cores = []core.Kind{}
	}
	v := nodeView{
		Fingerprint: n.Fingerprint(), Name: n.Name, Protocol: n.Protocol,
		Transport: n.TransportLabel(), Security: n.SecurityLabel(),
		Server: n.Server, Port: n.Port, Cores: cores,
	}
	if l, ok := a.svc.Latency(subID, v.Fingerprint); ok {
		v.LatencyMS, v.LatencyError, v.LatencyCore, v.LatencyMethod = l.LatencyMS, l.Error, l.Core, l.Method
	}
	return v
}

func (a *api) subscriptionView(sub *store.Subscription, set store.Settings) subscriptionView {
	v := subscriptionView{
		ID: sub.ID, Name: sub.Name, DisplayName: sub.DisplayName(), URL: sub.URL, UserAgent: sub.UserAgent,
		Info: sub.Info, Format: sub.Format, Nodes: make([]nodeView, len(sub.Nodes)), Skipped: sub.Skipped,
		AddedAt: sub.AddedAt, UpdatedAt: sub.UpdatedAt, CheckedAt: sub.CheckedAt,
		NextUpdate: store.NextRefresh(sub, set), LastError: sub.LastError,
	}
	for i := range sub.Nodes {
		v.Nodes[i] = a.nodeView(sub.ID, &sub.Nodes[i])
	}
	return v
}

func (a *api) listSubscriptions(w http.ResponseWriter, r *http.Request, st *store.Store) {
	set := st.Settings()
	subs := st.Subscriptions()
	out := make([]subscriptionView, len(subs))
	for i := range subs {
		out[i] = a.subscriptionView(&subs[i], set)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) getSubscription(w http.ResponseWriter, r *http.Request, st *store.Store) {
	sub, ok := st.Subscription(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, store.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, a.subscriptionView(&sub, st.Settings()))
}

func (a *api) addSubscription(w http.ResponseWriter, r *http.Request, st *store.Store) {
	var req struct {
		Name      string `json:"name"`
		URL       string `json:"url"`
		Content   string `json:"content"`
		UserAgent string `json:"user_agent"`
	}
	if !decode(w, r, &req) {
		return
	}
	// Not the request context: a UI closing mid-fetch should still get the
	// subscription added.
	ctx, cancel := backgroundCtx(time.Minute)
	defer cancel()
	sub, err := st.Add(ctx, store.AddRequest{Name: req.Name, URL: req.URL, Content: req.Content, UserAgent: req.UserAgent})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a.subscriptionView(&sub, st.Settings()))
}

func (a *api) editSubscription(w http.ResponseWriter, r *http.Request, st *store.Store) {
	var e store.Edit
	if !decode(w, r, &e) {
		return
	}
	ctx, cancel := backgroundCtx(time.Minute)
	defer cancel()
	sub, err := st.Edit(ctx, r.PathValue("id"), e)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.subscriptionView(&sub, st.Settings()))
}

func (a *api) removeSubscription(w http.ResponseWriter, r *http.Request, st *store.Store) {
	if err := st.Remove(r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) refreshSubscription(w http.ResponseWriter, r *http.Request, st *store.Store) {
	ctx, cancel := backgroundCtx(time.Minute)
	defer cancel()
	sub, err := st.Refresh(ctx, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.subscriptionView(&sub, st.Settings()))
}

func (a *api) moveSubscription(w http.ResponseWriter, r *http.Request, st *store.Store) {
	var req struct {
		Index *int `json:"index"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Index == nil {
		writeError(w, http.StatusBadRequest, errors.New(`need "index"`))
		return
	}
	if err := st.Move(r.PathValue("id"), *req.Index); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type selectionView struct {
	store.Selection
	// Available is false when the selected node left its subscription.
	Available bool      `json:"available"`
	Node      *nodeView `json:"node,omitempty"`
}

func (a *api) selectionView(st *store.Store) selectionView {
	sel, n, ok := st.Selected()
	v := selectionView{Selection: sel, Available: ok}
	if ok {
		nv := a.nodeView(sel.Subscription, &n)
		v.Node = &nv
	}
	return v
}

func (a *api) getSelection(w http.ResponseWriter, r *http.Request, st *store.Store) {
	writeJSON(w, http.StatusOK, a.selectionView(st))
}

func (a *api) putSelection(w http.ResponseWriter, r *http.Request, st *store.Store) {
	var req store.Selection
	if !decode(w, r, &req) {
		return
	}
	if _, err := st.Select(req.Subscription, req.Fingerprint, req.Name); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.selectionView(st))
}

func (a *api) testLatency(w http.ResponseWriter, r *http.Request, st *store.Store) {
	var req struct {
		Subscription string `json:"subscription"`
	}
	if !decode(w, r, &req) {
		return
	}
	ctx, cancel := backgroundCtx(5 * time.Minute)
	defer cancel()
	res, err := a.svc.TestLatency(ctx, req.Subscription)
	switch {
	case errors.Is(err, ErrTestRunning):
		writeError(w, http.StatusConflict, err)
	case err != nil:
		writeError(w, http.StatusBadRequest, err)
	default:
		writeJSON(w, http.StatusOK, res)
	}
}

// decode reads a JSON body; an empty body decodes as {}.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request: %w", err))
		return false
	}
	return true
}

func writeStoreError(w http.ResponseWriter, err error) {
	var fe *store.FetchError
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, store.ErrExists):
		writeError(w, http.StatusConflict, err)
	case errors.As(err, &fe):
		writeError(w, http.StatusBadGateway, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
}
