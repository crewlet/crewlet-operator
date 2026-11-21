// Package egressproxy is the tier egress proxy: the one exit a tenant
// namespace's pods have to the internet once its NetworkPolicies deny every
// other. It names the tenant and pod behind each connection from the
// connection's source address and the namespace's credential, holds the
// destination to the tier's policy, refuses every address inside the
// platform's networks whatever a name resolves to, and logs one line per
// connection.
package egressproxy

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// Config is one proxy's tier policy, read from its configuration file.
type Config struct {
	// TrustTiers are the values of the namespace trust tier label this
	// proxy serves; a namespace of any other tier is refused.
	TrustTiers []string `json:"trustTiers"`
	// Ports are the destination ports allowed. Empty means 443 and 80.
	Ports []int `json:"ports,omitempty"`
	// AllowHosts, when not empty, are the only destination hosts allowed:
	// an exact name, or *.suffix for every name under suffix.
	AllowHosts []string `json:"allowHosts,omitempty"`
	// DenyHosts are refused even when AllowHosts admits them.
	DenyHosts []string `json:"denyHosts,omitempty"`
	// AllowIPLiterals admits a destination written as an address rather
	// than a name. Off, every destination must be a name, so the log
	// carries what the tenant asked for and host lists apply.
	AllowIPLiterals bool `json:"allowIPLiterals,omitempty"`
	// DeniedCIDRs are refused besides the special-purpose ranges the proxy
	// always refuses: the cluster's own public ranges, say.
	DeniedCIDRs []string `json:"deniedCIDRs,omitempty"`
	// MaxConnectionSeconds ends any connection that lasts longer, so a
	// tunnel cannot be held open indefinitely. Zero means 1800.
	MaxConnectionSeconds int `json:"maxConnectionSeconds,omitempty"`
	// IdleTimeoutSeconds ends a connection with no bytes either way for
	// this long. Zero means 300.
	IdleTimeoutSeconds int `json:"idleTimeoutSeconds,omitempty"`
}

// Policy is a Config checked and made ready to use.
type Policy struct {
	tiers           map[string]bool
	ports           map[int]bool
	allowHosts      []string
	denyHosts       []string
	allowIPLiterals bool
	denied          []netip.Prefix
	maxConnection   time.Duration
	idleTimeout     time.Duration
}

// LoadConfig reads the configuration file at path.
func LoadConfig(path string) (*Policy, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: the path is the proxy's own flag, not request input.
	if err != nil {
		return nil, fmt.Errorf("reading the configuration: %w", err)
	}
	var c Config
	if err := yaml.UnmarshalStrict(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing the configuration %s: %w", path, err)
	}
	return NewPolicy(&c)
}

// NewPolicy checks c and fills in its defaults.
func NewPolicy(c *Config) (*Policy, error) {
	if len(c.TrustTiers) == 0 {
		return nil, errors.New("trustTiers is empty: a proxy that serves no tier would refuse every namespace")
	}
	p := &Policy{
		tiers:           map[string]bool{},
		ports:           map[int]bool{},
		allowIPLiterals: c.AllowIPLiterals,
		maxConnection:   seconds(c.MaxConnectionSeconds, 1800),
		idleTimeout:     seconds(c.IdleTimeoutSeconds, 300),
	}
	for _, t := range c.TrustTiers {
		p.tiers[t] = true
	}
	ports := c.Ports
	if len(ports) == 0 {
		ports = []int{443, 80}
	}
	for _, port := range ports {
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("port %d is not 1 to 65535", port)
		}
		p.ports[port] = true
	}
	var err error
	if p.allowHosts, err = hostPatterns(c.AllowHosts); err != nil {
		return nil, fmt.Errorf("allowHosts: %w", err)
	}
	if p.denyHosts, err = hostPatterns(c.DenyHosts); err != nil {
		return nil, fmt.Errorf("denyHosts: %w", err)
	}
	p.denied = append(p.denied, alwaysDenied...)
	for _, cidr := range c.DeniedCIDRs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, fmt.Errorf("deniedCIDRs: %w", err)
		}
		p.denied = append(p.denied, prefix.Masked())
	}
	return p, nil
}

func seconds(n, fallback int) time.Duration {
	if n <= 0 {
		n = fallback
	}
	return time.Duration(n) * time.Second
}

func hostPatterns(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, h := range in {
		h = normalizeHost(h)
		name := strings.TrimPrefix(h, "*.")
		if name == "" || strings.Contains(name, "*") {
			return nil, fmt.Errorf("%q is neither a name nor *.suffix", h)
		}
		out = append(out, h)
	}
	return out, nil
}
