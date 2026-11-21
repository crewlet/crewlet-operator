package egressproxy

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Source is the pod a connection comes from and the namespace it runs in.
type Source struct {
	Namespace       string
	NamespaceUID    string
	NamespaceLabels map[string]string
	Pod             string
	PodLabels       map[string]string
}

// Directory names the pod behind a source address.
type Directory interface {
	// Lookup returns the one running pod with this address, and false when
	// there is none or more than one, which the proxy refuses alike.
	Lookup(addr netip.Addr) (Source, bool)
}

const podIPIndex = "podIP"

// KubeDirectory is a Directory over the cluster's pods and namespaces, held
// in informers so that no connection waits on the API server.
type KubeDirectory struct {
	pods       cache.SharedIndexInformer
	namespaces cache.SharedIndexInformer
}

// NewKubeDirectory builds the informers; Run starts them.
func NewKubeDirectory(cs kubernetes.Interface) (*KubeDirectory, error) {
	factory := informers.NewSharedInformerFactory(cs, 10*time.Minute)
	d := &KubeDirectory{
		pods:       factory.Core().V1().Pods().Informer(),
		namespaces: factory.Core().V1().Namespaces().Informer(),
	}
	// Keep only what Lookup reads: a cluster's pod specs are large, and the
	// proxy holds every pod.
	if err := d.pods.SetTransform(slimPod); err != nil {
		return nil, fmt.Errorf("setting the pod transform: %w", err)
	}
	if err := d.namespaces.SetTransform(slimNamespace); err != nil {
		return nil, fmt.Errorf("setting the namespace transform: %w", err)
	}
	if err := d.pods.AddIndexers(cache.Indexers{podIPIndex: podIPs}); err != nil {
		return nil, fmt.Errorf("adding the pod address index: %w", err)
	}
	return d, nil
}

// Run starts the informers and blocks until ctx ends.
func (d *KubeDirectory) Run(ctx context.Context) {
	go d.pods.RunWithContext(ctx)
	go d.namespaces.RunWithContext(ctx)
	<-ctx.Done()
}

// Synced reports whether both informers hold a full listing.
func (d *KubeDirectory) Synced() bool {
	return d.pods.HasSynced() && d.namespaces.HasSynced()
}

// Lookup implements Directory.
func (d *KubeDirectory) Lookup(addr netip.Addr) (Source, bool) {
	objs, err := d.pods.GetIndexer().ByIndex(podIPIndex, addr.Unmap().String())
	if err != nil || len(objs) != 1 {
		return Source{}, false
	}
	pod, ok := objs[0].(*corev1.Pod)
	if !ok {
		return Source{}, false
	}
	obj, exists, err := d.namespaces.GetStore().GetByKey(pod.Namespace)
	if err != nil || !exists {
		return Source{}, false
	}
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return Source{}, false
	}
	return Source{
		Namespace:       ns.Name,
		NamespaceUID:    string(ns.UID),
		NamespaceLabels: ns.Labels,
		Pod:             pod.Name,
		PodLabels:       pod.Labels,
	}, true
}

// podIPs indexes a pod under its addresses while it can hold them. A pod on
// the host network carries its node's address, which is no pod's identity,
// and a finished pod's address may already be another pod's.
func podIPs(obj any) ([]string, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod.Spec.HostNetwork || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return nil, nil
	}
	out := make([]string, 0, len(pod.Status.PodIPs))
	for _, ip := range pod.Status.PodIPs {
		if addr, err := netip.ParseAddr(ip.IP); err == nil {
			out = append(out, addr.Unmap().String())
		}
	}
	return out, nil
}

func slimPod(obj any) (any, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return obj, nil
	}
	return &corev1.Pod{
		ObjectMeta: slimMeta(&pod.ObjectMeta),
		Spec:       corev1.PodSpec{HostNetwork: pod.Spec.HostNetwork},
		Status:     corev1.PodStatus{Phase: pod.Status.Phase, PodIPs: pod.Status.PodIPs},
	}, nil
}

func slimNamespace(obj any) (any, error) {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return obj, nil
	}
	return &corev1.Namespace{ObjectMeta: slimMeta(&ns.ObjectMeta)}, nil
}

func slimMeta(m *metav1.ObjectMeta) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name: m.Name, Namespace: m.Namespace, UID: m.UID,
		ResourceVersion: m.ResourceVersion, Labels: m.Labels,
	}
}
