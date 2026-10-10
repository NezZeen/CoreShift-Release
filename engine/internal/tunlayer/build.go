package tunlayer

func build(o Options) (obj, error) {
	o = o.withDefaults()
	if err := o.validate(); err != nil {
		return nil, err
	}
	dns, err := buildDNS(o)
	if err != nil {
		return nil, err
	}

	addrs := []string{o.Address.String()}
	if o.Address6.IsValid() {
		addrs = append(addrs, o.Address6.String())
	}
	tun := obj{
		"type":           "tun",
		"tag":            tagTun,
		"interface_name": o.InterfaceName,
		"address":        addrs,
		"mtu":            o.MTU,
		"auto_route":     true,
		"strict_route":   o.StrictRoute,
		"stack":          o.Stack,
		// Linux policy routing: our own table and rule priorities rather
		// than sing-box's defaults, so CleanupRoutes can tell the rules a
		// killed TUN layer left from those of anything else.
		"iproute2_table_index": RouteTable,
		"iproute2_rule_index":  RuleIndex,
	}
	exclude := prefixStrings(o.BypassAddresses)
	if o.ExcludeLAN {
		exclude = append(exclude, prefixStrings(excludeLAN(o.LANResolvers))...)
	}
	if len(exclude) > 0 {
		tun["route_exclude_address"] = exclude
	}

	upstream := obj{
		"type":        "socks",
		"tag":         tagProxy,
		"server":      o.Upstream.Addr().String(),
		"server_port": o.Upstream.Port(),
		"version":     "5",
	}
	if o.UpstreamUser != "" {
		upstream["username"], upstream["password"] = o.UpstreamUser, o.UpstreamPass
	}
	cfg := obj{
		"log":       obj{"level": o.LogLevel, "timestamp": true},
		"dns":       dns,
		"inbounds":  []any{tun},
		"outbounds": []any{upstream, obj{"type": "direct", "tag": tagDirect}},
		"route":     buildRoute(o),
	}
	if o.CacheFile != "" {
		cfg["experimental"] = obj{"cache_file": obj{
			"enabled":      true,
			"path":         o.CacheFile,
			"store_fakeip": o.DNS.FakeIP,
		}}
	}
	return cfg, nil
}
