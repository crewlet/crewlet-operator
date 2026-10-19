package render

import (
	"errors"
	"fmt"
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
)

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
	// FieldPublicURL or FieldAllowedOrigins.
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
// TestMirroredEngineFunctionsAreUnchanged holds the code of: api.external_url
// as config.API.validateExternalURL judges it (checkExternalURL, then
// checkBrowserHost), api.public.external_url as config.APIPublic.validate
// does (checkExternalURL alone: no browser compares an origin with it), and
// each api.auth.allowed_origins entry as config.checkOrigin does. Each is
// judged with the white space around it removed, since the engine trims every
// string of its Tier A before it judges any (config.Bootstrap.normalize). A
// ${NAME} reference, which the engine would resolve before it judges the
// address, is not read as one: the API refuses one in every address a
// Company states, and the operator derives none.
func refusedAddress(external, public string, origins []string) error {
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
	return nil
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
