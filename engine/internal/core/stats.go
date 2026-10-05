package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// Traffic is what a core has sent (Up) and received (Down) through the
// node since it started, in bytes.
type Traffic struct {
	Up, Down int64
}

// ReadTraffic asks a core started with Options.Stats for its byte counters.
// sing-box and mihomo answer on their Clash API; xray on its gRPC stats
// service.
func ReadTraffic(ctx context.Context, k Kind, addr netip.AddrPort, secret string) (Traffic, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if k == Xray {
		return xrayTraffic(ctx, addr)
	}
	return clashTraffic(ctx, addr, secret)
}

// statsClient keeps its connection to the core's API between samples: they
// come every second for as long as the VPN is on. Each run of a core has a
// port of its own, and a connection to one that stopped is closed with it.
var statsClient = &http.Client{Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 1, IdleConnTimeout: 30 * time.Second}}

func clashTraffic(ctx context.Context, addr netip.AddrPort, secret string) (Traffic, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr.String()+"/connections", nil)
	if err != nil {
		return Traffic{}, err
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := statsClient.Do(req)
	if err != nil {
		return Traffic{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Traffic{}, fmt.Errorf("clash api: %s", resp.Status)
	}
	t, err := clashTotals(bufio.NewReaderSize(io.LimitReader(resp.Body, 16<<20), 4<<10))
	if err != nil {
		return Traffic{}, fmt.Errorf("clash api: %w", err)
	}
	// Drained, the connection goes back to the pool for the next sample.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 16<<20))
	return t, nil
}

// clashTotals reads uploadTotal and downloadTotal from the Clash API's
// /connections answer, a JSON object, without decoding the list of open
// connections that comes with them: on a busy device it is hundreds of
// kilobytes, every second. It reads only as far as it needs to. sing-box
// sorts the keys (connections first), mihomo does not.
func clashTotals(r *bufio.Reader) (Traffic, error) {
	var t Traffic
	var haveUp, haveDown bool
	depth := 0
	key := make([]byte, 0, 16)
	// expectKey: at depth 1, the next string is a key.
	expectKey := false
	for !haveUp || !haveDown {
		c, err := r.ReadByte()
		if err != nil {
			if err == io.EOF {
				err = errors.New("malformed answer: it ends early")
			}
			return Traffic{}, err
		}
		switch c {
		case '{', '[':
			depth++
			expectKey = depth == 1 && c == '{'
		case '}', ']':
			depth--
			if depth < 0 {
				return Traffic{}, errors.New("malformed answer")
			}
			if depth == 0 {
				return t, nil // a total missing counts as nothing yet
			}
		case ',':
			expectKey = depth == 1
		case '"':
			s, err := readJSONString(r, &key, expectKey)
			if err != nil {
				return Traffic{}, err
			}
			if !expectKey {
				continue
			}
			expectKey = false
			var dst *int64
			switch string(s) {
			case "uploadTotal":
				dst, haveUp = &t.Up, true
			case "downloadTotal":
				dst, haveDown = &t.Down, true
			default:
				continue
			}
			v, err := readJSONInt(r)
			if err != nil {
				return Traffic{}, fmt.Errorf("%s: %w", s, err)
			}
			*dst = v
		}
	}
	return t, nil
}

// readJSONString reads the rest of a string whose opening quote was read.
// With keep it returns the raw bytes (escapes left as they are: the keys
// looked for have none) in *buf; otherwise it only skips them.
func readJSONString(r *bufio.Reader, buf *[]byte, keep bool) ([]byte, error) {
	*buf = (*buf)[:0]
	for {
		c, err := r.ReadByte()
		if err != nil {
			return nil, errors.New("malformed answer: unterminated string")
		}
		switch c {
		case '"':
			return *buf, nil
		case '\\':
			if _, err := r.ReadByte(); err != nil {
				return nil, errors.New("malformed answer: unterminated string")
			}
			c = 0 // an escaped character never matches a key looked for
		}
		if keep && len(*buf) < 32 {
			*buf = append(*buf, c)
		}
	}
}

// readJSONInt reads the colon and the non-negative integer after a key.
func readJSONInt(r *bufio.Reader) (int64, error) {
	var v int64
	digits := 0
	for {
		c, err := r.ReadByte()
		if err != nil {
			return 0, errors.New("malformed answer")
		}
		switch {
		case c == ':' && digits == 0, c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if digits > 0 {
				return v, nil
			}
		case c >= '0' && c <= '9':
			if v > (1<<63-1-9)/10 {
				return 0, errors.New("number out of range")
			}
			v = v*10 + int64(c-'0')
			digits++
		default:
			if digits == 0 {
				return 0, fmt.Errorf("not a number: %q", c)
			}
			// The value ended: put back what follows (a comma, a brace).
			if err := r.UnreadByte(); err != nil {
				return 0, err
			}
			return v, nil
		}
	}
}

// xrayStatsClient speaks HTTP/2 without TLS, which is what gRPC over a
// plain loopback port needs.
var xrayStatsClient = func() *http.Client {
	var p http.Protocols
	p.SetUnencryptedHTTP2(true)
	return &http.Client{Transport: &http.Transport{Proxy: nil, Protocols: &p}}
}()

func xrayTraffic(ctx context.Context, addr netip.AddrPort) (Traffic, error) {
	up, err := xrayStat(ctx, addr, "inbound>>>socks-in>>>traffic>>>uplink")
	if err != nil {
		return Traffic{}, err
	}
	down, err := xrayStat(ctx, addr, "inbound>>>socks-in>>>traffic>>>downlink")
	if err != nil {
		return Traffic{}, err
	}
	return Traffic{Up: up, Down: down}, nil
}

// xrayStat calls StatsService.GetStats. The messages are small enough to
// encode by hand: GetStatsRequest{name = 1} and
// GetStatsResponse{stat = 1: Stat{name = 1, value = 2}}.
func xrayStat(ctx context.Context, addr netip.AddrPort, name string) (int64, error) {
	msg := protoString(1, name)
	frame := make([]byte, 5, 5+len(msg))
	binary.BigEndian.PutUint32(frame[1:], uint32(len(msg)))
	frame = append(frame, msg...)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+addr.String()+"/xray.app.stats.command.StatsService/GetStats", bytes.NewReader(frame))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("TE", "trailers")
	resp, err := xrayStatsClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return 0, err
	}
	status := resp.Trailer.Get("Grpc-Status")
	if status == "" {
		status = resp.Header.Get("Grpc-Status") // a trailers-only response
	}
	if status != "" && status != "0" {
		gm := resp.Trailer.Get("Grpc-Message") + resp.Header.Get("Grpc-Message")
		// A counter appears with the first byte through the outbound.
		if strings.Contains(gm, "not found") {
			return 0, nil
		}
		return 0, fmt.Errorf("xray stats: grpc status %s: %s", status, gm)
	}
	if len(body) < 5 {
		return 0, errors.New("xray stats: empty response")
	}
	stat, ok := protoField(body[5:], 1)
	if !ok {
		return 0, nil
	}
	v, _ := protoVarint(stat, 2)
	return int64(v), nil
}

func protoString(field int, s string) []byte {
	b := binary.AppendUvarint(nil, uint64(field<<3|2))
	b = binary.AppendUvarint(b, uint64(len(s)))
	return append(b, s...)
}

// protoField returns the first length-delimited field number field in b.
func protoField(b []byte, field int) ([]byte, bool) {
	for len(b) > 0 {
		key, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, false
		}
		b = b[n:]
		switch key & 7 {
		case 0:
			_, n = binary.Uvarint(b)
			if n <= 0 {
				return nil, false
			}
			b = b[n:]
		case 2:
			l, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return nil, false
			}
			if int(key>>3) == field {
				return b[n : n+int(l)], true
			}
			b = b[n+int(l):]
		default:
			return nil, false
		}
	}
	return nil, false
}

// protoVarint returns the first varint field number field in b.
func protoVarint(b []byte, field int) (uint64, bool) {
	for len(b) > 0 {
		key, n := binary.Uvarint(b)
		if n <= 0 {
			return 0, false
		}
		b = b[n:]
		switch key & 7 {
		case 0:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return 0, false
			}
			if int(key>>3) == field {
				return v, true
			}
			b = b[n:]
		case 2:
			l, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return 0, false
			}
			b = b[n+int(l):]
		default:
			return 0, false
		}
	}
	return 0, false
}
