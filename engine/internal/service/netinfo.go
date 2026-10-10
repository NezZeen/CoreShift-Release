package service

import (
	"context"
	"strings"
	"time"

	"coreshift/engine/internal/msg"
	"coreshift/engine/internal/ping"
	"coreshift/engine/internal/tunlayer"
)

// defaultNetworkName is the desktop's NetworkName: the interface of the
// default route outside the tunnel.
func defaultNetworkName() string {
	name, _ := ping.DefaultRouteInterface(tunlayer.DefaultInterface)
	return name
}

// networkKind says what an interface is from its name, as the systems name
// them: Android's wlan0 and rmnet_data2, Linux's wlp3s0 and enp4s0,
// Windows' «Беспроводная сеть», «Ethernet 2», «Сотовая связь». None when
// the name does not tell.
func networkKind(name string) msg.Msg {
	n := strings.ToLower(name)
	has := func(parts ...string) bool {
		for _, p := range parts {
			if strings.HasPrefix(n, p) || strings.Contains(n, " "+p) {
				return true
			}
		}
		return false
	}
	switch {
	case has("wlan", "wlp", "wl", "wi-fi", "wifi", "беспровод"):
		return msg.New("net.kind.wifi")
	case has("rmnet", "ccmni", "wwan", "wwp", "pdp", "сотов", "cellular", "mobile"):
		return msg.New("net.kind.mobile")
	case has("eth", "enp", "eno", "ens", "enx", "ethernet", "подключение по локальной"):
		return msg.New("net.kind.cable")
	case has("usb", "rndis", "ncm"):
		return msg.New("net.kind.usb")
	}
	return msg.Msg{}
}

// networkLabel names a network for the journal: «Wi-Fi (wlan0)», or the
// interface's own name when it says nothing more.
func networkLabel(name string) msg.Msg {
	if k := networkKind(name); !k.IsZero() && !strings.EqualFold(k.String(), name) {
		return msg.New("net.label", "kind", k, "name", name)
	}
	return msg.Raw(name)
}

// describeNetwork is the network the device is on in one line for the
// journal: which one, whether it has IPv6 of its own, how many resolvers
// it gives. name is its interface, "" when unknown.
func (s *Service) describeNetwork(ctx context.Context, name string) msg.List {
	parts := []msg.Msg{}
	if name != "" {
		parts = append(parts, networkLabel(name))
	}
	if s.cfg.HostIPv6 != nil {
		if s.cfg.HostIPv6() {
			parts = append(parts, msg.New("net.ipv6.yes"))
		} else {
			parts = append(parts, msg.New("net.ipv6.no"))
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if addrs, err := s.systemResolvers(ctx); err == nil {
		parts = append(parts, msg.New("net.dns_count", "n", len(addrs)))
	}
	return msg.Join(" · ", parts...)
}

// netNamer follows the name of the network a connection runs over and
// tells the journal when it changes: Wi-Fi to mobile data, another cable.
type netNamer struct {
	last string
}

// look notes the network's name now; it reports the line for the journal
// when the network is another than the one noted last.
func (n *netNamer) look(ctx context.Context, s *Service) (msg.Msg, bool) {
	name := s.cfg.netName()
	if name == "" || name == n.last {
		return msg.Msg{}, false
	}
	was := n.last
	n.last = name
	if was == "" {
		return msg.Msg{}, false
	}
	return msg.New("net.changed", "was", networkLabel(was), "now", s.describeNetwork(ctx, name)), true
}
