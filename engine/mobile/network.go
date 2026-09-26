//go:build android

package mobile

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"

	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/x/list"
)

// SetNetwork reports the network the phone uses, as ConnectivityManager
// sees it: the interface's name and index, its addresses ("10.0.0.5/24",
// comma separated) and DNS servers (comma separated). An empty name means
// offline. The app calls it at start and on every change.
//
// Go cannot read this on Android: since Android 11 apps may not list
// network interfaces through netlink.
func SetNetwork(name string, index int32, addresses, dns string) {
	currentNetwork.set(name, int(index), parseList(addresses, netip.ParsePrefix), parseList(dns, netip.ParseAddr))
}

func parseList[T any](s string, parse func(string) (T, error)) []T {
	var out []T
	for _, f := range strings.Split(s, ",") {
		if v, err := parse(strings.TrimSpace(f)); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// networkState is the phone's network, and sing-box's default interface
// monitor.
type networkState struct {
	mu        sync.Mutex
	iface     *control.Interface
	dns       []netip.Addr
	callbacks list.List[tun.DefaultInterfaceUpdateCallback]
	mine      []string
}

var currentNetwork = &networkState{}

func (n *networkState) set(name string, index int, addrs []netip.Prefix, dns []netip.Addr) {
	n.mu.Lock()
	var iface *control.Interface
	if name != "" {
		iface = &control.Interface{Index: index, MTU: 1500, Name: name, Addresses: addrs, Flags: net.FlagUp | net.FlagRunning}
	}
	changed := !sameInterface(n.iface, iface)
	n.iface, n.dns = iface, dns
	callbacks := n.callbacks.Array()
	n.mu.Unlock()
	if changed {
		// sing-box closes connections of the old network.
		for _, cb := range callbacks {
			cb(iface, 0)
		}
	}
}

func sameInterface(a, b *control.Interface) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Index == b.Index && a.Name == b.Name
}

// resolvers are the DNS servers of the network: the TUN layer's resolver
// for direct names.
func (n *networkState) resolvers(context.Context, string) ([]netip.Addr, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.dns) == 0 {
		return nil, errors.New("the network reported no DNS servers")
	}
	return append([]netip.Addr(nil), n.dns...), nil
}

// hasIPv6 reports whether the network gives the phone IPv6 of its own.
func (n *networkState) hasIPv6() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.iface == nil {
		return false
	}
	relayed := []netip.Prefix{netip.MustParsePrefix("2001::/32"), netip.MustParsePrefix("2002::/16")}
	for _, p := range n.iface.Addresses {
		a := p.Addr()
		if a.Is6() && a.IsGlobalUnicast() && !relayed[0].Contains(a) && !relayed[1].Contains(a) {
			return true
		}
	}
	return false
}

func (n *networkState) interfaces() []control.Interface {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.iface == nil {
		return nil
	}
	return []control.Interface{*n.iface}
}

// tun.DefaultInterfaceMonitor, fed by SetNetwork.

func (n *networkState) Start() error { return nil }
func (n *networkState) Close() error { return nil }

func (n *networkState) DefaultInterface() *control.Interface {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.iface
}

func (n *networkState) OverrideAndroidVPN() bool { return false }
func (n *networkState) AndroidVPNEnabled() bool  { return false }

func (n *networkState) RegisterCallback(cb tun.DefaultInterfaceUpdateCallback) *list.Element[tun.DefaultInterfaceUpdateCallback] {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.callbacks.PushBack(cb)
}

func (n *networkState) UnregisterCallback(e *list.Element[tun.DefaultInterfaceUpdateCallback]) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.callbacks.Remove(e)
}

func (n *networkState) RegisterMyInterface(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.mine = append(n.mine, name)
}

func (n *networkState) MyInterfaces() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.mine...)
}
