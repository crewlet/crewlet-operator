package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// TestEveryEnginePodIsGivenTheFleetsBridgeAddress holds a data pod and a seat
// pod to one tool bridge address: the Company's public address while it has
// one, and none while it has none. Crewlet's bridge address is the fleet's,
// the same on every node: a box's call reaches a data pod, which relays it
// over the broker to the pod holding the run's seat, a seat pod included.
// The seat pod needs it for every box it launches, a box on its own host
// included, since a node without the ingress role serves no route of the
// bridge's to dial on loopback; and a data pod for every box in the
// Company's E2B sandbox. Given none, a pod builds no bridge for those boxes
// and Crewlet refuses their launches.
//
// The address is all it is read from: the templates take no LLMProvider and
// no cell, so a provider moving between cells, or appearing, rolls no pod.
func TestEveryEnginePodIsGivenTheFleetsBridgeAddress(t *testing.T) {
	t.Parallel()
	class := &crewletaiv1alpha1.EngineClass{Name: "standard"}
	class.Spec.Data.Storage.Size = new(resource.MustParse("16Gi"))
	pods := map[string]func(*crewletaiv1alpha1.Company) (*corev1ac.PodTemplateSpecApplyConfiguration, error){
		"a data pod": func(c *crewletaiv1alpha1.Company) (*corev1ac.PodTemplateSpecApplyConfiguration, error) {
			return podTemplate(c, class, &workload{
				image: engineImageV020, bootstrap: &render.BootstrapDocument{}, startupSeconds: 1866,
			})
		},
		"a seat pod": func(c *crewletaiv1alpha1.Company) (*corev1ac.PodTemplateSpecApplyConfiguration, error) {
			plan := render.PoolPlanOf(&crewletaiv1alpha1.SeatPool{Name: poolGeneral}, class.Spec.SeatPools.Defaults)
			w := &seatWorkload{name: companyName + "-seats-" + poolGeneral, pool: poolGeneral, plan: &plan}
			return seatPodTemplate(c, class, w, w.name, engineImageV020, &render.BootstrapDocument{}, "hash")
		},
	}
	for pod, template := range pods {
		for _, public := range []string{"", "https://nimbus.example.com"} {
			c := &crewletaiv1alpha1.Company{Namespace: "tenant", Name: companyName}
			c.Status.Endpoints.PublicBaseURL = public
			got, err := template(c)
			if err != nil {
				t.Fatalf("%s: %v", pod, err)
			}
			var given []string
			for _, env := range got.Spec.Containers[0].Env {
				if *env.Name == envMCPBridgeURL {
					given = append(given, *env.Value)
				}
			}
			switch {
			case public == "" && len(given) != 0:
				t.Errorf("%s of a Company with no public address was given %s %v", pod, envMCPBridgeURL, given)
			case public != "" && (len(given) != 1 || given[0] != public):
				t.Errorf("%s was given %s %v, want the Company's public address %q", pod, envMCPBridgeURL, given, public)
			}
		}
	}
}
