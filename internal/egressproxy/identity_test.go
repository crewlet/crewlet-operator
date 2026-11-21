package egressproxy

import (
	"net/netip"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func pod(name, ns, ip string, phase corev1.PodPhase, hostNetwork bool) *corev1.Pod {
	return &corev1.Pod{
		Name: name, Namespace: ns, Labels: map[string]string{"crewlet.ai/company": company},
		Spec:   corev1.PodSpec{HostNetwork: hostNetwork},
		Status: corev1.PodStatus{Phase: phase, PodIPs: []corev1.PodIP{{IP: ip}}},
	}
}

func TestKubeDirectory(t *testing.T) {
	cs := fake.NewClientset(
		&corev1.Namespace{Name: tenantNS, UID: tenantUID, Labels: map[string]string{"crewlet.ai/tenant": tenantID}},
		pod("engine", "t-abc", "10.1.0.4", corev1.PodRunning, false),
		pod("node-agent", "kube-system", "10.0.0.9", corev1.PodRunning, true),
		pod("done", "t-abc", "10.1.0.5", corev1.PodSucceeded, false),
		pod("a", "t-abc", "10.1.0.6", corev1.PodRunning, false),
		pod("b", "t-abc", "10.1.0.6", corev1.PodPending, false),
		pod("orphan", "gone", "10.1.0.7", corev1.PodRunning, false),
	)
	d, err := NewKubeDirectory(cs)
	if err != nil {
		t.Fatal(err)
	}
	go d.Run(t.Context())
	deadline := time.Now().Add(10 * time.Second)
	for !d.Synced() {
		if time.Now().After(deadline) {
			t.Fatal("informers never synced")
		}
		time.Sleep(10 * time.Millisecond)
	}

	src, ok := d.Lookup(netip.MustParseAddr("::ffff:10.1.0.4"))
	if !ok || src.Pod != "engine" || src.Namespace != "t-abc" || src.NamespaceUID != "uid-abc" ||
		src.NamespaceLabels["crewlet.ai/tenant"] != "abc" || src.PodLabels["crewlet.ai/company"] != "acme" {
		t.Fatalf("Lookup(engine) = %+v, %v", src, ok)
	}
	for ip, why := range map[string]string{
		"10.0.0.9": "a host-network pod carries its node's address",
		"10.1.0.5": "a finished pod's address may be another's",
		"10.1.0.6": "two pods claim the address",
		"10.1.0.7": "the pod's namespace is unknown",
		"10.1.0.8": "no pod has the address",
	} {
		if src, ok := d.Lookup(netip.MustParseAddr(ip)); ok {
			t.Errorf("Lookup(%s) = %+v, want none: %s", ip, src, why)
		}
	}
}
