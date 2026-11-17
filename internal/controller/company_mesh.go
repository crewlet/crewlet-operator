package controller

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
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

// namespacedMeshPorts are the annotations a mesh also reads off a pod's
// namespace: Linkerd's proxy injector takes a pod's value of one of these
// keys over its namespace's, and its namespace's over its installation's.
// Istio reads its keys off the pod alone, its installation's defaults aside.
var namespacedMeshPorts = []string{linkerdSkipInboundPorts, linkerdSkipOutboundPorts}

// namespaceMeshPorts is the Company's namespace's values of
// namespacedMeshPorts, the ones it sets: what a mesh would read for a pod
// that gives none of its own, which leaveLeafUncaptured merges the leaf port
// into rather than replace.
func (r *CompanyReconciler) namespaceMeshPorts(ctx context.Context, c *crewletaiv1alpha1.Company) (map[string]string, error) {
	ns := &metav1.PartialObjectMetadata{}
	ns.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Namespace"))
	if err := r.Get(ctx, types.NamespacedName{Name: c.Namespace}, ns); err != nil {
		return nil, fmt.Errorf("reading Namespace %s: %w", c.Namespace, err)
	}
	ports := map[string]string{}
	for _, key := range namespacedMeshPorts {
		if list := ns.Annotations[key]; list != "" {
			ports[key] = list
		}
	}
	return ports, nil
}

// namespaceChanged passes a namespace update its Companies follow: one that
// changes its labels, which decide whether its Companies' classes admit them
// (spec.allowedNamespaces), or its mesh port lists (meshPortsChanged).
func namespaceChanged() predicate.Predicate {
	return predicate.Or[client.Object](predicate.LabelChangedPredicate{}, meshPortsChanged())
}

// meshPortsChanged passes a namespace update that changes one of
// namespacedMeshPorts, which the engine pods of its Companies carry merged
// with the leaf port. A namespace's creation and deletion are the label
// predicate's to pass.
func meshPortsChanged() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return false },
		DeleteFunc: func(event.DeleteEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return false
			}
			old, updated := e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations()
			return slices.ContainsFunc(namespacedMeshPorts, func(key string) bool { return old[key] != updated[key] })
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// leaveLeafUncaptured adds render.LeafPort to each of keys in annotations,
// merged (withPort) into the list the mesh would read for the pod without
// it: the class's podTemplate's, else, for a key a mesh also reads off the
// namespace (namespacedMeshPorts), the namespace's, of namespace. Writing
// the key on the pod replaces both for the mesh, so this is what keeps them.
// What a mesh's installation sets for every pod (Linkerd's
// proxyInit.ignoreOutboundPorts, Istio's global.proxy.excludeOutboundPorts)
// is replaced too, and the operator cannot read it: a class restates it.
func leaveLeafUncaptured(annotations, namespace map[string]string, keys []string) {
	for _, key := range keys {
		list := annotations[key]
		// Linkerd reads an empty value as none and falls back to the
		// namespace's, so an empty one here does too.
		if list == "" && slices.Contains(namespacedMeshPorts, key) {
			list = namespace[key]
		}
		annotations[key] = withPort(list, render.LeafPort)
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
