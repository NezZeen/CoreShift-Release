package service

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"coreshift/engine/internal/proc"
	"coreshift/engine/internal/tunlayer"
)

// TUNLayer starts the TUN + DNS layer. It is an interface so that tests, and
// Android, where the layer runs inside the app on the VpnService's TUN, can
// replace it (creating a real TUN needs administrator rights).
type TUNLayer interface {
	Start(ctx context.Context, o tunlayer.Options) (TUNInstance, error)
}

type TUNInstance interface {
	Exited() <-chan struct{}
	ExitError() error
	Stop()
}

// singBoxTUN runs the layer as a dedicated sing-box process, separate from
// whichever core is active, so swapping cores never touches the interface.
type singBoxTUN struct {
	group  *proc.Group
	bin    string
	dir    string
	onLine func(string)
	// onRetry tells the journal a start is tried again, and why: the
	// failed attempt's FATAL line alone reads as if the tunnel never came
	// up, though the next one does.
	onRetry func(attempt int, delay time.Duration, cause error)
}

const tunStartTimeout = 20 * time.Second

// tunSettle is how long a layer whose interface came up must keep running
// to count as started: sing-box can still fail right after creating the
// adapter (setting its address or routes), and it is cheaper to see that
// here than to say "connected" and tear it all down a moment later.
const tunSettle = 500 * time.Millisecond

// The layer is killed rather than shut down, so Windows removes its Wintun
// adapter asynchronously, and for a while after the interface has left the
// list a new adapter with the same name can be neither created nor opened.
// Starting waits for the old one to go and retries that specific failure.
const (
	interfaceGoneTimeout = 10 * time.Second
	adapterRetries       = 3
)

func (t *singBoxTUN) Start(ctx context.Context, o tunlayer.Options) (TUNInstance, error) {
	cfg, err := tunlayer.Build(o)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(t.dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(t.dir, "tun.json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		return nil, err
	}
	name := o.InterfaceName
	if name == "" {
		name = tunlayer.DefaultInterface
	}
	if !waitInterfaceGone(ctx, name, interfaceGoneTimeout) {
		return nil, fmt.Errorf("interface %q already exists; is another instance running?", name)
	}
	addr := o.Address
	if !addr.IsValid() {
		addr = tunlayer.DefaultAddress
	}
	for attempt := 0; ; attempt++ {
		p, err := t.start(ctx, path, name, addr.Addr())
		if err == nil {
			var undo func()
			if o.ExcludeLAN {
				undo = addResolverRules(o.LANResolvers, t.onLine)
			}
			return tunProcess{p, name, undo}, nil
		}
		if attempt >= adapterRetries || !isAdapterRace(err) {
			if attempt > 0 {
				err = fmt.Errorf("%w (after %d attempts)", err, attempt+1)
			}
			return nil, err
		}
		delay := time.Duration(attempt+1) * time.Second
		if t.onRetry != nil {
			t.onRetry(attempt+1, delay, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

// start runs the layer and returns once its interface is up with its
// address and the process has kept running a moment after that.
func (t *singBoxTUN) start(ctx context.Context, path, name string, addr netip.Addr) (*proc.Process, error) {
	p, err := t.group.Start(proc.Spec{
		Name:   "tun",
		Path:   t.bin,
		Args:   []string{"run", "-c", path, "-D", t.dir},
		Dir:    t.dir,
		OnLine: t.onLine,
		// Killed, it would leave its adapter for Windows to remove late.
		Graceful: true,
	})
	if err != nil {
		return nil, err
	}
	err = p.WaitFor(ctx, tunStartTimeout, "interface "+name, func() bool { return interfaceHas(name, addr) })
	if err == nil {
		select {
		case <-p.Exited():
			err = p.ExitError()
		case <-ctx.Done():
			err = ctx.Err()
		case <-time.After(tunSettle):
		}
	}
	if err != nil {
		p.Stop()
		return nil, err
	}
	return p, nil
}

// interfaceHas reports whether interface name exists and has addr: an
// adapter left half registered by an earlier run may be listed without it.
func interfaceHas(name string, addr netip.Addr) bool {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return false
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(n.IP); ok && ip.Unmap() == addr {
				return true
			}
		}
	}
	return false
}

// isAdapterRace recognises sing-box failing because the previous Wintun
// adapter is half removed, or its address is not yet released.
func isAdapterRace(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "create adapter") || strings.Contains(msg, "open existing adapter") ||
		strings.Contains(msg, "address: The object already exists")
}

// ipv6Refused recognises the TUN layer failing because the system would not
// give its interface IPv6: sing-tun's "set ipv6 address" (Windows), or
// netlink refusing the IPv6 address (Linux).
func ipv6Refused(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "ipv6") || strings.Contains(msg, tunlayer.DefaultAddress6.Addr().String())
}

// tunProcess stops like the process it wraps, then waits until the
// interface is gone, so the next Start does not race its removal.
type tunProcess struct {
	*proc.Process
	name string
	// undoRules removes the resolver rules (resolver_rules.go), if any.
	undoRules func()
}

func (t tunProcess) Stop() {
	if t.undoRules != nil {
		t.undoRules()
	}
	t.Process.Stop()
	waitInterfaceGone(context.Background(), t.name, interfaceGoneTimeout)
}

// waitInterfaceGone reports whether interface name is absent, waiting up
// to timeout for it to disappear.
func waitInterfaceGone(ctx context.Context, name string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := net.InterfaceByName(name); err != nil {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
