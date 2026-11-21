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
	"::/96",          // unspecified, loopback, IPv4-compatible
	"100::/64",       // discard-only
	"2001::/32",      // Teredo, which tunnels to any IPv4 address
	"2001:db8::/32",  // documentation
	"2002::/16",      // 6to4, which tunnels to any IPv4 address
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
// An address with an IPv6 zone is refused outright: a zone names a local
// interface, and netip.Prefix.Contains is false for every zoned address, so
// no range below would ever match it.
func (p *Policy) addressDenied(addr netip.Addr) bool {
	if addr.Zone() != "" {
		return true
	}
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

// hostRefusal is why the policy refuses a destination name, or "". A
// single-label name and a name under the cluster's domain are the cluster's
// own: resolved through the pod's search list they become Service and pod
// addresses, which the special-purpose ranges catch only when the cluster
// uses private ones.
func (p *Policy) hostRefusal(host string) string {
	if !strings.Contains(host, ".") {
		return "single-label name not allowed"
	}
	if host == p.clusterDomain || strings.HasSuffix(host, "."+p.clusterDomain) {
		return "cluster name not allowed"
	}
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
