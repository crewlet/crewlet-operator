package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// lastAppliedAnnotation is where kubectl apply keeps the whole object it
// applied, data included for a Secret.
const lastAppliedAnnotation = corev1.LastAppliedConfigAnnotation

// CacheOptions are the options of the manager's cache that the reconcilers
// are written for. The manager watches kinds cluster-wide that it needs only
// a little of, so the cache keeps only that:
//
//   - managedFields of every object, which the reconcilers never read and
//     which is often the larger part of an object;
//   - for Secrets, which are cached as metadata so that no Secret's value is
//     held in memory, also the last-applied-configuration annotation, which
//     carries the whole Secret, values included, for every Secret created
//     with kubectl apply;
//   - StatefulSets, cached whole because the runtime reconciler reads their
//     status, only when the operator manages them. The one read that must
//     also see a StatefulSet of someone else's (existingStatefulSet) asks the
//     API server when the cache has none.
func CacheOptions() cache.Options {
	return cache.Options{
		DefaultTransform: cache.TransformStripManagedFields(),
		ByObject: map[client.Object]cache.ByObject{
			&appsv1.StatefulSet{}: {Label: labels.SelectorFromSet(labels.Set{managedByLabel: FieldOwner})},
			&corev1.Secret{}:      {Transform: stripSecretMetadata()},
		},
	}
}

// stripSecretMetadata is the transform of the Secrets the cache holds as
// metadata: managedFields and the last-applied-configuration annotation go.
func stripSecretMetadata() toolscache.TransformFunc {
	strip := cache.TransformStripManagedFields()
	return func(in any) (any, error) {
		out, err := strip(in)
		if err != nil {
			return nil, err
		}
		if obj, err := meta.Accessor(out); err == nil {
			if annotations := obj.GetAnnotations(); annotations[lastAppliedAnnotation] != "" {
				delete(annotations, lastAppliedAnnotation)
				obj.SetAnnotations(annotations)
			}
		}
		return out, nil
	}
}
