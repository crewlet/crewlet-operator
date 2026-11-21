package egressproxy

import (
	"net/netip"
	"strings"
)

// alwaysDenied are the special-purpose ranges no tier may reach through the
// proxy: the node and cluster networks live in them, and so do the cloud
// metadata endpoints (169.254.169.254, fd00:ec2::254), which hand out the
// node's credentials. A NetworkPolicy keeps tenant pods from these ranges;
// without this list the proxy would be the way around it.
var alwaysDenied = mustPrefixes(
	"0.0.0.0/8",      // this network, unspecified
	"10.0.0.0/8",     // private
	"100.64.0.0/10",  // shared address space (CGNAT)
	"127.0.0.0/8",    // loopback
	"169.254.0.0/16", // link-local, metadata
	"172.16.0.0/12",  // private
	"192.0.0.0/24",   // IETF protocol assignments
	"192.168.0.0/16", // private
	"198.18.0.0/15",  // benchmarking
	"224.0.0.0/4",    // multicast
	"240.0.0.0/4",    // reserved, broadcast
	"::/128",         // unspecified
	"::1/128",        // loopback
	"64:ff9b:1::/48", // local-use NAT64
	"fc00::/7",       // unique local, metadata
	"fe80::/10",      // link-local
	"ff00::/8",       // multicast
)

// nat64 is the well-known NAT64 prefix: its addresses reach the IPv4 address
// in their last 32 bits, which is checked as that address.
var nat64 = netip.MustParsePrefix("64:ff9b::/96")

func mustPrefixes(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(cidrs))
	for i, c := range cidrs {
		out[i] = netip.MustParsePrefix(c)
	}
	return out
}

// addressDenied reports whether the policy refuses a destination address.
func (p *Policy) addressDenied(addr netip.Addr) bool {
	addr = addr.Unmap()
	if nat64.Contains(addr) {
		b := addr.As16()
		addr = netip.AddrFrom4([4]byte(b[12:]))
	}
	for _, prefix := range p.denied {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// hostRefusal is why the policy refuses a destination host, or "".
func (p *Policy) hostRefusal(host string) string {
	if matchAny(p.denyHosts, host) {
		return "host denied"
	}
	if len(p.allowHosts) > 0 && !matchAny(p.allowHosts, host) {
		return "host not allowed"
	}
	return ""
}

func matchAny(patterns []string, host string) bool {
	for _, pat := range patterns {
		if suffix, ok := strings.CutPrefix(pat, "*."); ok {
			if strings.HasSuffix(host, "."+suffix) {
				return true
			}
		} else if host == pat {
			return true
		}
	}
	return false
}

// normalizeHost lowercases a host and drops a trailing dot, so that
// Example.COM. and example.com meet the same lists.
func normalizeHost(h string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
}
