package ruleset

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/sagernet/sing-box/common/srs"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"google.golang.org/protobuf/encoding/protowire"
)

// MaxDatSize bounds a geosite.dat or geoip.dat file: the lists of every
// category, where a rule set is one.
const MaxDatSize = maxUnpacked

// The v2ray lists (v2fly's and Xray's geosite.dat and geoip.dat), as
// protobuf:
//
//	GeoSiteList { repeated GeoSite entry = 1; }
//	GeoSite     { string country_code = 1; repeated Domain domain = 2; }
//	Domain      { Type type = 1; string value = 2; repeated Attribute attribute = 3; }
//	Attribute   { string key = 1; bool bool_value = 2; int64 int_value = 3; }
//	GeoIPList   { repeated GeoIP entry = 1; }
//	GeoIP       { string country_code = 1; repeated CIDR cidr = 2; bool reverse_match = 3; }
//	CIDR        { bytes ip = 1; uint32 prefix = 2; }
//
// Domain types: 0 a keyword (Plain), 1 a regular expression, 2 a domain
// and its subdomains, 3 the exact name (Full).
const (
	domainPlain = iota
	domainRegex
	domainSuffix
	domainFull
)

// FromDat makes a rule set of the category name of a v2ray list: dat is a
// geosite.dat, or a geoip.dat with ip. Categories are matched in any case
// ("RU", "ru"); "google@cn" takes only the names of google with the
// attribute cn, as v2ray does. Regular expressions Go cannot read are left
// out.
func FromDat(dat []byte, ip bool, name string) ([]byte, error) {
	if len(dat) > MaxDatSize {
		return nil, errors.New("list too large")
	}
	code, attrs, _ := strings.Cut(name, "@")
	var want []string
	if attrs != "" {
		if ip {
			return nil, fmt.Errorf("geoip categories have no attributes: %q", name)
		}
		want = strings.Split(attrs, "@")
	}
	var rule option.DefaultHeadlessRule
	found := false
	err := fields(dat, func(num protowire.Number, typ protowire.Type, v []byte) error {
		if num != 1 || typ != protowire.BytesType {
			return nil
		}
		if c, err := entryCode(v); err != nil || !strings.EqualFold(c, code) {
			return err
		}
		found = true
		if ip {
			return addCIDRs(&rule, v)
		}
		return addDomains(&rule, v, want)
	})
	if err != nil {
		return nil, fmt.Errorf("damaged list: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("no category %q in the list", name)
	}
	if len(rule.Domain)+len(rule.DomainSuffix)+len(rule.DomainKeyword)+len(rule.DomainRegex)+len(rule.IPCIDR) == 0 {
		return nil, fmt.Errorf("category %q is empty", name)
	}
	var buf bytes.Buffer
	plain := option.PlainRuleSet{Rules: []option.HeadlessRule{{Type: C.RuleTypeDefault, DefaultOptions: rule}}}
	if err := srs.Write(&buf, plain, C.RuleSetVersion2); err != nil {
		return nil, err
	}
	b := buf.Bytes()
	if len(b) > MaxSize {
		return nil, errors.New("rule set too large")
	}
	if err := Validate(b, ip); err != nil {
		return nil, err
	}
	return b, nil
}

// fields calls f with every field of the message b, stopping at its first
// error.
func fields(b []byte, f func(num protowire.Number, typ protowire.Type, v []byte) error) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]
		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			return protowire.ParseError(m)
		}
		v := b[:m]
		switch typ {
		case protowire.BytesType:
			v, _ = protowire.ConsumeBytes(v)
		case protowire.VarintType:
		default:
			v = nil
		}
		if err := f(num, typ, v); err != nil {
			return err
		}
		b = b[m:]
	}
	return nil
}

// varint returns the value of a varint field fields passed on.
func varint(v []byte) uint64 {
	x, n := protowire.ConsumeVarint(v)
	if n < 0 {
		return 0
	}
	return x
}

// entryCode returns the country_code of a GeoSite or GeoIP.
func entryCode(entry []byte) (code string, err error) {
	err = fields(entry, func(num protowire.Number, typ protowire.Type, v []byte) error {
		if num == 1 && typ == protowire.BytesType {
			code = string(v)
		}
		return nil
	})
	return code, err
}

// addDomains adds the names of a GeoSite to r, those with every attribute
// of want.
func addDomains(r *option.DefaultHeadlessRule, site []byte, want []string) error {
	return fields(site, func(num protowire.Number, typ protowire.Type, d []byte) error {
		if num != 2 || typ != protowire.BytesType {
			return nil
		}
		kind, value := uint64(domainPlain), ""
		attrs := map[string]bool{}
		err := fields(d, func(num protowire.Number, typ protowire.Type, v []byte) error {
			switch {
			case num == 1 && typ == protowire.VarintType:
				kind = varint(v)
			case num == 2 && typ == protowire.BytesType:
				value = string(v)
			case num == 3 && typ == protowire.BytesType:
				return fields(v, func(num protowire.Number, typ protowire.Type, k []byte) error {
					if num == 1 && typ == protowire.BytesType {
						attrs[strings.ToLower(string(k))] = true
					}
					return nil
				})
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, a := range want {
			if !attrs[a] {
				return nil
			}
		}
		if value == "" {
			return nil
		}
		switch kind {
		case domainPlain:
			r.DomainKeyword = append(r.DomainKeyword, value)
		case domainRegex:
			// v2ray's expressions are Go's; one that does not compile
			// would make sing-box refuse the whole set.
			if _, err := regexp.Compile(value); err == nil {
				r.DomainRegex = append(r.DomainRegex, value)
			}
		case domainSuffix:
			r.DomainSuffix = append(r.DomainSuffix, strings.TrimPrefix(value, "."))
		case domainFull:
			r.Domain = append(r.Domain, value)
		}
		return nil
	})
}

// addCIDRs adds the networks of a GeoIP to r.
func addCIDRs(r *option.DefaultHeadlessRule, geoip []byte) error {
	return fields(geoip, func(num protowire.Number, typ protowire.Type, c []byte) error {
		switch {
		case num == 3 && typ == protowire.VarintType && varint(c) != 0:
			return errors.New("inverted categories are not supported")
		case num != 2 || typ != protowire.BytesType:
			return nil
		}
		var raw []byte
		var bits uint64
		err := fields(c, func(num protowire.Number, typ protowire.Type, v []byte) error {
			switch {
			case num == 1 && typ == protowire.BytesType:
				raw = v
			case num == 2 && typ == protowire.VarintType:
				bits = varint(v)
			}
			return nil
		})
		if err != nil {
			return err
		}
		a, ok := netip.AddrFromSlice(raw)
		if !ok || bits > 128 {
			return nil
		}
		n := int(bits)
		if a.Is4In6() {
			a, n = a.Unmap(), n-96
		}
		// Invalid (more bits than the address has, or fewer than an IPv4
		// one written as IPv6 can have): left out.
		if p := netip.PrefixFrom(a, n); p.IsValid() {
			r.IPCIDR = append(r.IPCIDR, p.Masked().String())
		}
		return nil
	})
}
