package socksgate

import (
	"encoding/binary"
	"errors"
	"io"
	"net/netip"
)

// SOCKS5 (RFC 1928) commands, address types and replies the gate needs.
const (
	cmdUDPAssociate = 3

	atypIPv4   = 1
	atypDomain = 3
	atypIPv6   = 4

	repFailure = 1
)

var errAddrType = errors.New("socksgate: unknown address type")

// request is a client's request: its command and the bytes as sent.
type request struct {
	cmd byte
	raw []byte
}

// readRequest reads VER CMD RSV ATYP DST.ADDR DST.PORT.
func readRequest(r io.Reader) (request, error) {
	raw, err := readMessage(r)
	if err != nil {
		return request{}, err
	}
	if raw[0] != 5 {
		return request{}, errVersion
	}
	return request{cmd: raw[1], raw: raw}, nil
}

// response is a server's reply: its code, the address it names (for UDP
// ASSOCIATE, the relay the client is to send datagrams to) and the bytes.
type response struct {
	code byte
	addr netip.AddrPort
	raw  []byte
}

// readReply reads VER REP RSV ATYP BND.ADDR BND.PORT.
func readReply(r io.Reader) (response, error) {
	raw, err := readMessage(r)
	if err != nil {
		return response{}, err
	}
	if raw[0] != 5 {
		return response{}, errVersion
	}
	return response{code: raw[1], addr: parseAddr(raw[3:]), raw: raw}, nil
}

// readMessage reads a request or a reply, which are laid out alike.
func readMessage(r io.Reader) ([]byte, error) {
	b := make([]byte, 5, 4+1+255+2)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	var rest int
	switch b[3] {
	case atypIPv4:
		rest = 4 - 1 + 2
	case atypIPv6:
		rest = 16 - 1 + 2
	case atypDomain:
		rest = int(b[4]) + 2
	default:
		return nil, errAddrType
	}
	b = b[:5+rest]
	if _, err := io.ReadFull(r, b[5:]); err != nil {
		return nil, err
	}
	return b, nil
}

// parseAddr reads ATYP ADDR PORT; a domain name gives an invalid AddrPort.
func parseAddr(b []byte) netip.AddrPort {
	switch b[0] {
	case atypIPv4:
		return netip.AddrPortFrom(netip.AddrFrom4([4]byte(b[1:5])), binary.BigEndian.Uint16(b[5:7]))
	case atypIPv6:
		return netip.AddrPortFrom(netip.AddrFrom16([16]byte(b[1:17])), binary.BigEndian.Uint16(b[17:19]))
	}
	return netip.AddrPort{}
}

// appendAddr appends a as ATYP ADDR PORT; the invalid AddrPort as 0.0.0.0:0.
func appendAddr(b []byte, a netip.AddrPort) []byte {
	ip := a.Addr().Unmap()
	switch {
	case ip.Is4():
		b = append(b, atypIPv4)
		b = append(b, ip.AsSlice()...)
	case ip.Is6():
		b = append(b, atypIPv6)
		b = append(b, ip.AsSlice()...)
	default:
		b = append(b, atypIPv4, 0, 0, 0, 0)
	}
	return binary.BigEndian.AppendUint16(b, a.Port())
}

// reply is a reply with code naming addr.
func reply(code byte, addr netip.AddrPort) []byte {
	return appendAddr([]byte{5, code, 0}, addr)
}

// udpHeaderLen is the length of the header in front of a SOCKS5 UDP
// datagram, RSV RSV FRAG ATYP DST.ADDR DST.PORT; 0 when it is malformed.
func udpHeaderLen(b []byte) int {
	if len(b) < 4 {
		return 0
	}
	var n int
	switch b[3] {
	case atypIPv4:
		n = 4 + 4 + 2
	case atypIPv6:
		n = 4 + 16 + 2
	case atypDomain:
		if len(b) < 5 {
			return 0
		}
		n = 4 + 1 + int(b[4]) + 2
	default:
		return 0
	}
	if n > len(b) {
		return 0
	}
	return n
}
