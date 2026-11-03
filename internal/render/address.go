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
	"unicode/utf8"
)

// The Tier A keys of the addresses a data pod's engine is told it is reached
// at, which AddressError names.
const (
	FieldExternalURL    = "api.external_url"
	FieldPublicURL      = "api.public.external_url"
	FieldAllowedOrigins = "api.auth.allowed_origins"
	FieldArtifactsURL   = "api.artifacts.external_url"
)

// The variables of an engine pod's environment that hand an address of the
// engine to its sandboxes, which dial it back (Crewlet's mcpbridge.BaseURLVar,
// the tool bridge's, and sandbox.OtelReceiverURLVar, the telemetry
// receiver's), and which AddressError names when Crewlet would refuse to
// start on the address one holds. Crewlet's boot reads a third the same way,
// sandbox.BoxGitURLVar (CREWLET_BOX_GIT_URL), the base a coding box reaches
// the company's code host at, which no pod the operator runs carries: the
// operator does not offer the code host, so it sets none, and the API refuses
// every CREWLET_ variable a Company or a class gives the engine's pods.
const (
	EnvBridgeURL   = "CREWLET_MCP_BRIDGE_URL"
	EnvReceiverURL = "CREWLET_SANDBOX_OTEL_RECEIVER_URL"
)

// BoxURL is an address an engine pod's environment hands its sandboxes: the
// variable, EnvBridgeURL or EnvReceiverURL, and the address it holds.
type BoxURL struct {
	Variable, URL string
}

// ArtifactPlaceholder names an artifact's host label in an artifact origin
// (Crewlet's config.ArtifactPlaceholder).
const ArtifactPlaceholder = "{artifact}"

// The schemes an address may have.
const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// artifactLabelLen is the length of an artifact's host label (Crewlet's
// artifacts.LabelLen).
const artifactLabelLen = 26

// ErrAddressRefused is a data pod's Tier A asked for with an address Crewlet
// refuses to start on (an *AddressError says which), in its crewlet.yaml or in
// the variables that hand it to sandboxes (BootstrapInput.BoxURLs). The API
// refuses such an address where a Company or its class states it, but the
// operator also derives addresses from objects it does not own: the API
// route's host, and the webhooks route's, are the class's templates expanded
// with the Company's namespace and name, and a namespace named 4711 or 0x1f
// under {{name}}.{{namespace}}, or a Company named cafe under
// {{uid}}.0x{{name}}, makes a host whose last label is a number, which
// Crewlet refuses as a browser's address and as one a sandbox dials. So the
// render refuses such an address first, and the caller holds the pods back on
// it rather than start one that crash-loops.
var ErrAddressRefused = errors.New("crewlet refuses to start on an address the data pod's crewlet.yaml or environment names")

// AddressError is the address Crewlet refuses, and why.
type AddressError struct {
	// Field is where the address is written: the Tier A key, FieldExternalURL,
	// FieldPublicURL, FieldArtifactsURL or FieldAllowedOrigins, or the
	// variable that hands it to sandboxes, EnvBridgeURL or EnvReceiverURL.
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
// Company states, and the operator derives none. An artifact origin is judged
// after them, since its verdict reads the other addresses, and then each
// address handed to sandboxes, as the engine's boot reads it
// (engine.ReadBoxURLs): by sandbox.ParseBoxURL's grammar, which a value that
// is set has to meet whatever the company runs.
func refusedAddress(external, public, artifacts string, origins []string, boxes []BoxURL) error {
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
	if artifacts != "" {
		if reason := artifactsURLRefusal(artifacts, external, public, origins); reason != "" {
			return &AddressError{Field: FieldArtifactsURL, Address: artifacts, Reason: reason}
		}
	}
	for _, box := range boxes {
		if reason := boxURLRefusal(box.URL); reason != "" {
			return &AddressError{Field: box.Variable, Address: box.URL, Reason: reason}
		}
	}
	return nil
}

// boxURLRefusal is why Crewlet refuses to boot with raw as an address a
// sandbox dials back to the engine (sandbox.ParseBoxURL), or "": it must be
// an absolute http or https URL, with no user information, a host a
// sandbox's URL parser takes (boxHostRefusal), a port from 1 to 65535 where
// it names one, and no query or fragment, since the engine appends its own
// path to it. White space around it is dropped, and a value of white space
// alone is unset, a feature the engine turns off rather than refuses, which
// the operator never hands a pod.
func boxURLRefusal(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	scheme, _, absolute := strings.Cut(value, "://")
	if !absolute {
		return "is not an absolute URL, with a scheme and a host, which a sandbox dials as it is written"
	}
	if !strings.EqualFold(scheme, schemeHTTP) && !strings.EqualFold(scheme, schemeHTTPS) {
		return "names a scheme a sandbox cannot dial: it must be http or https"
	}
	u, err := url.Parse(value)
	if err != nil {
		return "does not parse as a URL"
	}
	if u.User != nil {
		return "carries user information, a name or password before an @, which every sandbox would be handed"
	}
	if u.Hostname() == "" {
		return "names no host for a sandbox to dial"
	}
	if reason := boxHostRefusal(u); reason != "" {
		return reason
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
			return fmt.Sprintf("has port %s, and a port is 1 to 65535", port)
		}
	}
	if u.RawQuery != "" || u.ForceQuery {
		return "carries a query, which the path the engine appends to it would follow"
	}
	// By the character, because url.Parse records "https://h#" as no
	// fragment at all, and a client sends nothing after a '#'.
	if strings.Contains(value, "#") {
		return "carries a fragment, after which a client sends none of the path the engine appends to it"
	}
	return ""
}

// boxHostRefusal is why Crewlet refuses the host url.Parse read out of an
// address a sandbox dials (sandbox's boxHostRefusal), or "": the host the URL
// parser of a sandbox's coding agent takes, the WHATWG URL Standard's, which
// refuses an IPv6 zone, a host that is not UTF-8 once its escapes are
// decoded, one holding a forbidden domain code point, and one whose last
// label is a number and that is no IPv4 address. Net/url, the API server's
// URL parser and the engine's Tier A check, takes each.
func boxHostRefusal(u *url.URL) string {
	host := u.Hostname()
	if strings.HasPrefix(u.Host, "[") {
		if strings.Contains(host, "%") {
			return "names an IPv6 host with a zone, which the URL parser of a sandbox's coding agent refuses"
		}
		return ""
	}
	if !utf8.ValidString(host) {
		return "names a host that is not valid UTF-8, a % escape in it decoding to bytes that spell no character, " +
			"which the URL parser of a sandbox's coding agent refuses"
	}
	if strings.ContainsFunc(host, forbiddenInADomain) {
		return "names a host holding a character no host may hold, a space, a control character or one of " +
			"# % / : < > ? @ [ \\ ] ^ |, which the URL parser of a sandbox's coding agent refuses"
	}
	if endsInANumber(host) && !isIPv4Address(host) {
		return "names a host whose last label is a number, which the URL parser of a sandbox's coding agent reads " +
			"as an IPv4 address, and it is none: write an address as four numbers from 0 to 255 joined by dots, " +
			"and a host name whose last label is not a number"
	}
	return ""
}

// forbiddenInADomain is the WHATWG URL Standard's forbidden domain code point
// (sandbox's forbiddenInADomain): a C0 control, space, DEL, or one of
// # % / : < > ? @ [ \ ] ^ |.
func forbiddenInADomain(r rune) bool {
	return r <= ' ' || r == 0x7f || strings.ContainsRune(`#%/:<>?@[\]^|`, r)
}

// endsInANumber is the standard's ends-in-a-number checker (sandbox's
// endsInANumber): whether a name's last label, one trailing dot ignored, is
// decimal digits, or any other part ipv4Number reads.
func endsInANumber(name string) bool {
	labels := strings.Split(name, ".")
	if labels[len(labels)-1] == "" {
		if len(labels) == 1 {
			return false
		}
		labels = labels[:len(labels)-1]
	}
	last := labels[len(labels)-1]
	if last != "" && strings.Trim(last, "0123456789") == "" {
		return true
	}
	_, ok := ipv4Number(last)
	return ok
}

// isIPv4Address is whether the standard's IPv4 parser reads name as an
// address (sandbox's isIPv4Address): at most four dot-separated parts, one
// trailing dot ignored, each a number ipv4Number reads, every one but the last
// at most 255 and the last below 256 to the power of the bytes left for it.
func isIPv4Address(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 4 {
		return false
	}
	for i, part := range parts {
		n, ok := ipv4Number(part)
		if !ok {
			return false
		}
		if i < len(parts)-1 && n > 255 {
			return false
		}
		if i == len(parts)-1 && n >= 1<<(8*(5-len(parts))) {
			return false
		}
	}
	return true
}

// ipv4Number is the standard's IPv4 number parser (sandbox's ipv4Number): a
// part in hexadecimal after 0x or 0X, in octal after a leading 0, and in
// decimal otherwise, 0x alone being zero; false for an empty part or a digit
// outside its radix. The value stops at 2^32, past every bound an address
// puts on a part.
func ipv4Number(part string) (n uint64, ok bool) {
	if part == "" {
		return 0, false
	}
	radix := uint64(10)
	switch {
	case len(part) >= 2 && (part[:2] == "0x" || part[:2] == "0X"):
		part, radix = part[2:], 16
	case len(part) >= 2 && part[0] == '0':
		part, radix = part[1:], 8
	}
	const saturated = 1 << 32
	for i := range len(part) {
		var d uint64
		switch c := part[i]; {
		case '0' <= c && c <= '9':
			d = uint64(c - '0')
		case 'a' <= c && c <= 'f':
			d = uint64(c-'a') + 10
		case 'A' <= c && c <= 'F':
			d = uint64(c-'A') + 10
		default:
			return 0, false
		}
		if d >= radix {
			return 0, false
		}
		n = min(n*radix+d, saturated)
	}
	return n, true
}

// artifactsURLRefusal is why Crewlet refuses raw as api.artifacts.external_url
// beside the node's other addresses (config.APIArtifacts.validate), or "".
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

// artifactTemplateRefusal is why Crewlet refuses where raw names
// ArtifactPlaceholder (config.checkArtifactTemplate), or "".
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

// artifactHostShape reports whether the template gives some artifact host
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

// artifactLoopbackHost is config.loopbackHost: localhost, a name under it, or
// a loopback IP.
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
