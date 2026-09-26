//go:build android

package mobile

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"coreshift/engine/internal/service"
	"coreshift/engine/internal/tunlayer"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/logger"
	singservice "github.com/sagernet/sing/service"
	"golang.org/x/sys/unix"
)

// vpnTUN is the service's TUN layer on Android: the desktop configuration,
// run by sing-box inside the app on the VpnService's TUN.
type vpnTUN struct {
	platform Platform
	dir      string
	log      func(source, line string)
}

func (t *vpnTUN) Start(ctx context.Context, o tunlayer.Options) (service.TUNInstance, error) {
	o.Platform = true
	cfg, err := tunlayer.Build(o)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(t.dir, 0o700); err != nil {
		return nil, err
	}
	pl := &platform{app: t.platform}
	bctx := include.Context(context.Background())
	bctx = singservice.ContextWith[adapter.PlatformInterface](bctx, pl)
	options, err := json.UnmarshalExtendedContext[option.Options](bctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("TUN layer config: %w", err)
	}
	instance, err := box.New(box.Options{Context: bctx, Options: options, PlatformLogWriter: logWriter(t.log)})
	if err != nil {
		t.platform.CloseTun()
		return nil, err
	}
	if err := instance.Start(); err != nil {
		instance.Close()
		t.platform.CloseTun()
		return nil, err
	}
	i := &vpnInstance{box: instance, platform: t.platform, exited: make(chan struct{})}
	currentTUN.set(i)
	return i, nil
}

type logWriter func(source, line string)

func (w logWriter) WriteMessage(level log.Level, message string) {
	if w != nil && level <= log.LevelWarn {
		w("tun", message)
	}
}

// vpnInstance is a running TUN layer.
type vpnInstance struct {
	box      *box.Box
	platform Platform
	once     sync.Once
	exited   chan struct{}
	mu       sync.Mutex
	err      error
}

func (i *vpnInstance) Exited() <-chan struct{} { return i.exited }

func (i *vpnInstance) ExitError() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.err
}

func (i *vpnInstance) Stop() { i.stop(nil) }

func (i *vpnInstance) stop(err error) {
	i.once.Do(func() {
		i.mu.Lock()
		i.err = err
		i.mu.Unlock()
		i.box.Close()
		i.platform.CloseTun()
		currentTUN.clear(i)
		close(i.exited)
	})
}

// currentTUN is the running instance, for Revoked.
var currentTUN = &tunSlot{}

type tunSlot struct {
	mu sync.Mutex
	i  *vpnInstance
}

func (s *tunSlot) set(i *vpnInstance) { s.mu.Lock(); s.i = i; s.mu.Unlock() }

func (s *tunSlot) clear(i *vpnInstance) {
	s.mu.Lock()
	if s.i == i {
		s.i = nil
	}
	s.mu.Unlock()
}

func (s *tunSlot) revoke() {
	s.mu.Lock()
	i := s.i
	s.mu.Unlock()
	if i != nil {
		// The service sees the layer exit and disconnects with this.
		go i.stop(errors.New("android turned the VPN off: another VPN app was started, or CoreShift was turned off in the system settings"))
	}
}

// platform is sing-box's view of Android: the TUN comes from the
// VpnService, the network from ConnectivityManager (SetNetwork), and the
// rest is not used.
type platform struct {
	app   Platform
	mu    sync.Mutex
	addrs []netip.Addr
}

var _ adapter.PlatformInterface = (*platform)(nil)

func (p *platform) Initialize(adapter.NetworkManager) error { return nil }

// The app is outside the VPN, so its sockets need no protecting.
func (p *platform) UsePlatformAutoDetectInterfaceControl() bool { return true }
func (p *platform) AutoDetectInterfaceControl(int) error        { return nil }

func (p *platform) UsePlatformInterface() bool { return true }

func (p *platform) OpenInterface(options *tun.Options, _ option.TunPlatformOptions) (tun.Tun, error) {
	cfg := &TunConfig{MTU: int32(options.MTU)}
	var addrs []netip.Addr
	if len(options.Inet4Address) > 0 {
		cfg.Address4 = options.Inet4Address[0].String()
		cfg.DNS = tunlayer.DNSAddress(options.Inet4Address[0]).String()
		addrs = append(addrs, options.Inet4Address[0].Addr())
	}
	if len(options.Inet6Address) > 0 {
		cfg.Address6 = options.Inet6Address[0].String()
		addrs = append(addrs, options.Inet6Address[0].Addr())
	}
	fd, err := p.app.OpenTun(cfg)
	if err != nil {
		return nil, fmt.Errorf("start the VPN: %w", err)
	}
	name, err := tunName(int(fd))
	if err != nil {
		return nil, err
	}
	// sing-box closes its copy; the VpnService keeps the original.
	dup, err := unix.Dup(int(fd))
	if err != nil {
		return nil, fmt.Errorf("dup TUN: %w", err)
	}
	options.Name = name
	options.FileDescriptor = dup
	if options.InterfaceMonitor != nil {
		options.InterfaceMonitor.RegisterMyInterface(name)
	}
	p.mu.Lock()
	p.addrs = addrs
	p.mu.Unlock()
	return tun.New(*options)
}

func tunName(fd int) (string, error) {
	var ifr [unix.IFNAMSIZ + 64]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TUNGETIFF), uintptr(unsafe.Pointer(&ifr[0]))); errno != 0 {
		return "", fmt.Errorf("name of the TUN: %w", syscall.Errno(errno))
	}
	return unix.ByteSliceToString(ifr[:]), nil
}

func (p *platform) ProcessPlatformOptions(option.TunPlatformOptions) error { return nil }

func (p *platform) UsePlatformDefaultInterfaceMonitor() bool { return true }

func (p *platform) CreateDefaultInterfaceMonitor(logger.Logger) tun.DefaultInterfaceMonitor {
	return currentNetwork
}

func (p *platform) UsePlatformNetworkInterfaces() bool { return true }

func (p *platform) NetworkInterfaces() ([]adapter.NetworkInterface, error) {
	currentNetwork.mu.Lock()
	dns := make([]string, len(currentNetwork.dns))
	for i, a := range currentNetwork.dns {
		dns[i] = a.String()
	}
	currentNetwork.mu.Unlock()
	var out []adapter.NetworkInterface
	for _, ifc := range currentNetwork.interfaces() {
		out = append(out, adapter.NetworkInterface{Interface: ifc, DNSServers: dns})
	}
	return out, nil
}

func (p *platform) UnderNetworkExtension() bool              { return false }
func (p *platform) NetworkExtensionIncludeAllNetworks() bool { return false }
func (p *platform) ClearDNSCache()                           {}
func (p *platform) RequestPermissionForWIFIState() error     { return nil }
func (p *platform) UsePlatformWIFIMonitor() bool             { return false }

func (p *platform) ReadWIFIState(context.Context) adapter.WIFIState { return adapter.WIFIState{} }

func (p *platform) UsePlatformConnectionOwnerFinder() bool { return false }

func (p *platform) FindConnectionOwner(*adapter.FindConnectionOwnerRequest) (*adapter.ConnectionOwner, error) {
	return nil, os.ErrInvalid
}

func (p *platform) UsePlatformNotification() bool                       { return false }
func (p *platform) SendNotification(*adapter.Notification) error        { return nil }
func (p *platform) CancelNotification(identifier string, _ int32) error { return nil }

func (p *platform) MyInterfaceAddress() []netip.Addr {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.addrs
}

func (p *platform) UsePlatformNeighborResolver() bool { return false }

func (p *platform) StartNeighborMonitor(adapter.NeighborUpdateListener) error { return os.ErrInvalid }
func (p *platform) CloseNeighborMonitor(adapter.NeighborUpdateListener) error { return nil }

func (p *platform) UsePlatformShell() bool    { return false }
func (p *platform) CheckPlatformShell() error { return os.ErrInvalid }

func (p *platform) OpenShellSession(*adapter.PlatformUser, string, []string, string, int32, int32) (adapter.ShellSession, error) {
	return nil, os.ErrInvalid
}

func (p *platform) LookupUser(string) (*adapter.PlatformUser, error) { return nil, os.ErrInvalid }
func (p *platform) LookupSFTPServer() (string, error)                { return "", os.ErrInvalid }
func (p *platform) ReadSystemSSHHostKey() ([]byte, error)            { return nil, os.ErrInvalid }
func (p *platform) TailscaleHostname() string                        { return "" }
func (p *platform) UsePlatformBridge() bool                          { return false }

func (p *platform) CreateBridge(adapter.BridgeOptions) (adapter.BridgeSession, error) {
	return nil, os.ErrInvalid
}
