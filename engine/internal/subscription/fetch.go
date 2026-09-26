package subscription

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultUserAgent is sent unless the user overrides it. Panels (Marzban,
// Remnawave, 3x-ui…) pick the response format from the User-Agent; an unknown
// one gets the base64 link list, which carries every protocol we support.
const DefaultUserAgent = "CoreShift/0.1"

const maxBodySize = 16 << 20

// Info is the metadata panels send in response headers.
type Info struct {
	Title          string
	Upload         uint64
	Download       uint64
	Total          uint64    // 0 = unlimited
	Expire         time.Time // zero = never
	UpdateInterval time.Duration
	SupportURL     string
	WebPageURL     string
}

type Fetched struct {
	Result
	Info Info
}

// Fetch downloads and parses a subscription. Errors never include the URL,
// because it usually carries the access token.
func Fetch(ctx context.Context, client *http.Client, rawURL, userAgent string) (Fetched, error) {
	if userAgent == "" {
		userAgent = DefaultUserAgent
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Fetched{}, errors.New("invalid subscription URL")
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	ThisDevice().setHeaders(req.Header)
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return Fetched{}, fmt.Errorf("fetch subscription: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Fetched{}, fmt.Errorf("fetch subscription: server returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize+1))
	if err != nil {
		return Fetched{}, fmt.Errorf("fetch subscription: %w", err)
	}
	if len(body) > maxBodySize {
		return Fetched{}, fmt.Errorf("fetch subscription: response larger than %d MB", maxBodySize>>20)
	}
	res, err := Parse(body)
	if err == nil {
		err = placeholdersOnly(&res)
	}
	return Fetched{Result: res, Info: parseInfo(resp.Header)}, err
}

// placeholdersOnly fails a subscription whose every server is a placeholder
// such as 0.0.0.0:1: panels send those to show a message in the node's name
// ("app not supported", "subscription expired", "device limit reached").
func placeholdersOnly(res *Result) error {
	var names []string
	for _, n := range res.Nodes {
		if !isPlaceholder(n.Server, n.Port) {
			return nil
		}
		names = append(names, strings.TrimSpace(n.Name))
	}
	res.Nodes = nil
	return fmt.Errorf("the panel sent a message instead of servers: %s", strings.Join(names, "; "))
}

func isPlaceholder(server string, port uint16) bool {
	if port <= 1 {
		return true
	}
	ip, err := netip.ParseAddr(strings.Trim(server, "[]"))
	return err == nil && (ip.IsUnspecified() || ip.IsLoopback())
}

func parseInfo(h http.Header) Info {
	var info Info
	for _, part := range strings.Split(h.Get("Subscription-Userinfo"), ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n := parseUint(v)
		switch strings.ToLower(k) {
		case "upload":
			info.Upload = n
		case "download":
			info.Download = n
		case "total":
			info.Total = n
		case "expire":
			if n > 0 {
				info.Expire = time.Unix(int64(n), 0)
			}
		}
	}
	info.Title = decodeTitle(h.Get("Profile-Title"))
	if info.Title == "" {
		if _, params, err := mime.ParseMediaType(h.Get("Content-Disposition")); err == nil {
			info.Title = params["filename"]
		}
	}
	if hours, err := strconv.Atoi(strings.TrimSpace(h.Get("Profile-Update-Interval"))); err == nil && hours > 0 {
		info.UpdateInterval = time.Duration(hours) * time.Hour
	}
	info.SupportURL = h.Get("Support-Url")
	info.WebPageURL = h.Get("Profile-Web-Page-Url")
	return info
}

// parseUint accepts integers and the floats some panels emit ("1.5e+10").
func parseUint(s string) uint64 {
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseUint(s, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
		return uint64(f)
	}
	return 0
}

// decodeTitle handles the "base64:…" form panels use for non-ASCII titles.
func decodeTitle(s string) string {
	s = strings.TrimSpace(s)
	if enc, ok := strings.CutPrefix(s, "base64:"); ok {
		if b, err := decodeBase64(enc); err == nil {
			return string(b)
		}
	}
	return s
}
