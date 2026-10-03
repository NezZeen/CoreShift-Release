// Package dnswake renews the TUN layer's DNS connections after the device
// slept, for sing-box running in-process (Android).
//
// The remote DNS server's connection (DNS over HTTPS by default) runs
// through the proxy: sing-box, the core's SOCKS port, the core's connection
// to its server, then the resolver. While a phone sleeps, the server or a
// carrier NAT drops the server connection, and the phone never hears of it.
// Nothing that could notice runs meanwhile: Go's timers, sing-box's and the
// core's idle timeouts all count monotonic time, which stops while the
// device sleeps. After waking, lookups go out on the dead connection and
// wait out the whole DNS timeout; only then does sing-box replace it
// (dns/transport/https.go Exchange), and the lookups that were on it fail.
//
// Here every connection-based DNS transport is wrapped: before a lookup, one
// whose last answer came before the device slept (or long ago) is renewed,
// so the lookup goes out on a fresh connection; and a lookup whose
// connection was closed under it (by a renewal, a network change or the
// timeout of another lookup) is asked once more while time is left.
//
// The desktop runs the stock sing-box binary, so this cannot apply there;
// the layer's shorter DNS timeout (tunlayer) covers it on every platform.
package dnswake

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"

	mDNS "github.com/miekg/dns"
)

const (
	// sleepLimit: a connection whose last answer came before the device
	// slept this long is renewed before the next lookup. Carrier NATs and
	// the cores' servers drop idle connections after minutes, and a sleeping
	// phone hears of neither.
	sleepLimit = 30 * time.Second
	// idleLimit: one unused this long, awake or not, is renewed too: that is
	// about when cores, servers and NATs give up on an idle connection.
	idleLimit = 4 * time.Minute
)

// stale reports whether a connection last answered wall ago (wall clock),
// awake ago (monotonic, not counting sleep) is to be renewed.
func stale(wall, awake time.Duration) bool {
	return wall-awake >= sleepLimit || wall >= idleLimit
}

// elapsed returns the time since t on the wall clock and on the monotonic
// one, which stops while the device sleeps.
func elapsed(t time.Time) (wall, awake time.Duration) {
	now := time.Now()
	return now.Round(0).Sub(t.Round(0)), now.Sub(t)
}

// Install wraps the connection-based DNS transports of the sing-box
// registries in ctx (from include.Context). Call it before box.New.
func Install(ctx context.Context) error {
	registry, ok := service.FromContext[adapter.DNSTransportRegistry](ctx).(*dns.TransportRegistry)
	if !ok {
		return errors.New("dnswake: no sing-box DNS transport registry in the context")
	}
	wrapRegistry(registry, include.DNSTransportRegistry())
	return nil
}

// wrapRegistry replaces the constructors of registry's connection-based
// types with ones that wrap what original builds. The option types must be
// the ones original registers for each type.
func wrapRegistry(registry *dns.TransportRegistry, original adapter.DNSTransportRegistry) {
	wrapType[option.RemoteDNSServerOptions](registry, original, C.DNSTypeUDP)
	wrapType[option.RemoteDNSServerOptions](registry, original, C.DNSTypeTCP)
	wrapType[option.RemoteTLSDNSServerOptions](registry, original, C.DNSTypeTLS)
	wrapType[option.RemoteTLSDNSServerOptions](registry, original, C.DNSTypeQUIC)
	wrapType[option.RemoteHTTPSDNSServerOptions](registry, original, C.DNSTypeHTTPS)
	wrapType[option.RemoteHTTPSDNSServerOptions](registry, original, C.DNSTypeHTTP3)
}

func wrapType[O any](registry *dns.TransportRegistry, original adapter.DNSTransportRegistry, typ string) {
	dns.RegisterTransport[O](registry, typ, func(ctx context.Context, logger log.ContextLogger, tag string, options O) (adapter.DNSTransport, error) {
		inner, err := original.CreateDNSTransport(ctx, logger, tag, typ, &options)
		if err != nil {
			return nil, err
		}
		return Wrap(inner), nil
	})
}

// Transport is a DNS transport whose connections are renewed after sleep.
type Transport struct {
	adapter.DNSTransport
	elapsed func(time.Time) (wall, awake time.Duration)

	mu   sync.Mutex
	last time.Time // the last answer or renewal
}

// Wrap returns inner renewed after sleep.
func Wrap(inner adapter.DNSTransport) *Transport {
	return &Transport{DNSTransport: inner, elapsed: elapsed, last: time.Now()}
}

// renewIfStale renews the connections when the last answer is stale.
// Concurrent lookups renew once: the first marks the renewal.
func (t *Transport) renewIfStale() {
	t.mu.Lock()
	renew := stale(t.elapsed(t.last))
	if renew {
		t.last = time.Now()
	}
	t.mu.Unlock()
	if renew {
		t.DNSTransport.Reset()
	}
}

func (t *Transport) answered() {
	t.mu.Lock()
	t.last = time.Now()
	t.mu.Unlock()
}

// retry reports whether a lookup that failed with err is asked once more:
// while the caller still waits, the failure is the connection's (closed
// under the lookup, or unusable), and the next attempt gets a fresh one.
func retry(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() == nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func (t *Transport) Exchange(ctx context.Context, message *mDNS.Msg) (*mDNS.Msg, error) {
	t.renewIfStale()
	response, err := t.DNSTransport.Exchange(ctx, message)
	if retry(ctx, err) {
		response, err = t.DNSTransport.Exchange(ctx, message)
	}
	if err == nil {
		t.answered()
	}
	return response, err
}

func (t *Transport) ExchangeAsync(ctx context.Context, message *mDNS.Msg, callback func(response *mDNS.Msg, err error)) {
	t.renewIfStale()
	t.DNSTransport.ExchangeAsync(ctx, message, func(response *mDNS.Msg, err error) {
		if retry(ctx, err) {
			// Not from inside the callback: it may run where a new
			// connection must not be dialled (a transport's read loop).
			go func() {
				response, err := t.DNSTransport.Exchange(ctx, message)
				if err == nil {
					t.answered()
				}
				callback(response, err)
			}()
			return
		}
		if err == nil {
			t.answered()
		}
		callback(response, err)
	})
}
