package service

import (
	"sync"
	"time"

	"coreshift/engine/internal/supervisor"
)

// Event is what the UI receives, as JSON over the API or directly on Android.
type Event struct {
	Time time.Time `json:"time"`
	// Kind is one of: state (a failed connection carries its Error),
	// core-state, swap, core-failed, health, log, tun, dns; "network" when
	// the device lost its network or got it back (Reason "lost", "waiting",
	// "back" or "reconnect", see netwatch.go; Line says it in words);
	// "latency" for node latency tests (Reason "started" and
	// "finished" around one result per node); "store" when settings,
	// subscriptions or the selection
	// changed (Reason says which, see store.Change); "options" when the
	// options for the next connection changed; "ping" for the connected
	// server's latency (LatencyMS and Method, or Error), every few seconds;
	// "traffic" every second while connected (Up, Down and rates); "cores"
	// when a core was updated (Core, Line is the new version; Reason
	// "updated", or "applied" when the running connection moved to it
	// without disconnecting; "tun" with Reason "updated" when the desktop's
	// TUN layer moved to an updated sing-box, coreapply.go); "speedtest"
	// while a speed test runs (see Service.SpeedTest); "rules" for the rule
	// sets of the Russian preset (Reason is the set; Line "builtin",
	// "downloaded" or "updated", or an Error: Line "kept" when the previous
	// copy stays in use, "damaged" when the one on disk was thrown away);
	// "direct" (Reason "blocked") when direct connections do not get
	// through the network while the tunnel works, once a connection: Line
	// says so and what to change, empty when the journal said it lately
	// (direct.go, Status.DirectBlocked); "action" for why a connection is
	// about to change, when not by the app's own buttons (Service.
	// LogAction): Line says it, Source is the journal's source for it,
	// empty for the user's action.
	Kind      string `json:"kind"`
	State     State  `json:"state,omitempty"`
	Core      string `json:"core,omitempty"`
	From      string `json:"from,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Error     string `json:"error,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	// Line is a line of output; Source says whose (a core, or "tun").
	Line   string `json:"line,omitempty"`
	Source string `json:"source,omitempty"`
	Probe  bool   `json:"probe,omitempty"`
	// Subscription is the ID a store or latency event is about.
	Subscription string `json:"subscription,omitempty"`
	// Fingerprint identifies the node of a latency event.
	Fingerprint string `json:"fingerprint,omitempty"`
	// Method is how a latency or ping result was measured: "icmp", "tcp"
	// or "proxy" (through Core).
	Method string `json:"method,omitempty"`
	// Traffic events, every second while connected: bytes sent and
	// received through the node during this connection, and per second.
	Up       int64 `json:"up,omitempty"`
	Down     int64 `json:"down,omitempty"`
	UpRate   int64 `json:"up_rate,omitempty"`
	DownRate int64 `json:"down_rate,omitempty"`
}

func fromSupervisor(e supervisor.Event) Event {
	out := Event{
		Time:      e.Time,
		Kind:      string(e.Kind),
		Core:      string(e.Core),
		From:      string(e.From),
		Reason:    string(e.Reason),
		LatencyMS: e.Latency.Milliseconds(),
		Line:      e.Line,
		Probe:     e.Probe,
	}
	if e.Err != nil {
		out.Error = e.Err.Error()
	}
	if e.Kind == supervisor.EventLog {
		out.Source = string(e.Core)
	}
	if e.Kind == supervisor.EventState {
		// Core states are reported as "core" events so they are not confused
		// with the service state.
		out.Kind = "core-state"
		out.Reason = string(e.State)
	}
	return out
}

// hub fans events out to subscribers and keeps recent ones for late joiners,
// such as a UI opened after the connection was made.
type hub struct {
	mu     sync.Mutex
	subs   map[chan Event]struct{}
	recent []Event
	// traffic keeps the last traffic events apart from recent, where one
	// a second would push everything else out; enough for the UI's graph.
	traffic []Event
	// logs keeps the cores' and the TUN layer's output apart too: a verbose
	// journal prints hundreds of lines a minute, which pushed the
	// connection's own events (connected, the network, the session) out
	// of recent before a UI opened late could see them.
	logs []Event
}

const (
	hubReplay   = 300
	trafficKeep = 120
	logsKeep    = 500
	kindTraffic = "traffic"
)

func newHub() *hub { return &hub{subs: map[chan Event]struct{}{}} }

func (h *hub) publish(e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if e.Kind == kindTraffic {
		h.traffic = append(h.traffic, e)
		if len(h.traffic) > trafficKeep {
			h.traffic = h.traffic[len(h.traffic)-trafficKeep:]
		}
	} else if e.Kind == "log" {
		h.logs = append(h.logs, e)
		if len(h.logs) > logsKeep {
			h.logs = h.logs[len(h.logs)-logsKeep:]
		}
	} else {
		h.recent = append(h.recent, e)
		if len(h.recent) > hubReplay {
			h.recent = h.recent[len(h.recent)-hubReplay:]
		}
	}
	for ch := range h.subs {
		select {
		case ch <- e:
		default: // a stalled subscriber loses events rather than blocking the engine
		}
	}
}

// clearTraffic forgets the graph of a connection that ended.
func (h *hub) clearTraffic() {
	h.mu.Lock()
	h.traffic = nil
	h.mu.Unlock()
}

// subscribe returns a channel of new events, optionally preceded by the
// recent ones, and a function that unsubscribes.
func (h *hub) subscribe(replay bool) (<-chan Event, func()) {
	ch := make(chan Event, 256+hubReplay+trafficKeep+logsKeep)
	h.mu.Lock()
	if replay {
		for _, e := range h.recent {
			ch <- e
		}
		for _, e := range h.logs {
			ch <- e
		}
		for _, e := range h.traffic {
			ch <- e
		}
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}
