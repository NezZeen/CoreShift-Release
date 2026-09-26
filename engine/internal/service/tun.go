package service

import (
	"context"
	"fmt"
	"net"
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
}

const tunStartTimeout = 20 * time.Second

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
	for attempt := 0; ; attempt++ {
		p, err := t.start(ctx, path, name)
		if err == nil {
			return tunProcess{p, name}, nil
		}
		if attempt >= adapterRetries || !isAdapterRace(err) {
			return nil, err
		}
		delay := time.Duration(attempt+1) * time.Second
		if t.onLine != nil {
			t.onLine(fmt.Sprintf("Windows is still removing the previous TUN adapter; retrying in %s", delay))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func (t *singBoxTUN) start(ctx context.Context, path, name string) (*proc.Process, error) {
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
	err = p.WaitFor(ctx, tunStartTimeout, "interface "+name, func() bool {
		_, err := net.InterfaceByName(name)
		return err == nil
	})
	if err != nil {
		p.Stop()
		return nil, err
	}
	return p, nil
}

// isAdapterRace recognises sing-box failing because the previous Wintun
// adapter is half removed, or its address is not yet released.
func isAdapterRace(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "create adapter") || strings.Contains(msg, "open existing adapter") ||
		strings.Contains(msg, "address: The object already exists")
}

// tunProcess stops like the process it wraps, then waits until the
// interface is gone, so the next Start does not race its removal.
type tunProcess struct {
	*proc.Process
	name string
}

func (t tunProcess) Stop() {
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
