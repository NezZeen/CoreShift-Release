package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/showwin/speedtest-go/speedtest"
)

func init() {
	// Loading speedtest makes http.DefaultClient its own (its package-level
	// client sets the default client's transport to itself, with its User-
	// Agent). Nothing here uses the default client, but whatever does gets
	// Go's plain transport back. The service's own clients never come from
	// New, which would do it again.
	http.DefaultClient.Transport = nil
}

// ooklaConns is how many connections an Ookla phase keeps busy, as Throne
// and speedtest.net do.
const ooklaConns = 8

// ooklaPickTimeout bounds finding the server: the list, then a ping of
// each server on it (at most 4 s, by the library).
const ooklaPickTimeout = 20 * time.Second

// ooklaServer is the speedtest.net server a test measures against.
type ooklaServer struct {
	// Name is the server's city, Sponsor who runs it.
	Name, Sponsor, Country string
	Latency                time.Duration
}

// ooklaTest is one speed test against speedtest.net's servers; tests put
// a fake in its place (Config.ookla).
type ooklaTest interface {
	// Pick takes the server list for the address the test comes from and
	// the nearest server on it that answers.
	Pick(ctx context.Context) (ooklaServer, error)
	// Download and Upload run their phase for up to d and return its
	// speed in bytes per second, 0 when unknown.
	Download(ctx context.Context, d time.Duration) (int64, error)
	Upload(ctx context.Context, d time.Duration) (int64, error)
	// Rate is the running phase's smoothed speed, bytes per second, and
	// Moved what it has moved; both are safe to call while it runs.
	Rate() int64
	Moved() int64
	// Close lets the connections go.
	Close()
}

// errNoOoklaServer means speedtest.net named no server that answers.
var errNoOoklaServer = errors.New("no speedtest.net server answered")

// ooklaLive is the real test, with github.com/showwin/speedtest-go.
type ooklaLive struct {
	st    *speedtest.Speedtest
	uc    *speedtest.UserConfig // the library's, read when it fetches
	mgr   *ooklaManager
	tr    *http.Transport
	srv   *speedtest.Server
	rate  atomic.Int64
	phase atomic.Int32 // 1 download, 2 upload
}

// newOoklaTest goes through proxy (the core's SOCKS inbound, credentials
// included; nil for none) on the transport the Cloudflare test uses, so
// both take the same path.
func newOoklaTest(proxy *url.URL) ooklaTest {
	tr := newTransport(proxy)
	tr.MaxIdleConnsPerHost, tr.MaxConnsPerHost = ooklaConns, ooklaConns+2
	client := &http.Client{}
	o := &ooklaLive{tr: tr, mgr: &ooklaManager{Manager: speedtest.NewDataManager(), stop: make(chan struct{})}}
	// Not speedtest.New: it starts from http.DefaultClient and makes it
	// the library's. The client is ours: the library points it at its own
	// transport, and ours, through the proxy, takes its place right after.
	o.st = &speedtest.Speedtest{Manager: o.mgr}
	o.uc = &speedtest.UserConfig{PingMode: speedtest.HTTP, MaxConnections: ooklaConns}
	speedtest.WithDoer(client)(o.st)
	speedtest.WithUserConfig(o.uc)(o.st)
	client.Transport = userAgent{tr}
	o.mgr.SetCallbackDownload(func(r speedtest.ByteRate) {
		if o.phase.Load() == 1 {
			o.rate.Store(int64(r))
		}
	})
	o.mgr.SetCallbackUpload(func(r speedtest.ByteRate) {
		if o.phase.Load() == 2 {
			o.rate.Store(int64(r))
		}
	})
	return o
}

func (o *ooklaLive) Pick(ctx context.Context) (ooklaServer, error) {
	ctx, cancel := context.WithTimeout(ctx, ooklaPickTimeout)
	defer cancel()
	// speedtest.net places many a VPN server's address wrong, in the
	// middle of the USA say; Cloudflare knows better where it is.
	if loc := egressLocation(ctx, &http.Client{Transport: o.tr}, speedServer+"/meta"); loc != nil {
		o.uc.Location = loc
	}
	list, err := o.st.FetchServerListContext(ctx)
	if err != nil {
		return ooklaServer{}, fmt.Errorf("speedtest.net server list: %w", err)
	}
	// The list is sorted by distance from the address the request came
	// from; FindServer takes the quickest of it to answer.
	found, err := list.FindServer(nil)
	if err != nil || len(found) == 0 || found[0].Latency <= 0 {
		return ooklaServer{}, errNoOoklaServer
	}
	o.srv = found[0]
	return ooklaServer{Name: o.srv.Name, Sponsor: o.srv.Sponsor, Country: o.srv.Country, Latency: o.srv.Latency}, nil
}

func (o *ooklaLive) Download(ctx context.Context, d time.Duration) (int64, error) {
	if o.srv == nil {
		return 0, errNoOoklaServer
	}
	o.begin(1, d)
	err := o.srv.DownloadTestContext(ctx)
	return max(int64(o.srv.DLSpeed), 0), err
}

func (o *ooklaLive) Upload(ctx context.Context, d time.Duration) (int64, error) {
	if o.srv == nil {
		return 0, errNoOoklaServer
	}
	o.begin(2, d)
	err := o.srv.UploadTestContext(ctx)
	return max(int64(o.srv.ULSpeed), 0), err
}

// begin starts a phase: the library ends it after d, or sooner when the
// rate has settled.
func (o *ooklaLive) begin(phase int32, d time.Duration) {
	o.rate.Store(0)
	o.phase.Store(phase)
	o.mgr.SetCaptureTime(d)
}

func (o *ooklaLive) Rate() int64 { return o.rate.Load() }

func (o *ooklaLive) Moved() int64 {
	if o.phase.Load() == 2 {
		return o.mgr.GetTotalUpload()
	}
	return o.mgr.GetTotalDownload()
}

// Close stops what is left of a phase and drops the connections.
func (o *ooklaLive) Close() {
	o.mgr.close()
	o.tr.CloseIdleConnections()
}

// ooklaManager is the library's data manager with a way to stop: the
// library's phase ignores its context and only ends on its timer, its
// workers asking again and again as soon as their requests fail. Once
// stopped, they wait instead until that timer ends the phase.
type ooklaManager struct {
	speedtest.Manager
	stop    chan struct{}
	stopped atomic.Bool
}

func (m *ooklaManager) close() {
	if m.stopped.CompareAndSwap(false, true) {
		close(m.stop)
	}
}

func (m *ooklaManager) wrap(fn func()) func() {
	return func() {
		select {
		case <-m.stop:
			time.Sleep(50 * time.Millisecond)
		default:
			fn()
		}
	}
}

func (m *ooklaManager) RegisterDownloadHandler(fn func()) *speedtest.TestDirection {
	return m.Manager.RegisterDownloadHandler(m.wrap(fn))
}

func (m *ooklaManager) RegisterUploadHandler(fn func()) *speedtest.TestDirection {
	return m.Manager.RegisterUploadHandler(m.wrap(fn))
}

// egressLocation is where Cloudflare places the address the test comes
// from, by its answer at metaURL; nil when it does not say.
func egressLocation(ctx context.Context, client *http.Client, metaURL string) *speedtest.Location {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	// It answers 403 to a request that does not come from its page.
	req.Header.Set("Referer", speedServer+"/")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var meta struct {
		City      string `json:"city"`
		Country   string `json:"country"`
		Latitude  string `json:"latitude"`
		Longitude string `json:"longitude"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&meta) != nil {
		return nil
	}
	lat, err1 := strconv.ParseFloat(meta.Latitude, 64)
	lon, err2 := strconv.ParseFloat(meta.Longitude, 64)
	if err1 != nil || err2 != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 || (lat == 0 && lon == 0) {
		return nil
	}
	return &speedtest.Location{Name: meta.City, CC: meta.Country, Lat: lat, Lon: lon}
}

// userAgent names the client as the library does; speedtest.net servers
// expect it.
type userAgent struct{ rt http.RoundTripper }

func (u userAgent) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("User-Agent", speedtest.DefaultUserAgent)
	return u.rt.RoundTrip(r)
}
