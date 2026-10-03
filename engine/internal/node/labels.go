package node

import "strings"

// TransportLabel is a short human-readable transport, e.g. "ws", "xhttp/auto"
// or "tcp +vision".
func (n *Node) TransportLabel() string {
	switch n.Protocol {
	case Hysteria2, TUIC:
		return "quic"
	case WireGuard:
		return "udp"
	}
	t := string(n.Transport.Network)
	if t == "" {
		t = "tcp"
	}
	if n.Transport.Mode != "" {
		t += "/" + n.Transport.Mode
	}
	if n.Flow != "" {
		t += " +" + strings.TrimPrefix(n.Flow, "xtls-rprx-")
	}
	return t
}

// SecurityLabel is "none", "tls", "tls (pinned)", "tls (insecure)" or
// "reality".
func (n *Node) SecurityLabel() string {
	switch {
	case n.TLS == nil:
		return "none"
	case n.TLS.Reality != nil:
		return "reality"
	case n.TLS.PinSHA256 != "":
		return "tls (pinned)"
	case n.TLS.Insecure:
		return "tls (insecure)"
	}
	return "tls"
}
