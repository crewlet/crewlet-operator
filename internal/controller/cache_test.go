package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/event"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// TestCacheKeepsNoSecretValue: a Secret created with kubectl apply carries
// its whole data in an annotation, which the metadata cache would otherwise
// hold for every Secret of the cluster.
func TestCacheKeepsNoSecretValue(t *testing.T) {
	var transform cache.ByObject
	for obj, by := range CacheOptions().ByObject {
		if _, ok := obj.(*corev1.Secret); ok {
			transform = by
		}
	}
	if transform.Transform == nil {
		t.Fatal("Secrets are cached without a transform")
	}
	in := &metav1.PartialObjectMetadata{
		Name: "slack",
		Annotations: map[string]string{
			corev1.LastAppliedConfigAnnotation: `{"data":{"botToken":"eG94Yi0x"}}`,
			"team":                             "platform",
		},
		ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "kubectl"}},
	}
	out, err := transform.Transform(in)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(*metav1.PartialObjectMetadata)
	if _, kept := got.Annotations[corev1.LastAppliedConfigAnnotation]; kept {
		t.Error("the last-applied-configuration annotation, which holds the Secret's data, is cached")
	}
	if got.Annotations["team"] != "platform" {
		t.Errorf("annotations = %v: an annotation of the user's own was dropped", got.Annotations)
	}
	if got.ManagedFields != nil {
		t.Error("managedFields are cached")
	}
}

// TestCacheHoldsTheStatefulSetsTheOperatorManages: the StatefulSet informer
// is restricted by label, so the data StatefulSet must carry it.
func TestCacheHoldsTheStatefulSetsTheOperatorManages(t *testing.T) {
	var selector labels.Selector
	for obj, by := range CacheOptions().ByObject {
		if _, ok := obj.(*appsv1.StatefulSet); ok {
			selector = by.Label
		}
	}
	if selector == nil {
		t.Fatal("StatefulSets are cached without a label selector")
	}
	c := &crewletaiv1alpha1.Company{}
	c.Name = companyName
	if !selector.Matches(labels.Set(labelsFor(c, componentData))) {
		t.Errorf("the cache's selector %s does not match the data StatefulSet's labels", selector)
	}
}

func TestLimitsSeeOnlyCreationsAndDeletions(t *testing.T) {
	p := createdOrDeleted()
	seat := &crewletaiv1alpha1.Seat{}
	if !p.Create(event.CreateEvent{Object: seat}) || !p.Delete(event.DeleteEvent{Object: seat}) {
		t.Error("a Seat created or deleted does not reach the limits")
	}
	if p.Update(event.UpdateEvent{ObjectOld: seat, ObjectNew: seat}) {
		t.Error("a Seat's update, which cannot change how many there are, re-runs its Company")
	}
}
