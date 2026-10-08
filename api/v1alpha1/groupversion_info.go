// Package v1alpha1 contains the v1alpha1 API of the crewlet.ai group: a
// Crewlet company and its org chart as Kubernetes objects.
//
// Each typed field maps onto one field of Crewlet's company document, which
// its doc comment names; enum values are PascalCase here and Crewlet's own
// spelling there. Engine tuning without a typed field passes through
// Company.spec.engineConfig.
//
// No field holds a credential. Every position Crewlet marks as a credential is
// filled from a Secret through a SecretKeyReference or a SecretReference
// (NamespacedSecretKeyReference and NamespacedSecretReference on the
// cluster-scoped EngineClass), or is not offered in v1alpha1: the credentials
// of the integrations other than Slack and GitHub (see IntegrationSpec) and of
// the cli-agent model provider (see LLMProviderType). docs/concepts/credentials.md
// in the repository maps every position to its field.
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
	// types; each kind's file adds itself from init. It is apimachinery's
	// builder rather than controller-runtime's, so that importing this
	// package (as a provisioning control plane does to write these objects) pulls in
	// k8s.io/apimachinery and nothing of the operator.
	SchemeBuilder = runtime.NewSchemeBuilder(func(scheme *runtime.Scheme) error {
		metav1.AddToGroupVersion(scheme, GroupVersion)
		return nil
	})

	// AddToScheme registers every type in this package with a scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
