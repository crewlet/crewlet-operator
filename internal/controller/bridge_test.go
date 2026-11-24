package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// TestNoEnginePodTemplateReadsThePublicAddress holds a data pod's and a seat
// pod's templates to the Company's public address, beside what the data pod's
// crewlet.yaml makes of it: none. Every box an engine pod makes is in Crewlet's
// direct cell, on the pod's own host, and dials the tool bridge and the
// telemetry receiver on its own pod's listener, on loopback (Crewlet's
// boxHostURL), so no pod is handed the address in its environment, and a
// Company whose address changes rolls no seat pod.
//
// Mutation: give every pod the public address in an environment variable,
// and this fails.
func TestNoEnginePodTemplateReadsThePublicAddress(t *testing.T) {
	t.Parallel()
	class := &crewletaiv1alpha1.EngineClass{Name: "standard"}
	class.Spec.Data.Storage.Size = new(resource.MustParse("16Gi"))
	class.Spec.Telemetry.OTLPEndpoint = "https://otel.example.com"
	pods := map[string]func(*crewletaiv1alpha1.Company) (*corev1ac.PodTemplateSpecApplyConfiguration, error){
		"a data pod": func(c *crewletaiv1alpha1.Company) (*corev1ac.PodTemplateSpecApplyConfiguration, error) {
			return podTemplate(c, class, &workload{
				image: engineImageV020, bootstrap: &render.BootstrapDocument{}, startupSeconds: 1747,
			})
		},
		"a seat pod": func(c *crewletaiv1alpha1.Company) (*corev1ac.PodTemplateSpecApplyConfiguration, error) {
			plan := render.PoolPlanOf(&crewletaiv1alpha1.SeatPool{Name: poolGeneral}, class.Spec.SeatPools.Defaults)
			w := &seatWorkload{name: companyName + "-seats-" + poolGeneral, pool: poolGeneral, plan: &plan}
			return seatPodTemplate(c, class, w, w.name, engineImageV020, &render.BootstrapDocument{}, "hash", nil)
		},
	}
	for pod, template := range pods {
		templates := make([]*corev1ac.PodTemplateSpecApplyConfiguration, 0, 2)
		for _, public := range []string{"", "https://nimbus.example.com"} {
			c := &crewletaiv1alpha1.Company{Namespace: "tenant", Name: companyName}
			c.Status.Endpoints.PublicBaseURL = public
			got, err := template(c)
			if err != nil {
				t.Fatalf("%s: %v", pod, err)
			}
			templates = append(templates, got)
		}
		if !equality.Semantic.DeepEqual(templates[0], templates[1]) {
			t.Errorf("%s of a Company with a public address differs from one without: its environment is\n%v\nwith "+
				"it and\n%v\nwithout", pod, envNames(templates[1]), envNames(templates[0]))
		}
	}
}

// envNames is the names of the variables of a pod template's first container.
func envNames(p *corev1ac.PodTemplateSpecApplyConfiguration) []string {
	out := make([]string, 0, len(p.Spec.Containers[0].Env))
	for _, env := range p.Spec.Containers[0].Env {
		out = append(out, *env.Name)
	}
	return out
}
