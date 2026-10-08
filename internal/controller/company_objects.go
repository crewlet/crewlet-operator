package controller

import (
	"fmt"

	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// FieldOwner is the field manager of everything the operator writes with
// server-side apply. The API server records which fields it set, so a field
// someone else adds (an annotation from a policy engine, a sidecar's volume)
// is left alone, and a field of the operator's that someone changes is taken
// back on the next reconcile.
const FieldOwner = "crewlet-operator"

// The components a Company's objects are labelled with
// (app.kubernetes.io/component).
const (
	componentData     = "data"
	componentAPI      = "api"
	componentWebhooks = "webhooks"
	componentKeyring  = "keyring"
	componentPlatform = "platform"
)

// The names of the objects the operator creates for a Company. Every one is
// derived from the Company's name, which the API server holds to a DNS label
// of at most 47 characters, so that the data StatefulSet's name stays within
// the 52 characters its pods' controller-revision-hash label leaves room for.
func keyringSecretName(c *crewletaiv1alpha1.Company) string   { return c.Name + "-keyring" }
func apiTokensSecretName(c *crewletaiv1alpha1.Company) string { return c.Name + "-api-tokens" }
func apiAccessSecretName(c *crewletaiv1alpha1.Company) string { return c.Name + "-api-access" }
func platformSecretName(c *crewletaiv1alpha1.Company) string  { return c.Name + "-platform" }
func bootstrapConfigName(c *crewletaiv1alpha1.Company) string { return c.Name + "-bootstrap-data" }
func serviceAccountName(c *crewletaiv1alpha1.Company) string  { return c.Name + "-engine" }
func engineServiceName(c *crewletaiv1alpha1.Company) string   { return c.Name + "-engine" }
func dataServiceName(c *crewletaiv1alpha1.Company) string     { return c.Name + "-data" }
func dataStatefulSetName(c *crewletaiv1alpha1.Company) string { return c.Name + "-data" }
func webhooksRouteName(c *crewletaiv1alpha1.Company) string   { return c.Name + "-webhooks" }
func apiRouteName(c *crewletaiv1alpha1.Company) string        { return c.Name + "-api" }
func networkPolicyName(c *crewletaiv1alpha1.Company) string   { return c.Name }

// apiURL is the in-cluster base URL of a Company's engine API: its engine
// Service, which routes to ready data pods only. The namespace-qualified
// .svc name resolves from every namespace through the cluster's DNS search
// path, without assuming the cluster's domain.
func apiURL(c *crewletaiv1alpha1.Company) string {
	return fmt.Sprintf("http://%s.%s.svc:%d", engineServiceName(c), c.Namespace, render.APIPort)
}

// labelsFor are the labels of every object the operator creates for a
// Company, for the component given.
func labelsFor(c *crewletaiv1alpha1.Company, component string) map[string]string {
	labels := selectorFor(c, component)
	labels["app.kubernetes.io/part-of"] = "crewlet"
	labels["app.kubernetes.io/managed-by"] = FieldOwner
	return labels
}

// selectorFor selects a component's objects of one Company. A StatefulSet's
// selector cannot change once it is created, so this set is fixed for good;
// labels that may change are added by labelsFor only.
func selectorFor(c *crewletaiv1alpha1.Company, component string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "crewlet",
		"app.kubernetes.io/instance":   c.Name,
		"app.kubernetes.io/component":  component,
		crewletaiv1alpha1.CompanyLabel: c.Name,
	}
}

// ownerReference makes the Company the controller of an object, so that the
// object is collected with it and its changes are queued for it.
// blockOwnerDeletion holds a foreground deletion of the Company until the
// object is gone.
func ownerReference(c *crewletaiv1alpha1.Company) *metav1ac.OwnerReferenceApplyConfiguration {
	return metav1ac.OwnerReference().
		WithAPIVersion(crewletaiv1alpha1.GroupVersion.String()).
		WithKind("Company").
		WithName(c.Name).
		WithUID(c.UID).
		WithController(true).
		WithBlockOwnerDeletion(true)
}
