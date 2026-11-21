package chart_test

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/crewlet/crewlet-operator/internal/egresstoken"
)

const (
	verbList   = "list"
	verbCreate = "create"
)

// TestEgressProxyReadsOnlyPodsAndNamespaces holds the proxy's ClusterRole
// to what naming a connection's source needs: a proxy that could read
// Secrets would hold every tenant's credentials, and one that could write
// would be a way into the cluster from the internet's side.
func TestEgressProxyReadsOnlyPodsAndNamespaces(t *testing.T) {
	proxy := as(t, egressProxyUser)
	core := func(resource string) schema.GroupResource { return schema.GroupResource{Resource: resource} }
	for _, resource := range []string{"pods", "namespaces"} {
		for _, verb := range []string{verbGet, verbList, "watch"} {
			if !allowed(t, proxy, "", verb, core(resource), "") {
				t.Errorf("the egress proxy may not %s %s", verb, resource)
			}
		}
		for _, verb := range []string{verbCreate, "update", "patch", "delete"} {
			if allowed(t, proxy, "", verb, core(resource), "") {
				t.Errorf("the egress proxy may %s %s", verb, resource)
			}
		}
	}
	for _, resource := range []string{"secrets", "configmaps", "serviceaccounts"} {
		if allowed(t, proxy, "", verbGet, core(resource), "") || allowed(t, proxy, "", verbList, core(resource), "") {
			t.Errorf("the egress proxy may read %s", resource)
		}
	}
}

// TestEgressProxyPolicyAdmitsOnlyItsTiers reads the tier's NetworkPolicy:
// the proxy port, from tenant namespaces of the tier's trust tiers alone.
func TestEgressProxyPolicyAdmitsOnlyItsTiers(t *testing.T) {
	var policy *networkingv1.NetworkPolicy
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered), 4096)
	for policy == nil {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(&obj.Object); err != nil {
			if errors.Is(err, io.EOF) {
				t.Fatal("the chart renders no NetworkPolicy for the egress proxy tier")
			}
			t.Fatal(err)
		}
		if obj.GetKind() != "NetworkPolicy" || obj.GetLabels()["crewlet.ai/egress-tier"] != egressTier {
			continue
		}
		policy = &networkingv1.NetworkPolicy{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, policy); err != nil {
			t.Fatal(err)
		}
	}
	both := []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}
	if !slices.Equal(policy.Spec.PolicyTypes, both) {
		t.Errorf("policy types %v, want Ingress and Egress", policy.Spec.PolicyTypes)
	}
	// A CNI that denies what no policy allows cuts a proxy whose policy
	// names no egress off from DNS and every destination.
	if len(policy.Spec.Egress) != 1 || len(policy.Spec.Egress[0].To) != 0 || len(policy.Spec.Egress[0].Ports) != 0 {
		t.Errorf("egress = %+v, want one rule open to everything", policy.Spec.Egress)
	}
	if len(policy.Spec.Ingress) != 1 || len(policy.Spec.Ingress[0].From) != 1 || len(policy.Spec.Ingress[0].Ports) != 1 {
		t.Fatalf("ingress = %+v, want one rule with one port and one peer", policy.Spec.Ingress)
	}
	rule := policy.Spec.Ingress[0]
	if rule.Ports[0].Port == nil || rule.Ports[0].Port.String() != "proxy" {
		t.Errorf("port %v, want the proxy port", rule.Ports[0].Port)
	}
	peer := rule.From[0]
	if peer.PodSelector != nil || peer.IPBlock != nil || peer.NamespaceSelector == nil {
		t.Fatalf("peer %+v, want a namespace selector alone", peer)
	}
	want := map[string][]string{egresstoken.LabelTenant: nil, egresstoken.LabelTrustTier: {"t1", "t2"}}
	got := map[string][]string{}
	for _, req := range peer.NamespaceSelector.MatchExpressions {
		got[req.Key] = req.Values
	}
	if len(peer.NamespaceSelector.MatchLabels) != 0 || len(got) != len(want) ||
		!slices.Equal(got[egresstoken.LabelTrustTier], want[egresstoken.LabelTrustTier]) {
		t.Errorf("namespace selector %+v, want the tenant label and trust tiers t1, t2", peer.NamespaceSelector)
	}
	if _, ok := got[egresstoken.LabelTenant]; !ok {
		t.Errorf("namespace selector %+v does not require the tenant label", peer.NamespaceSelector)
	}
}
