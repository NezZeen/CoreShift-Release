package subscription

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"coreshift/engine/internal/node"
)

// ParseLink parses one share link: vless://, vmess://, trojan://, ss://,
// hysteria2:// (hy2://), tuic://, anytls:// or wireguard:// (wg://).
//
// Errors never quote the link, since it carries credentials.
func ParseLink(link string) (node.Node, error) {
	link = strings.TrimSpace(link)
	scheme, _, ok := strings.Cut(link, "://")
	if !ok {
		return node.Node{}, errors.New("not a share link")
	}
	scheme = strings.ToLower(scheme)
	var n node.Node
	var err error
	switch scheme {
	case "vless":
		n, err = parseVLESS(link)
	case "vmess":
		n, err = parseVMess(link)
	case "trojan":
		n, err = parseTrojan(link)
	case "ss":
		n, err = parseShadowsocks(link)
	case "hysteria2", "hy2":
		n, err = parseHysteria2(link)
	case "tuic":
		n, err = parseTUIC(link)
	case "anytls":
		n, err = parseAnyTLS(link)
	case "wireguard", "wg":
		n, err = parseWireGuard(link)
	case "http", "https":
		return node.Node{}, errors.New("this is a subscription URL, not a node link; download it with Fetch")
	default:
		return node.Node{}, fmt.Errorf("unsupported scheme %q", scheme)
	}
	if err == nil {
		err = finish(&n)
	}
	if err != nil {
		return node.Node{}, fmt.Errorf("%s: %w", scheme, err)
	}
	return n, nil
}

// finish fills defaults shared by every source format and validates.
func finish(n *node.Node) error {
	if n.Name == "" {
		n.Name = fmt.Sprintf("%s:%d", n.Server, n.Port)
	}
	if n.Transport.Network == node.NetTCP {
		n.Transport.Network = ""
	}
	return n.Validate()
}

// linkParts is a share link split without net/url, which rejects the port
// lists hysteria2 uses and unescaped "/" in base64 userinfo (WireGuard keys).
type linkParts struct {
	UserInfo string // percent-decoded
	Host     string
	Port     string // raw; may be a hysteria2 port list
	Path     string
	Query    url.Values
	Name     string
}

func splitLink(link string) (linkParts, error) {
	var p linkParts
	_, rest, _ := strings.Cut(link, "://")
	rest, frag, _ := strings.Cut(rest, "#")
	p.Name = strings.TrimSpace(unescape(frag))
	rest, rawQuery, _ := strings.Cut(rest, "?")
	// A bad pair must not drop the whole link; keep what parsed.
	p.Query, _ = url.ParseQuery(rawQuery)

	authority := rest
	start := strings.LastIndex(rest, "@")
	if start >= 0 {
		p.UserInfo = unescape(rest[:start])
	}
	if i := strings.IndexByte(rest[start+1:], '/'); i >= 0 {
		authority = rest[start+1 : start+1+i]
		p.Path = rest[start+1+i:]
	} else {
		authority = rest[start+1:]
	}

	if strings.HasPrefix(authority, "[") {
		end := strings.IndexByte(authority, ']')
		if end < 0 {
			return p, errors.New("malformed IPv6 address")
		}
		p.Host = authority[1:end]
		p.Port = strings.TrimPrefix(authority[end+1:], ":")
	} else if i := strings.LastIndexByte(authority, ':'); i >= 0 {
		p.Host, p.Port = authority[:i], authority[i+1:]
	} else {
		p.Host = authority
	}
	if p.Host == "" {
		return p, errors.New("missing server")
	}
	return p, nil
}

func unescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

func parsePort(s string) (uint16, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 16)
	if err != nil || n == 0 {
		return 0, errors.New("missing or invalid port")
	}
	return uint16(n), nil
}

// base builds the common part of a node from link parts.
func base(proto node.Protocol, p linkParts) (node.Node, error) {
	port, err := parsePort(p.Port)
	return node.Node{Name: p.Name, Protocol: proto, Server: p.Host, Port: port}, err
}

func parseVLESS(link string) (node.Node, error) {
	p, err := splitLink(link)
	if err != nil {
		return node.Node{}, err
	}
	n, err := base(node.VLESS, p)
	if err != nil {
		return n, err
	}
	q := p.Query
	n.UUID = p.UserInfo
	n.Flow = q.Get("flow")
	if e := q.Get("encryption"); e != "none" {
		n.Encryption = e
	}
	if n.Transport, err = transportFromQuery(q); err != nil {
		return n, err
	}
	n.TLS, err = tlsFromQuery(q, q.Get("security"))
	return n, err
}

func parseTrojan(link string) (node.Node, error) {
	p, err := splitLink(link)
	if err != nil {
		return node.Node{}, err
	}
	n, err := base(node.Trojan, p)
	if err != nil {
		return n, err
	}
	q := p.Query
	n.Password = p.UserInfo
	if n.Transport, err = transportFromQuery(q); err != nil {
		return n, err
	}
	security := q.Get("security")
	if security == "" {
		security = "tls" // trojan is TLS unless told otherwise
	}
	n.TLS, err = tlsFromQuery(q, security)
	return n, err
}

// transportFromQuery reads the v2rayN / Xray share link transport fields.
func transportFromQuery(q url.Values) (node.Transport, error) {
	t := node.Transport{Host: q.Get("host"), Path: q.Get("path")}
	switch typ := strings.ToLower(q.Get("type")); typ {
	case "", "tcp", "raw":
		t.Network = node.NetTCP
		if h := q.Get("headerType"); h == "http" {
			t.HeaderType = h
		} else {
			t.Host, t.Path = "", ""
		}
	case "ws":
		t.Network = node.NetWS
		t.Path, t.EarlyData = splitEarlyData(t.Path)
	case "grpc":
		t.Network = node.NetGRPC
		t.ServiceName = q.Get("serviceName")
		t.Host = q.Get("authority")
		t.Path = ""
		if m := q.Get("mode"); m == "multi" {
			t.Mode = m
		}
	case "http", "h2":
		t.Network = node.NetHTTP
	case "httpupgrade":
		t.Network = node.NetHTTPUpgrade
	case "xhttp", "splithttp":
		t.Network = node.NetXHTTP
		t.Mode = q.Get("mode")
		if t.Mode == "auto" {
			t.Mode = ""
		}
		extra, err := node.SanitizeXHTTPExtra(q.Get("extra"))
		if err != nil {
			return t, err
		}
		t.Extra = extra
	default:
		return t, fmt.Errorf("unsupported transport %q", typ)
	}
	return t, nil
}

// splitEarlyData moves "?ed=2048" out of a WebSocket path.
func splitEarlyData(path string) (string, uint32) {
	p, rawQuery, ok := strings.Cut(path, "?")
	if !ok {
		return path, 0
	}
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return path, 0
	}
	ed, err := strconv.ParseUint(q.Get("ed"), 10, 32)
	if err != nil {
		return path, 0
	}
	q.Del("ed")
	if len(q) > 0 {
		p += "?" + q.Encode()
	}
	return p, uint32(ed)
}

func tlsFromQuery(q url.Values, security string) (*node.TLS, error) {
	switch strings.ToLower(security) {
	case "", "none":
		return nil, nil
	case "tls", "xtls", "reality":
	default:
		return nil, fmt.Errorf("unsupported security %q", security)
	}
	t := &node.TLS{
		ServerName:  first(q, "sni", "peer", "serverName"),
		ALPN:        splitList(q.Get("alpn")),
		Insecure:    boolParam(q, "allowInsecure", "insecure", "allow_insecure", "skip-cert-verify"),
		Fingerprint: q.Get("fp"),
	}
	if t.Fingerprint == "none" {
		t.Fingerprint = ""
	}
	if strings.EqualFold(security, "reality") {
		t.Reality = &node.Reality{PublicKey: q.Get("pbk"), ShortID: q.Get("sid"), SpiderX: q.Get("spx")}
	}
	return t, nil
}

func first(q url.Values, keys ...string) string {
	for _, k := range keys {
		if v := q.Get(k); v != "" {
			return v
		}
	}
	return ""
}

func boolParam(q url.Values, keys ...string) bool {
	switch strings.ToLower(first(q, keys...)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseVMess handles the v2rayN base64 JSON form and falls back to the
// URL form (vmess://uuid@host:port?...).
func parseVMess(link string) (node.Node, error) {
	payload := strings.TrimPrefix(link[strings.Index(link, "://")+3:], "/")
	raw, err := decodeBase64(payload)
	if err != nil {
		if strings.Contains(payload, "@") {
			return parseVMessURL(link)
		}
		return node.Node{}, errors.New("payload is neither base64 JSON nor a URL")
	}
	var f fields
	if err := json.Unmarshal(raw, &f); err != nil {
		return node.Node{}, errors.New("invalid JSON payload")
	}
	port, err := parsePort(f.str("port"))
	if err != nil {
		return node.Node{}, err
	}
	n := node.Node{
		Name:     strings.TrimSpace(f.str("ps")),
		Protocol: node.VMess,
		Server:   f.str("add"),
		Port:     port,
		UUID:     f.str("id"),
		AlterID:  f.int("aid"),
		Cipher:   f.str("scy"),
	}
	if n.Cipher == "" {
		n.Cipher = "auto"
	}
	// Re-express the JSON as share link query fields to reuse their parsing.
	q := url.Values{}
	net := f.str("net")
	q.Set("type", net)
	q.Set("host", f.str("host"))
	switch net {
	case "grpc":
		q.Set("serviceName", f.str("path"))
		q.Set("mode", f.str("type"))
	case "h2", "http":
		q.Set("host", strings.Split(f.str("host"), ",")[0])
		q.Set("path", f.str("path"))
	default:
		q.Set("path", f.str("path"))
		q.Set("headerType", f.str("type"))
	}
	for _, k := range []string{"sni", "alpn", "fp", "allowInsecure", "insecure"} {
		q.Set(k, f.str(k))
	}
	if n.Transport, err = transportFromQuery(q); err != nil {
		return n, err
	}
	n.TLS, err = tlsFromQuery(q, f.str("tls"))
	return n, err
}

func parseVMessURL(link string) (node.Node, error) {
	p, err := splitLink(link)
	if err != nil {
		return node.Node{}, err
	}
	n, err := base(node.VMess, p)
	if err != nil {
		return n, err
	}
	q := p.Query
	n.UUID = p.UserInfo
	n.Cipher = q.Get("encryption")
	if n.Cipher == "" {
		n.Cipher = "auto"
	}
	if n.Transport, err = transportFromQuery(q); err != nil {
		return n, err
	}
	n.TLS, err = tlsFromQuery(q, q.Get("security"))
	return n, err
}

// parseShadowsocks handles SIP002 (base64 or percent-encoded userinfo) and the
// legacy form where everything before "#" is base64.
func parseShadowsocks(link string) (node.Node, error) {
	body := link[strings.Index(link, "://")+3:]
	body, frag, _ := strings.Cut(body, "#")
	if !strings.Contains(body, "@") {
		main, query, _ := strings.Cut(body, "?")
		dec, err := decodeBase64(strings.TrimSuffix(main, "/"))
		if err != nil {
			return node.Node{}, errors.New("legacy payload is not base64")
		}
		body = string(dec)
		if query != "" {
			body += "?" + query
		}
	}
	p, err := splitLink("ss://" + body + "#" + frag)
	if err != nil {
		return node.Node{}, err
	}
	n, err := base(node.Shadowsocks, p)
	if err != nil {
		return n, err
	}
	userinfo := p.UserInfo
	if !strings.Contains(userinfo, ":") {
		dec, err := decodeBase64(userinfo)
		if err != nil {
			return n, errors.New("userinfo is not base64")
		}
		userinfo = string(dec)
	}
	n.Cipher, n.Password, _ = strings.Cut(userinfo, ":")
	if plugin := p.Query.Get("plugin"); plugin != "" {
		name, opts, _ := strings.Cut(plugin, ";")
		n.Shadowsocks = &node.ShadowsocksOptions{Plugin: name, PluginOpts: opts}
	}
	return n, nil
}

func parseHysteria2(link string) (node.Node, error) {
	p, err := splitLink(link)
	if err != nil {
		return node.Node{}, err
	}
	var hop string
	if strings.ContainsAny(p.Port, ",-") {
		hop = p.Port
		p.Port = strings.FieldsFunc(p.Port, func(r rune) bool { return r == ',' || r == '-' })[0]
	}
	n, err := base(node.Hysteria2, p)
	if err != nil {
		return n, err
	}
	q := p.Query
	n.Password = p.UserInfo
	n.TLS = &node.TLS{
		ServerName: first(q, "sni", "peer"),
		ALPN:       splitList(q.Get("alpn")),
		Insecure:   boolParam(q, "insecure", "allowInsecure"),
	}
	if pin := q.Get("pinSHA256"); pin != "" {
		if n.TLS.PinSHA256, err = node.NormalizePin(pin); err != nil {
			return n, err
		}
	}
	opts := node.Hysteria2Options{Obfs: q.Get("obfs"), ObfsPassword: q.Get("obfs-password"), Ports: hop}
	if m := q.Get("mport"); m != "" {
		opts.Ports = m
	}
	if opts != (node.Hysteria2Options{}) {
		n.Hysteria2 = &opts
	}
	return n, nil
}

func parseTUIC(link string) (node.Node, error) {
	p, err := splitLink(link)
	if err != nil {
		return node.Node{}, err
	}
	n, err := base(node.TUIC, p)
	if err != nil {
		return n, err
	}
	q := p.Query
	n.UUID, n.Password, _ = strings.Cut(p.UserInfo, ":")
	n.TLS = &node.TLS{
		ServerName: q.Get("sni"),
		ALPN:       splitList(q.Get("alpn")),
		Insecure:   boolParam(q, "allow_insecure", "allowInsecure", "insecure"),
	}
	n.TUIC = &node.TUICOptions{
		CongestionControl: first(q, "congestion_control", "congestion-control"),
		UDPRelayMode:      first(q, "udp_relay_mode", "udp-relay-mode"),
	}
	return n, nil
}

func parseAnyTLS(link string) (node.Node, error) {
	p, err := splitLink(link)
	if err != nil {
		return node.Node{}, err
	}
	n, err := base(node.AnyTLS, p)
	if err != nil {
		return n, err
	}
	q := p.Query
	n.Password = p.UserInfo
	n.TLS = &node.TLS{
		ServerName:  q.Get("sni"),
		ALPN:        splitList(q.Get("alpn")),
		Insecure:    boolParam(q, "insecure", "allowInsecure"),
		Fingerprint: q.Get("fp"),
	}
	return n, nil
}

func parseWireGuard(link string) (node.Node, error) {
	p, err := splitLink(link)
	if err != nil {
		return node.Node{}, err
	}
	n, err := base(node.WireGuard, p)
	if err != nil {
		return n, err
	}
	q := p.Query
	wg := &node.WireGuardOptions{
		PrivateKey:    p.UserInfo,
		PeerPublicKey: first(q, "publickey", "public_key", "peer_public_key"),
		PreSharedKey:  first(q, "presharedkey", "pre_shared_key"),
	}
	if wg.Address, err = parsePrefixes(splitList(first(q, "address", "ip"))); err != nil {
		return n, err
	}
	if wg.Reserved, err = parseReserved(q.Get("reserved")); err != nil {
		return n, err
	}
	wg.MTU, _ = strconv.Atoi(q.Get("mtu"))
	n.WireGuard = wg
	return n, nil
}

// parsePrefixes accepts "10.0.0.2/32" and bare addresses ("10.0.0.2").
func parsePrefixes(list []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, s := range list {
		if pr, err := netip.ParsePrefix(s); err == nil {
			out = append(out, pr)
			continue
		}
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q", s)
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

// parseReserved accepts "1,2,3" or the base64 form some clients emit.
func parseReserved(s string) ([]uint8, error) {
	if s == "" {
		return nil, nil
	}
	parts := splitList(s)
	if len(parts) == 1 {
		if b, err := decodeBase64(s); err == nil && len(b) == 3 {
			return b, nil
		}
	}
	out := make([]uint8, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseUint(p, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid reserved bytes")
		}
		out = append(out, uint8(v))
	}
	return out, nil
}

// decodeBase64 accepts standard and URL alphabets, padded or not, and
// ignores line breaks.
func decodeBase64(s string) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	if s == "" {
		return nil, errors.New("empty")
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("not base64")
}
