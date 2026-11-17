package controller

import (
	"maps"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The annotations each mesh reads, spelled as its documentation spells them
// rather than through the operator's constants, so that a misspelt constant
// fails here instead of leaving the port captured.
const (
	istioInbound    = "traffic.sidecar.istio.io/excludeInboundPorts"
	istioOutbound   = "traffic.sidecar.istio.io/excludeOutboundPorts"
	linkerdInbound  = "config.linkerd.io/skip-inbound-ports"
	linkerdOutbound = "config.linkerd.io/skip-outbound-ports"
)

// meshClass is a class whose seat pools are policy, carrying annotations on
// every engine pod.
func meshClass(policy crewletaiv1alpha1.SeatPoolPolicy, annotations map[string]string) *crewletaiv1alpha1.EngineClass {
	class := &crewletaiv1alpha1.EngineClass{}
	class.Spec.Data.Storage.Size = new(resource.MustParse("16Gi"))
	class.Spec.SeatPools.Policy = policy
	class.Spec.PodTemplate.Metadata.Annotations = annotations
	return class
}

// TestTheLeafLinkIsLeftOutOfAMeshsCapture holds both ends of a seat pod's
// leaf link out of a service mesh's capture, in Istio's and Linkerd's
// annotations alike: a data pod whose leaf listener is open excludes the
// leaf port inbound, and a seat pod excludes it outbound. The seat pod reads
// whether its link moves off the link's own socket (crewlet/crewlet#1430),
// and a sidecar on either end terminates the TCP connection and acknowledges
// what the far end has not yet taken. A data pod that opens no leaf listener
// excludes nothing, and neither pod excludes the port in the direction its
// link does not take.
func TestTheLeafLinkIsLeftOutOfAMeshsCapture(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		pod      podOfClass
		policy   crewletaiv1alpha1.SeatPoolPolicy
		excluded []string
		left     []string
	}{
		{
			"a data pod whose leaf listener is open", dataPodOf, crewletaiv1alpha1.SeatPoolsAllowed,
			[]string{istioInbound, linkerdInbound}, []string{istioOutbound, linkerdOutbound},
		},
		{
			"a data pod of a class that allows no seat pools", dataPodOf, crewletaiv1alpha1.SeatPoolsForbidden,
			nil, []string{istioInbound, linkerdInbound, istioOutbound, linkerdOutbound},
		},
		{
			"a seat pod, which dials the link", seatPodOf, crewletaiv1alpha1.SeatPoolsAllowed,
			[]string{istioOutbound, linkerdOutbound}, []string{istioInbound, linkerdInbound},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			pod := c.pod(t, meshClass(c.policy, nil))
			for _, key := range c.excluded {
				if got := pod.Annotations[key]; got != "7422" {
					t.Errorf("annotation %s is %q, want %q", key, got, "7422")
				}
			}
			for _, key := range c.left {
				if got, ok := pod.Annotations[key]; ok {
					t.Errorf("annotation %s is %q, want it unset", key, got)
				}
			}
		})
	}
}

// TestAMeshExclusionKeepsTheClasssPorts holds the leaf port's exclusion to
// a merge with the list a class's podTemplate already gives the same
// annotation: the class's entries stay, in their order, trimmed of white
// space and with empty ones dropped, and the leaf port is added once, after
// them, unless an entry already is the leaf port as the operator writes it
// (7422, never 07422, which iptables reads as octal). An entry that is no
// port (a Linkerd range, a typo) is kept as written beside the leaf port's
// own, never judged: each mesh judges its own syntax. The class's
// annotations are a copy's to change, never the class's own, and an
// exclusion in the direction a pod's link does not take is the class's
// alone, left as written.
func TestAMeshExclusionKeepsTheClasssPorts(t *testing.T) {
	t.Parallel()
	merges := []struct {
		name, class, want string
	}{
		{"an empty list", "", "7422"},
		{"one port", "15020", "15020,7422"},
		{"ports with white space", " 80 , 443 ", "80,443,7422"},
		{"empty entries", "80,,443,", "80,443,7422"},
		{"the leaf port first", "7422,80", "7422,80"},
		{"the leaf port among others", "80, 7422 ,443", "80,7422,443"},
		// Istio hands 07422 to iptables as written, which reads it as an
		// octal number: another port.
		{"the leaf port with a leading zero", "80,07422", "80,07422,7422"},
		{"a range that covers the leaf port", "7000-8000,80", "7000-8000,80,7422"},
		{"a range", "7000-8000", "7000-8000,7422"},
		{"an entry that is no port", "80,http", "80,http,7422"},
	}
	for _, kind := range []struct {
		name          string
		pod           podOfClass
		keys, foreign []string
	}{
		{"data pod", dataPodOf, []string{istioInbound, linkerdInbound}, []string{istioOutbound, linkerdOutbound}},
		{"seat pod", seatPodOf, []string{istioOutbound, linkerdOutbound}, []string{istioInbound, linkerdInbound}},
	} {
		for _, m := range merges {
			t.Run(kind.name+", "+m.name, func(t *testing.T) {
				t.Parallel()
				annotations := map[string]string{"example.com/team": "team-a"}
				for _, key := range kind.keys {
					annotations[key] = m.class
				}
				for _, key := range kind.foreign {
					annotations[key] = " 9090 "
				}
				written := maps.Clone(annotations)
				class := meshClass(crewletaiv1alpha1.SeatPoolsAllowed, annotations)

				pod := kind.pod(t, class)
				for _, key := range kind.keys {
					if got := pod.Annotations[key]; got != m.want {
						t.Errorf("the class sets %s to %q, and the pod carries %q, want %q", key, m.class, got, m.want)
					}
				}
				for _, key := range kind.foreign {
					if got := pod.Annotations[key]; got != " 9090 " {
						t.Errorf("the class sets %s to %q, and the pod carries %q, want the class's value", key, " 9090 ", got)
					}
				}
				if got := pod.Annotations["example.com/team"]; got != "team-a" {
					t.Errorf("the class's own annotation is %q on the pod, want %q", got, "team-a")
				}
				if !maps.Equal(class.Spec.PodTemplate.Metadata.Annotations, written) {
					t.Errorf("rendering the pod changed the class's annotations to %v, from %v",
						class.Spec.PodTemplate.Metadata.Annotations, written)
				}
			})
		}
	}
}
