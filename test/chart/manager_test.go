package chart_test

import (
	"maps"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// instanceLabel and companyLabel are the labels the operator gives every
// object of a Company, with the Company's name.
const (
	instanceLabel = "app.kubernetes.io/instance"
	companyLabel  = "crewlet.ai/company"
)

// engineSet is a data StatefulSet as the operator writes one, in the
// respects the manager's policy checks; mutate makes it what a subverted
// manager would write instead.
func engineSet(ns string, mutate ...func(*appsv1.StatefulSet)) *appsv1.StatefulSet {
	labels := map[string]string{instanceLabel: tenantName}
	maps.Copy(labels, managedBy)
	sts := &appsv1.StatefulSet{
		Namespace: ns, Name: "acme-data", Labels: labels,
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{instanceLabel: tenantName}},
			Template: corev1.PodTemplateSpec{
				Labels: labels,
				Spec: corev1.PodSpec{
					ServiceAccountName: "acme-engine",
					Containers:         []corev1.Container{{Name: "engine", Image: "ghcr.io/crewlet/crewlet:v0.1.0"}},
				},
			},
		},
	}
	for _, m := range mutate {
		m(sts)
	}
	return sts
}

// enginePods selects the pods of one component of a company, as the operator
// selects them; seatPods its seat pods.
func enginePods(component string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name": "crewlet", instanceLabel: tenantName,
		"app.kubernetes.io/component": component, companyLabel: tenantName,
	}
}

func seatPods() map[string]string { return enginePods("seats") }

func TestManagerWrites(t *testing.T) {
	m := as(t, managerUser)
	ns := namespace(t, "manager-writes", nil).Name
	awaitPolicy(t,
		func() error { return m.Create(ctx, &corev1.Secret{Namespace: ns, Name: "await-manager"}) },
		func() { _ = admin.Delete(ctx, &corev1.Secret{Namespace: ns, Name: "await-manager"}) })

	t.Run("writes only Opaque Secrets it labels", func(t *testing.T) {
		expect(t, m.Create(ctx, &corev1.Secret{Namespace: ns, Name: "keyring", Labels: managedBy}), "")
		expect(t, m.Create(ctx, &corev1.Secret{Namespace: ns, Name: "unlabelled"}),
			"the operator writes only objects labelled app.kubernetes.io/managed-by=crewlet-operator")
		expect(t, m.Create(ctx, &corev1.Secret{
			Namespace: ns, Name: "token", Labels: managedBy, Type: corev1.SecretTypeServiceAccountToken,
			Annotations: map[string]string{corev1.ServiceAccountNameKey: defaultSA},
		}), "the operator writes only Secrets of type Opaque")
	})

	t.Run("changes someone else's Secret only to adopt it, and deletes none", func(t *testing.T) {
		theirs := &corev1.Secret{Namespace: ns, Name: "theirs", Data: map[string][]byte{"k": []byte("v")}}
		if err := admin.Create(ctx, theirs); err != nil {
			t.Fatal(err)
		}
		changed := theirs.DeepCopy()
		changed.Labels = managedBy
		changed.Data = map[string][]byte{"k": []byte("rewritten")}
		expect(t, m.Patch(ctx, changed, client.MergeFrom(theirs)),
			"the operator changes or deletes only objects labelled app.kubernetes.io/managed-by=crewlet-operator")
		expect(t, m.Delete(ctx, theirs),
			"the operator changes or deletes only objects labelled app.kubernetes.io/managed-by=crewlet-operator")

		adopted := theirs.DeepCopy()
		adopted.Labels = managedBy
		expect(t, m.Patch(ctx, adopted, client.MergeFrom(theirs)), "")
		expect(t, m.Delete(ctx, adopted), "")
	})

	t.Run("writes only disruption budgets it labels", func(t *testing.T) {
		budget := func(name string, labels map[string]string) *policyv1.PodDisruptionBudget {
			return &policyv1.PodDisruptionBudget{
				Namespace: ns, Name: name, Labels: labels,
				Spec: policyv1.PodDisruptionBudgetSpec{
					MaxUnavailable: new(intstr.FromInt32(1)),
					Selector:       &metav1.LabelSelector{MatchLabels: seatPods()},
				},
			}
		}
		expect(t, m.Create(ctx, budget("acme-seats-general", managedBy)), "")
		expect(t, m.Create(ctx, budget("unlabelled", nil)),
			"the operator writes only objects labelled app.kubernetes.io/managed-by=crewlet-operator")
		theirs := budget("theirs", nil)
		if err := admin.Create(ctx, theirs); err != nil {
			t.Fatal(err)
		}
		expect(t, m.Delete(ctx, theirs),
			"the operator changes or deletes only objects labelled app.kubernetes.io/managed-by=crewlet-operator")
	})

	t.Run("budgets disruptions of a company's seat pods alone", func(t *testing.T) {
		const msg = "the operator budgets disruptions only of a company's seat pods"
		budget := func(name string, selector *metav1.LabelSelector) *policyv1.PodDisruptionBudget {
			return &policyv1.PodDisruptionBudget{
				Namespace: ns, Name: name, Labels: managedBy,
				Spec: policyv1.PodDisruptionBudgetSpec{MaxUnavailable: new(intstr.FromInt32(0)), Selector: selector},
			}
		}
		// A Shared pool's budget, which leaves a PerSeat seat's pods out.
		expect(t, m.Create(ctx, budget("acme-seats-batch", &metav1.LabelSelector{
			MatchLabels: seatPods(),
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "crewlet.ai/seat", Operator: metav1.LabelSelectorOpDoesNotExist},
			},
		})), "")
		for name, selector := range map[string]*metav1.LabelSelector{
			"the cluster's DNS": {MatchLabels: map[string]string{"k8s-app": "kube-dns"}},
			"every pod":         {},
			"a data pod":        {MatchLabels: enginePods("data")},
			"more pods by an expression": {
				MatchLabels: seatPods(),
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: companyLabel, Operator: metav1.LabelSelectorOpExists},
				},
			},
		} {
			t.Run(name, func(t *testing.T) {
				expect(t, m.Create(ctx, budget("drain-stopper", selector)), msg)
			})
		}
	})

	t.Run("writes NetworkPolicies that select a company's engine pods alone", func(t *testing.T) {
		const msg = "the operator's NetworkPolicies select only a company's engine pods"
		policy := func(name string, selector metav1.LabelSelector) *networkingv1.NetworkPolicy {
			return &networkingv1.NetworkPolicy{
				Namespace: ns, Name: name, Labels: managedBy,
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: selector,
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				},
			}
		}
		expect(t, m.Create(ctx, policy("acme-engine", metav1.LabelSelector{MatchLabels: enginePods("data")})), "")
		expect(t, m.Create(ctx, policy("acme-seats", metav1.LabelSelector{MatchLabels: seatPods()})), "")
		for name, selector := range map[string]metav1.LabelSelector{
			"every pod of the namespace": {},
			"the cluster's DNS":          {MatchLabels: map[string]string{"k8s-app": "kube-dns"}},
			"another component":          {MatchLabels: enginePods("keyring")},
			"more pods by an expression": {
				MatchLabels: enginePods("data"),
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: companyLabel, Operator: metav1.LabelSelectorOpExists},
				},
			},
		} {
			t.Run(name, func(t *testing.T) {
				expect(t, m.Create(ctx, policy("isolate", selector)), msg)
			})
		}
	})

	t.Run("runs only engine pods", func(t *testing.T) {
		const msg = "the operator runs only engine pods"
		expect(t, m.Create(ctx, engineSet(ns)), "")
		for name, mutate := range map[string]func(*appsv1.StatefulSet){
			"another ServiceAccount": func(s *appsv1.StatefulSet) { s.Spec.Template.Spec.ServiceAccountName = defaultSA },
			"the node's network":     func(s *appsv1.StatefulSet) { s.Spec.Template.Spec.HostNetwork = true },
			"the node's PIDs":        func(s *appsv1.StatefulSet) { s.Spec.Template.Spec.HostPID = true },
			"a host path": func(s *appsv1.StatefulSet) {
				s.Spec.Template.Spec.Volumes = []corev1.Volume{{
					Name: "root", HostPath: &corev1.HostPathVolumeSource{Path: "/"},
				}}
			},
			"a privileged container": func(s *appsv1.StatefulSet) {
				s.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{Privileged: new(true)}
			},
		} {
			t.Run(name, func(t *testing.T) {
				expect(t, m.Create(ctx, engineSet(ns, func(s *appsv1.StatefulSet) { s.Name = "other-data"; mutate(s) })), msg)
			})
		}
	})
}
