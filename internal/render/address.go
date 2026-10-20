package render

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// The Tier A keys of the addresses a data pod's engine is told it is reached
// at, which AddressError names.
const (
	FieldExternalURL    = "api.external_url"
	FieldPublicURL      = "api.public.external_url"
	FieldAllowedOrigins = "api.auth.allowed_origins"
	FieldArtifactsURL   = "api.artifacts.external_url"
)

// ArtifactPlaceholder is what an artifact origin's address names an
// artifact's host label with (Crewlet's config.ArtifactPlaceholder): present,
// at the start of the host, every artifact is reached on a host of its own.
const ArtifactPlaceholder = "{artifact}"

// The schemes an address a node is reached at may have.
const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// artifactLabelLen is the length of every artifact's host label (Crewlet's
// artifacts.LabelLen), which the placeholder stands for.
const artifactLabelLen = 26

// ErrAddressRefused is a data pod's Tier A asked for with an address Crewlet
// refuses to start on (an *AddressError says which). The API refuses such an
// address where a Company or its class states it, but the operator also
// derives addresses from objects it does not own: the API route's host is the
// class's template expanded with the Company's namespace and name, and a
// namespace named 4711 or 0x1f under {{name}}.{{namespace}}, or a Company
// named cafe under {{uid}}.0x{{name}}, makes a host whose last label is a
// number, which Crewlet refuses as a browser's address. So the render refuses
// such an address first, and the caller holds the pods back on it rather than
// start one that crash-loops.
var ErrAddressRefused = errors.New("crewlet refuses to start on an address the data pod's crewlet.yaml names")

// AddressError is the address Crewlet refuses, and why.
type AddressError struct {
	// Field is the Tier A key the address is written to: FieldExternalURL,
	// FieldPublicURL, FieldArtifactsURL or FieldAllowedOrigins.
	Field string
	// Address is the address as the render was given it.
	Address string
	// Reason is why Crewlet refuses it, a clause that follows the address.
	Reason string
}

func (e *AddressError) Error() string {
	return fmt.Sprintf("%v: %s %q %s", ErrAddressRefused, e.Field, e.Address, e.Reason)
}

// Unwrap makes an AddressError ErrAddressRefused for errors.Is.
func (e *AddressError) Unwrap() error { return ErrAddressRefused }

// refusedAddress is the first of a data pod's addresses Crewlet refuses to
// start on, or nil.
//
// It is Crewlet's own judgement, copied, which `crewlet validate` shows and
// TestMirroredEngineDeclarationsAreUnchanged holds the code of: api.external_url
// as config.API.validateExternalURL judges it (checkExternalURL, then
// checkBrowserHost), api.public.external_url as config.APIPublic.validate
// does (checkExternalURL alone: no browser compares an origin with it), and
// each api.auth.allowed_origins entry as config.checkOrigin does. Each is
// judged with the white space around it removed, since the engine trims every
// string of its Tier A before it judges any (config.Bootstrap.normalize). A
// ${NAME} reference, which the engine would resolve before it judges the
// address, is not read as one: the API refuses one in every address a
// Company states, and the operator derives none. api.artifacts.external_url,
// where the data pod opens the artifact listener at all, is judged last
// (artifactsURLRefusal), since its verdict reads the other addresses.
func refusedAddress(external, public, artifacts string, origins []string) error {
	if reason := externalURLRefusal(strings.TrimSpace(external), true); reason != "" {
		return &AddressError{Field: FieldExternalURL, Address: external, Reason: reason}
	}
	if reason := externalURLRefusal(strings.TrimSpace(public), false); reason != "" {
		return &AddressError{Field: FieldPublicURL, Address: public, Reason: reason}
	}
	for _, origin := range origins {
		if reason := originRefusal(strings.TrimSpace(origin)); reason != "" {
			return &AddressError{Field: FieldAllowedOrigins, Address: origin, Reason: reason}
		}
	}
	if artifacts == "" {
		return nil
	}
	if reason := artifactsURLRefusal(artifacts, external, public, origins); reason != "" {
		return &AddressError{Field: FieldArtifactsURL, Address: artifacts, Reason: reason}
	}
	return nil
}

// artifactsURLRefusal is why Crewlet refuses raw as the address a browser
// reaches the artifact origin at, on a node that opens the artifact listener
// and serves the API at external and its public listener at public, with the
// browser origins origins beside external's own (config.APIArtifacts.validate,
// with checkArtifactTemplate and checkCollisions), or "".
//
// The artifact origin is where every artifact, a page a person or an agent
// wrote, runs, so Crewlet refuses one a browser would treat as the API's: the
// dashboard's or the public listener's own origin, an origin the API admits
// writes from, a parent of the API's host (a cookie scoped to it would reach
// the API), and for an origin per artifact, a template that would give some
// artifact the API's host.
func artifactsURLRefusal(raw, external, public string, origins []string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "is empty, and a node that opens the artifact listener needs the address browsers reach it at"
	}
	perArtifact := strings.Contains(raw, ArtifactPlaceholder)
	if strings.Count(raw, ArtifactPlaceholder) > 1 {
		return "names " + ArtifactPlaceholder + " more than once, and an artifact's host carries its label once"
	}
	if perArtifact {
		if reason := artifactTemplateRefusal(raw); reason != "" {
			return reason
		}
	}
	rendered := strings.Replace(raw, ArtifactPlaceholder, strings.Repeat("a", artifactLabelLen), 1)
	if reason := externalURLRefusal(rendered, false); reason != "" {
		return reason
	}
	parsed, err := url.Parse(rendered)
	if err != nil {
		return fmt.Sprintf("is not a URL (%v)", err)
	}
	if strings.Trim(parsed.Path, "/") != "" {
		return "has a path, and the artifact origin serves its routes from the root of its host"
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme == schemeHTTP && !artifactLoopbackHost(host) {
		return "uses plain http on a routable host, so every byte an artifact's page loads would travel in the clear"
	}
	apex := host
	if perArtifact {
		_, apex, _ = strings.Cut(host, ".")
	}
	origin := parsed.Scheme + "://" + parsed.Host
	for _, other := range []string{external, public} {
		theirs, err := url.Parse(strings.TrimRight(strings.TrimSpace(other), "/"))
		if other == "" || err != nil || theirs.Hostname() == "" {
			continue
		}
		theirHost := strings.ToLower(theirs.Hostname())
		switch {
		case !perArtifact && strings.EqualFold(theirs.Scheme+"://"+theirs.Host, origin):
			return "is the origin " + other + " is served on, and an artifact must never run where the API's session runs"
		case perArtifact && artifactHostShape(theirHost, raw, apex):
			return "would give an artifact the host " + other + " is served on"
		case theirHost != apex && strings.HasSuffix(theirHost, "."+apex) && !perArtifact:
			return "is a parent domain of " + other + ", so a cookie scoped to it would reach the API"
		}
	}
	if !perArtifact {
		for _, allowed := range origins {
			if strings.EqualFold(strings.TrimRight(strings.TrimSpace(allowed), "/"), origin) {
				return "is an api.auth.allowed_origins entry, so the API would admit writes and sockets from artifact pages"
			}
		}
	}
	return ""
}

// artifactTemplateRefusal is why Crewlet refuses an artifact origin that names
// ArtifactPlaceholder anywhere but its one legal position, the start of the
// host's first label, followed only by label characters, the whole label
// fitting the 63 bytes a DNS label holds (config.checkArtifactTemplate), or "".
func artifactTemplateRefusal(raw string) string {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok || (scheme != schemeHTTPS && scheme != schemeHTTP) {
		return "does not start with https:// or http://"
	}
	if !strings.HasPrefix(rest, ArtifactPlaceholder) {
		return "does not begin its host with " + ArtifactPlaceholder + ", where an artifact's label goes"
	}
	label := strings.TrimPrefix(rest, ArtifactPlaceholder)
	i := strings.IndexAny(label, ".:/")
	switch {
	case i < 0:
		return "has no parent domain after the artifact's label"
	case label[i] != '.':
		return "puts " + ArtifactPlaceholder + " in the whole host, and an artifact host needs a parent domain"
	}
	label = label[:i]
	if strings.IndexFunc(label, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-'
	}) >= 0 {
		return "follows " + ArtifactPlaceholder + " with characters a host label does not hold, which are lowercase letters, digits and hyphens"
	}
	if n := artifactLabelLen + len(label); n > 63 {
		return fmt.Sprintf("makes every artifact's label %d bytes long, over the 63 a DNS label holds", n)
	}
	return ""
}

// artifactHostShape reports whether host is one the template would give some
// artifact: directly under the apex, with a first label of an artifact's
// label's length followed by the template's own suffix
// (config.artifactHostShape).
func artifactHostShape(host, template, apex string) bool {
	first, rest, ok := strings.Cut(host, ".")
	if !ok || rest != apex {
		return false
	}
	_, after, _ := strings.Cut(template, ArtifactPlaceholder)
	suffix, _, _ := strings.Cut(after, ".")
	return len(first) == artifactLabelLen+len(suffix) && strings.HasSuffix(first, suffix)
}

// artifactLoopbackHost reports whether a browser reaches host on its own
// machine: a loopback address, localhost or a name under localhost
// (config.loopbackHost).
func artifactLoopbackHost(host string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// externalURLRefusal is why Crewlet refuses raw as an address a node is
// reached at (config.checkExternalURL, and config.checkBrowserHost when a
// browser reaches it), or "".
func externalURLRefusal(raw string, browser bool) string {
	if raw == "" {
		return "is empty, and a node that serves the API, or opens its public listener, needs the address it is reached at"
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return "does not start with http:// or https://, in lower case"
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Sprintf("is not a URL (%v)", err)
	}
	if parsed.Hostname() == "" {
		return "has no host"
	}
	if reason := portRefusal(parsed.Port()); reason != "" {
		return reason
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "has a query or a fragment, onto which no path can be appended"
	}
	if browser {
		return browserHostRefusal(parsed.Hostname())
	}
	return ""
}

// originRefusal is why Crewlet refuses origin as an api.auth.allowed_origins
// entry (config.checkOrigin), or "".
func originRefusal(origin string) string {
	switch {
	case origin == "":
		return "is empty, and matches nothing"
	case origin == "*":
		return "would let any website act as the signed-in person"
	case !strings.HasPrefix(origin, "http://") && !strings.HasPrefix(origin, "https://"):
		return "has no http:// or https:// scheme, and matches nothing"
	case strings.HasSuffix(origin, "/"):
		return "ends in a slash, and an origin has no path"
	}
	parsed, err := url.Parse(origin)
	switch {
	case err != nil:
		return fmt.Sprintf("is not a URL a browser can be at (%v)", err)
	case parsed.Hostname() == "":
		return "has no host"
	case parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.User != nil:
		return "carries more than an origin, which is only scheme://host[:port]"
	}
	if reason := portRefusal(parsed.Port()); reason != "" {
		return reason
	}
	return browserHostRefusal(parsed.Hostname())
}

// portRefusal is why Crewlet refuses a URL's port (config.checkPort), or "".
// The URL parser checks only that a port is digits.
func portRefusal(port string) string {
	if port == "" {
		return ""
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Sprintf("has port %s, and a port is 1 to 65535", port)
	}
	return ""
}

// browserHostRefusal is why Crewlet refuses host as the host of an address a
// browser reaches (config.checkBrowserHost), or "": a host the engine could
// not compare an origin with, because a browser spells it otherwise when it
// sends it.
func browserHostRefusal(host string) string {
	switch {
	case strings.IndexFunc(host, func(r rune) bool { return r > unicode.MaxASCII }) >= 0:
		return "has a host outside ASCII, which a browser sends in its ASCII (xn--) form; write that form"
	case strings.Contains(host, "%"):
		return "names an IPv6 zone, which no browser can address; write the address without it"
	case !strings.Contains(host, ":") && endsInNumber(host):
		if addr, err := netip.ParseAddr(host); err != nil || !addr.Is4() {
			return "has a host a browser reads as an IPv4 address, since its last label is a number, and rewrites " +
				"before it sends it; write an IPv4 address as four decimal octets, such as 127.0.0.1, and a host name " +
				"whose last label is not a number"
		}
	}
	return ""
}

// endsInNumber is the URL standard's test for a host a browser parses as
// IPv4, as Crewlet takes it (config.endsInNumber): its last label, a trailing
// dot aside, is all digits, or 0x and hex digits.
func endsInNumber(host string) bool {
	labels := strings.Split(host, ".")
	if len(labels) > 1 && labels[len(labels)-1] == "" {
		labels = labels[:len(labels)-1]
	}
	last := strings.ToLower(labels[len(labels)-1])
	if last != "" && strings.Trim(last, "0123456789") == "" {
		return true
	}
	return strings.HasPrefix(last, "0x") && strings.Trim(last[2:], "0123456789abcdef") == ""
}
