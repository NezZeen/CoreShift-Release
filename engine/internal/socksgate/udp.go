package socksgate

import (
	"errors"
	"io"
	"net"
	"net/netip"
	"time"
)

// associate serves a client's UDP ASSOCIATE (the TUN layer sends all UDP
// this way: DNS through the proxy, QUIC, games, calls). The gate opens a
// UDP relay of its own on loopback, associates with the core for it and
// names it to the client; datagrams then go client → gate → the core's
// relay and back, counted on the way. The association lasts as long as
// the client's connection and the core's.
func (g *Gate) associate(s *session, up net.Conn, req request) {
	c := s.client
	fail := func() { c.Write(reply(repFailure, netip.AddrPort{})) }
	pc, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(netip.AddrPortFrom(g.Addr().Addr(), 0)))
	if err != nil {
		fail()
		return
	}
	g.mu.Lock()
	if _, ok := g.conns[s]; !ok {
		// Closed meanwhile: the core was swapped or the gate closed.
		g.mu.Unlock()
		pc.Close()
		return
	}
	s.udp = pc
	g.mu.Unlock()
	own := pc.LocalAddr().(*net.UDPAddr).AddrPort()

	// The request goes on as the client sent it: clients fill in its
	// address differently (sing names the destination, not the source),
	// and each core makes of it what it does for a client of its own.
	up.SetDeadline(time.Now().Add(replyTimeout))
	if _, err := up.Write(req.raw); err != nil {
		fail()
		return
	}
	rep, err := readReply(up)
	if err != nil {
		fail()
		return
	}
	if rep.code != 0 {
		c.Write(rep.raw)
		return
	}
	relayAt := rep.addr
	if !relayAt.IsValid() {
		fail() // a relay by name: no core answers so
		return
	}
	if relayAt.Addr().IsUnspecified() {
		// "Wherever you reached me": the core's own address.
		coreAddr := up.RemoteAddr().(*net.TCPAddr).AddrPort()
		relayAt = netip.AddrPortFrom(coreAddr.Addr(), relayAt.Port())
	}
	if _, err := c.Write(reply(0, own)); err != nil {
		return
	}
	up.SetDeadline(time.Time{})
	go g.relayUDP(pc, unmap(relayAt))

	// Either connection closing ends the association; returning closes
	// the other and the relay (session.close).
	done := make(chan struct{}, 2)
	go func() { io.Copy(io.Discard, up); done <- struct{}{} }()
	go func() { io.Copy(io.Discard, c); done <- struct{}{} }()
	<-done
}

// relayUDP passes datagrams between the client and the core's relay at
// core until pc is closed. The client is the first sender on loopback:
// the relay serves the one association, not whoever else finds the port.
// What the request says the client sends from cannot tell it: sing, for
// one, names the destination there.
func (g *Gate) relayUDP(pc *net.UDPConn, core netip.AddrPort) {
	buf := make([]byte, 64<<10)
	var client netip.AddrPort
	for {
		n, from, err := pc.ReadFromUDPAddrPort(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// A port unreachable for an earlier datagram, reported on
			// this read (Windows): the next datagrams may well arrive.
			continue
		}
		from = unmap(from)
		pkt := buf[:n]
		switch {
		case from == core:
			if !client.IsValid() {
				continue
			}
			if _, err := pc.WriteToUDPAddrPort(pkt, client); err == nil {
				g.down.Add(int64(payload(pkt)))
			}
		case client.IsValid() && from == client, !client.IsValid() && from.Addr().IsLoopback():
			client = from
			if _, err := pc.WriteToUDPAddrPort(pkt, core); err == nil {
				g.up.Add(int64(payload(pkt)))
			}
		}
	}
}

// payload is the length of a SOCKS5 UDP datagram's data, without its
// header.
func payload(pkt []byte) int { return len(pkt) - udpHeaderLen(pkt) }

func unmap(a netip.AddrPort) netip.AddrPort { return netip.AddrPortFrom(a.Addr().Unmap(), a.Port()) }
