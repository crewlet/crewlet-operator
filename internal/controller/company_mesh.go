package controller

import (
	"strconv"
	"strings"

	"github.com/crewlet/crewlet-operator/internal/render"
)

// The pod annotations by which a service mesh's sidecar injector leaves a
// port out of the traffic it captures, each a comma-separated list of ports.
// The operator writes them on both ends of a seat pod's leaf link
// (render.LeafPort): Crewlet reads whether that link is moving off the
// link's own socket, what it has read and what the far end has acknowledged
// (crewlet/crewlet#1430), and a sidecar on either end terminates the TCP
// connection, acknowledging bytes the far end has not taken. A node behind
// one would judge a link stalled in a buffer as moving, or one draining as
// still. Crewlet's satellite nodes guide states the requirement
// (docs/guides/satellite-nodes.md); the operator renders both ends, so it
// meets it on every pod rather than asking each platform to.
const (
	// Istio, https://istio.io/latest/docs/reference/config/annotations/
	// (traffic.sidecar.istio.io/excludeInboundPorts and
	// excludeOutboundPorts).
	istioExcludeInboundPorts  = "traffic.sidecar.istio.io/excludeInboundPorts"
	istioExcludeOutboundPorts = "traffic.sidecar.istio.io/excludeOutboundPorts"
	// Linkerd, https://linkerd.io/2-edge/reference/proxy-configuration/
	// (config.linkerd.io/skip-inbound-ports and skip-outbound-ports).
	linkerdSkipInboundPorts  = "config.linkerd.io/skip-inbound-ports"
	linkerdSkipOutboundPorts = "config.linkerd.io/skip-outbound-ports"
)

// leafInbound and leafOutbound are the mesh annotations that leave the leaf
// port out of capture on the end that accepts the link (a data pod) and on
// the end that dials it (a seat pod).
var (
	leafInbound  = []string{istioExcludeInboundPorts, linkerdSkipInboundPorts}
	leafOutbound = []string{istioExcludeOutboundPorts, linkerdSkipOutboundPorts}
)

// leaveLeafUncaptured adds render.LeafPort to each of keys in annotations,
// merged into the list the class's podTemplate may already give it
// (withPort).
func leaveLeafUncaptured(annotations map[string]string, keys []string) {
	for _, key := range keys {
		annotations[key] = withPort(annotations[key], render.LeafPort)
	}
}

// withPort is the comma-separated port list list with port added once: its
// entries in their order, trimmed of white space and with the empty ones
// dropped, and port after them unless an entry already is that port, written
// as port is, in decimal with no leading zero.
//
// An entry spelt any other way (07422, a range such as 7000-8000 that covers
// port) does not count as port: Istio hands each entry to iptables as
// written, which reads 07422 as an octal number, another port, so only
// port's own spelling is known to exclude it. Port added beside such an entry
// excludes nothing more in either mesh.
//
// An entry that is not a port at all, a Linkerd range or a typo, is kept as
// written, never refused or dropped. Each mesh owns the syntax of its own
// keys, and the two differ (Linkerd reads ranges, Istio's injector refuses a
// pod whose list holds anything but ports): the mesh judges the class's
// value at the pod's admission, as it does without the operator, and the
// StatefulSet's events say what it refused. Rewriting the list into what
// the operator guesses the mesh reads would change what the class's author
// wrote for a mesh the operator does not run.
func withPort(list string, port uint16) string {
	own := strconv.FormatUint(uint64(port), 10)
	entries := []string{}
	present := false
	for entry := range strings.SplitSeq(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		present = present || entry == own
		entries = append(entries, entry)
	}
	if !present {
		entries = append(entries, own)
	}
	return strings.Join(entries, ",")
}
