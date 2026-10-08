// Package v1alpha1 contains the v1alpha1 API of the crewlet.ai group: a
// Crewlet company and its org chart as Kubernetes objects.
//
// The package registers its types with apimachinery's own scheme builder
// rather than controller-runtime's, so that importing it — as a provisioning control
// plane does to write these objects — pulls in k8s.io/apimachinery and nothing
// of the operator.
//
// +kubebuilder:object:generate=true
// +groupName=crewlet.ai
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is the group and version of every type in this package.
	GroupVersion = schema.GroupVersion{Group: "crewlet.ai", Version: "v1alpha1"}

	// SchemeGroupVersion is GroupVersion under the name Kubernetes code
	// generators (client-gen, applyconfiguration-gen) look up.
	SchemeGroupVersion = GroupVersion

	// SchemeBuilder collects the functions that register this package's
	// types; each kind's file adds itself from init.
	SchemeBuilder = runtime.NewSchemeBuilder(func(scheme *runtime.Scheme) error {
		metav1.AddToGroupVersion(scheme, GroupVersion)
		return nil
	})

	// AddToScheme registers every type in this package with a scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
