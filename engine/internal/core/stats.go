package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
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

var statsClient = &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}

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
	var body struct {
		Up   int64 `json:"uploadTotal"`
		Down int64 `json:"downloadTotal"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&body); err != nil {
		return Traffic{}, fmt.Errorf("clash api: %w", err)
	}
	return Traffic{Up: body.Up, Down: body.Down}, nil
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
