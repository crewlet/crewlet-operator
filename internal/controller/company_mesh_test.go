package controller

import (
	"maps"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
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

// TestAMeshExclusionKeepsTheNamespacesLinkerdPorts holds the leaf port's
// exclusion to the list Linkerd would read for the pod without it: Linkerd's
// injector takes a pod's skip list over its namespace's, so a pod that
// carries the key loses the namespace's ports unless they are merged in. The
// namespace's list is the base where the class gives none (or an empty one,
// which Linkerd reads as none), and the class's wins where it gives one, as
// it would without the operator. Istio reads no namespace's value, so an
// Istio key the namespace map carries is never merged.
func TestAMeshExclusionKeepsTheNamespacesLinkerdPorts(t *testing.T) {
	t.Parallel()
	company := &crewletaiv1alpha1.Company{Namespace: testTenant, Name: companyName}
	pods := []struct {
		name           string
		of             func(*crewletaiv1alpha1.EngineClass, map[string]string) (*corev1ac.PodTemplateSpecApplyConfiguration, error)
		linkerd, istio string
	}{
		{
			"data pod",
			func(class *crewletaiv1alpha1.EngineClass, ns map[string]string) (*corev1ac.PodTemplateSpecApplyConfiguration, error) {
				return podTemplate(company, class, &workload{
					image: engineImageV020, bootstrap: &render.BootstrapDocument{}, startupSeconds: 1742, meshPorts: ns,
				})
			},
			linkerdInbound, istioInbound,
		},
		{
			"seat pod",
			func(class *crewletaiv1alpha1.EngineClass, ns map[string]string) (*corev1ac.PodTemplateSpecApplyConfiguration, error) {
				plan := render.PoolPlanOf(&crewletaiv1alpha1.SeatPool{Name: poolGeneral}, class.Spec.SeatPools.Defaults)
				return seatPodTemplate(company, class, &seatWorkload{name: "nimbus-seats-general", pool: poolGeneral, plan: &plan},
					"nimbus-seats-general-bootstrap", engineImageV020, &render.BootstrapDocument{}, "", ns)
			},
			linkerdOutbound, istioOutbound,
		},
	}
	for _, pod := range pods {
		for _, c := range []struct {
			name                   string
			class, namespace       map[string]string
			wantLinkerd, wantIstio string
		}{
			{"the namespace's list, where the class gives none", nil, map[string]string{pod.linkerd: "443,25"}, "443,25,7422", "7422"},
			{"the class's list, over the namespace's", map[string]string{pod.linkerd: "5432"}, map[string]string{pod.linkerd: "443"}, "5432,7422", "7422"},
			{"the namespace's list, where the class's is empty", map[string]string{pod.linkerd: ""}, map[string]string{pod.linkerd: "443"}, "443,7422", "7422"},
			{"no Istio list from the namespace", nil, map[string]string{pod.istio: "1234"}, "7422", "7422"},
			{"no namespace at all", nil, nil, "7422", "7422"},
		} {
			t.Run(pod.name+", "+c.name, func(t *testing.T) {
				t.Parallel()
				namespace := maps.Clone(c.namespace)
				got := decodedPod(t)(pod.of(meshClass(crewletaiv1alpha1.SeatPoolsAllowed, maps.Clone(c.class)), namespace))
				if v := got.Annotations[pod.linkerd]; v != c.wantLinkerd {
					t.Errorf("%s is %q, want %q", pod.linkerd, v, c.wantLinkerd)
				}
				if v := got.Annotations[pod.istio]; v != c.wantIstio {
					t.Errorf("%s is %q, want %q", pod.istio, v, c.wantIstio)
				}
				if !maps.Equal(namespace, c.namespace) {
					t.Errorf("rendering the pod changed the namespace's lists to %v, from %v", namespace, c.namespace)
				}
			})
		}
	}
}

// TestANamespacesLinkerdPortsRerenderItsCompanies holds the namespace watch
// to the lists the engine pods merge: an update that changes one of
// Linkerd's two skip lists is passed, so its Companies' pods follow, as is
// one that changes the namespace's labels, which its classes admit it by;
// one that changes any other annotation, an Istio key among them, which no
// mesh reads off a namespace, is not.
func TestANamespacesLinkerdPortsRerenderItsCompanies(t *testing.T) {
	t.Parallel()
	changed := namespaceChanged()
	namespace := func(annotations map[string]string) *metav1.PartialObjectMetadata {
		return &metav1.PartialObjectMetadata{Name: testTenant, Annotations: annotations}
	}
	labelled := namespace(nil)
	labelled.Labels = map[string]string{"team": "a"}
	if !changed.Update(event.UpdateEvent{ObjectOld: namespace(nil), ObjectNew: labelled}) {
		t.Error("a namespace's label change is not passed, and its classes' allowedNamespaces is not read again")
	}
	for _, c := range []struct {
		name     string
		old, new map[string]string
		want     bool
	}{
		{"Linkerd's outbound list set", nil, map[string]string{linkerdOutbound: "443"}, true},
		{"Linkerd's inbound list changed", map[string]string{linkerdInbound: "80"}, map[string]string{linkerdInbound: "80,443"}, true},
		{"Linkerd's outbound list removed", map[string]string{linkerdOutbound: "443"}, nil, true},
		{"an Istio list set", nil, map[string]string{istioOutbound: "443"}, false},
		{"another annotation set", nil, map[string]string{"example.com/team": "a"}, false},
		{"nothing changed", map[string]string{linkerdOutbound: "443"}, map[string]string{linkerdOutbound: "443"}, false},
	} {
		if got := changed.Update(event.UpdateEvent{ObjectOld: namespace(c.old), ObjectNew: namespace(c.new)}); got != c.want {
			t.Errorf("%s: namespaceChanged = %v, want %v", c.name, got, c.want)
		}
	}
	if mesh := meshPortsChanged(); mesh.Create(event.CreateEvent{Object: namespace(nil)}) ||
		mesh.Delete(event.DeleteEvent{Object: namespace(nil)}) {
		t.Error("meshPortsChanged passes a namespace's creation or deletion, which the label predicate passes")
	}
}
