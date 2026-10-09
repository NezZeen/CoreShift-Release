package socksgate

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"testing"

	"coreshift/engine/internal/core"
)

// The benchmarks compare a client of the core directly with one going
// through the gate in front of it: the gate's cost is the difference.
//
//	go test ./internal/socksgate -run - -bench . -benchtime 3s

// socksConnect opens a CONNECT to dst through the SOCKS5 server at addr.
func socksConnect(addr netip.AddrPort, auth core.SOCKSAuth, dst netip.AddrPort) (net.Conn, error) {
	c, err := net.Dial("tcp", addr.String())
	if err != nil {
		return nil, err
	}
	msg := append([]byte{5, 1, 2, 1, byte(len(auth.User))}, auth.User...)
	msg = append(append(msg, byte(len(auth.Pass))), auth.Pass...)
	msg = append(msg, 5, 1, 0, 1)
	msg = append(msg, dst.Addr().AsSlice()...)
	msg = binary.BigEndian.AppendUint16(msg, dst.Port())
	if _, err := c.Write(msg); err != nil {
		c.Close()
		return nil, err
	}
	reply := make([]byte, 4+10)
	if _, err := io.ReadFull(c, reply); err != nil {
		c.Close()
		return nil, err
	}
	if reply[0] != 5 || reply[1] != 2 || reply[3] != 0 || reply[5] != 0 {
		c.Close()
		return nil, fmt.Errorf("refused: % x", reply)
	}
	return c, nil
}

// sinkServer serves endless data to whoever connects, or echoes when the
// first byte is 'e'.
func sinkServer(b *testing.B) netip.AddrPort {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { ln.Close() })
	go func() {
		chunk := make([]byte, 256<<10)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				first := make([]byte, 1)
				if _, err := io.ReadFull(c, first); err != nil {
					return
				}
				if first[0] == 'e' {
					c.Write(first)
					io.Copy(c, c)
					return
				}
				for {
					if _, err := c.Write(chunk); err != nil {
						return
					}
				}
			}()
		}
	}()
	return netip.MustParseAddrPort(ln.Addr().String())
}

type benchPath struct {
	name string
	addr netip.AddrPort
	auth core.SOCKSAuth
}

func benchPaths(b *testing.B) []benchPath {
	f := startFakeCore(b, coreAuth)
	g := newGate(b, Config{Auth: clientAuth})
	g.SetTarget(f.target())
	return []benchPath{{"direct", f.addr(), coreAuth}, {"gate", g.Addr(), clientAuth}}
}

// BenchmarkThroughput: a download through one connection.
func BenchmarkThroughput(b *testing.B) {
	sink := sinkServer(b)
	for _, p := range benchPaths(b) {
		b.Run(p.name, func(b *testing.B) {
			c, err := socksConnect(p.addr, p.auth, sink)
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			c.Write([]byte{'d'})
			buf := make([]byte, 1<<20)
			b.SetBytes(int64(len(buf)))
			b.ResetTimer()
			for range b.N {
				if _, err := io.ReadFull(c, buf); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkConnect: a new connection, its request and one round trip, as
// a page's every new connection pays.
func BenchmarkConnect(b *testing.B) {
	sink := sinkServer(b)
	for _, p := range benchPaths(b) {
		b.Run(p.name, func(b *testing.B) {
			one := make([]byte, 1)
			for range b.N {
				c, err := socksConnect(p.addr, p.auth, sink)
				if err != nil {
					b.Fatal(err)
				}
				c.Write([]byte{'e'})
				if _, err := io.ReadFull(c, one); err != nil {
					b.Fatal(err)
				}
				c.Close()
			}
		})
	}
}

// BenchmarkRoundTrip: one small message back and forth on an open
// connection, the latency an interactive session sees.
func BenchmarkRoundTrip(b *testing.B) {
	sink := sinkServer(b)
	for _, p := range benchPaths(b) {
		b.Run(p.name, func(b *testing.B) {
			c, err := socksConnect(p.addr, p.auth, sink)
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			c.Write([]byte{'e'})
			msg := make([]byte, 64)
			if _, err := io.ReadFull(c, msg[:1]); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for range b.N {
				c.Write(msg)
				if _, err := io.ReadFull(c, msg); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
