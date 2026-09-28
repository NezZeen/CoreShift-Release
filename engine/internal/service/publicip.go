package service

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// IPInfo is the address sites see: the VPN server's while connected, the
// device's own otherwise.
type IPInfo struct {
	IP string `json:"ip"`
	// Country is the ISO 3166 code, "DE"; empty when the service that
	// answered does not say.
	Country string `json:"country,omitempty"`
	// VPN: looked up through the connected server.
	VPN bool `json:"vpn"`
}

// ipLookups answer with the caller's address: Cloudflare's trace with the
// country as well, then a plain one in case Cloudflare is out of reach.
var ipLookups = []string{
	"https://www.cloudflare.com/cdn-cgi/trace",
	"https://1.1.1.1/cdn-cgi/trace",
	"https://api.ipify.org",
}

// PublicIP looks up the address sites see, through the core while
// connected, so that it is the server's in any mode (and on Android, where
// CoreShift itself is outside its VPN). All lookups run at once; the first
// answer with a country wins, else the first answer.
func (s *Service) PublicIP(ctx context.Context) (IPInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	vpn := s.Status().State == Connected
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	if vpn {
		tr.Proxy = http.ProxyURL(&url.URL{Scheme: "socks5", Host: s.cfg.Listen.String()})
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}

	type result struct {
		info IPInfo
		err  error
	}
	results := make(chan result, len(ipLookups))
	for _, u := range ipLookups {
		go func() {
			info, err := lookupIP(ctx, client, u)
			info.VPN = vpn
			results <- result{info, err}
		}()
	}
	var first *IPInfo
	var err error
	for range ipLookups {
		r := <-results
		switch {
		case r.err != nil:
			err = r.err
		case r.info.Country != "":
			return r.info, nil
		case first == nil:
			first = &r.info
		}
	}
	if first != nil {
		return *first, nil
	}
	return IPInfo{}, err
}

func lookupIP(ctx context.Context, client *http.Client, u string) (IPInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return IPInfo{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return IPInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return IPInfo{}, errors.New(u + ": " + resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return IPInfo{}, err
	}
	return parseIPAnswer(string(body))
}

// parseIPAnswer reads Cloudflare's trace ("ip=…\nloc=DE\n…") or a bare
// address.
func parseIPAnswer(body string) (IPInfo, error) {
	var info IPInfo
	if !strings.Contains(body, "=") {
		info.IP = strings.TrimSpace(body)
	}
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		switch {
		case !ok:
		case k == "ip":
			info.IP = v
		case k == "loc" && len(v) == 2 && v != "XX":
			info.Country = strings.ToUpper(v)
		}
	}
	addr, err := netip.ParseAddr(info.IP)
	if err != nil {
		return IPInfo{}, errors.New("no address in the answer")
	}
	info.IP = addr.Unmap().String()
	return info, nil
}
