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
// (withPort). A pod annotation replaces the mesh's own setting of the same
// key for the namespace or the mesh, as each mesh documents, so the class's
// list is the only one the operator can keep, and it does.
func leaveLeafUncaptured(annotations map[string]string, keys []string) {
	for _, key := range keys {
		annotations[key] = withPort(annotations[key], render.LeafPort)
	}
}

// withPort is the comma-separated port list list with port added once: its
// entries in their order, trimmed of white space and with the empty ones
// dropped, and port after them unless an entry already is that port.
//
// An entry that is not a port, such as a Linkerd range (7000-8000) or a
// typo, is kept as written, never refused. The operator does not judge a
// mesh's syntax, which differs between meshes (Linkerd reads ranges, Istio
// does not): refusing the class's value would refuse values the mesh reads,
// and would hold back the pods' rollout while the pods already running,
// which may predate the exclusion, stay captured. Kept, the list still
// carries the port as an entry of its own, so whatever the mesh makes of the
// class's entries, the port is in the list it reads. A range that already
// covers the port gains it again, which excludes nothing more.
func withPort(list string, port uint16) string {
	entries := []string{}
	present := false
	for entry := range strings.SplitSeq(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if n, err := strconv.ParseUint(entry, 10, 16); err == nil && n == uint64(port) {
			present = true
		}
		entries = append(entries, entry)
	}
	if !present {
		entries = append(entries, strconv.FormatUint(uint64(port), 10))
	}
	return strings.Join(entries, ",")
}
