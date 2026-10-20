package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

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

// managedByLabel names the operator on every object it creates, so that a
// cache, a policy or a person can select them.
const managedByLabel = "app.kubernetes.io/managed-by"

// The components a Company's objects are labelled with
// (app.kubernetes.io/component).
const (
	componentData      = "data"
	componentSeats     = "seats"
	componentAPI       = "api"
	componentWebhooks  = "webhooks"
	componentArtifacts = "artifacts"
	componentKeyring   = "keyring"
)

// The names of the objects the operator creates for a Company. Every one is
// derived from the Company's name, which the API server holds to a DNS label
// of at most 47 characters, so that the data StatefulSet's name stays within
// the 52 characters its pods' controller-revision-hash label leaves room for;
// the seat pools' objects are named by scopedName.
func keyringSecretName(c *crewletaiv1alpha1.Company) string   { return c.Name + "-keyring" }
func apiTokensSecretName(c *crewletaiv1alpha1.Company) string { return c.Name + "-api-tokens" }
func apiAccessSecretName(c *crewletaiv1alpha1.Company) string { return c.Name + "-api-access" }
func bootstrapSecretName(c *crewletaiv1alpha1.Company) string { return c.Name + "-bootstrap" }
func serviceAccountName(c *crewletaiv1alpha1.Company) string  { return c.Name + "-engine" }
func engineServiceName(c *crewletaiv1alpha1.Company) string   { return c.Name + "-engine" }
func dataServiceName(c *crewletaiv1alpha1.Company) string     { return c.Name + "-data" }
func dataStatefulSetName(c *crewletaiv1alpha1.Company) string { return c.Name + "-data" }
func webhooksRouteName(c *crewletaiv1alpha1.Company) string   { return c.Name + "-webhooks" }
func apiRouteName(c *crewletaiv1alpha1.Company) string        { return c.Name + "-api" }
func artifactsRouteName(c *crewletaiv1alpha1.Company) string  { return c.Name + "-artifacts" }
func networkPolicyName(c *crewletaiv1alpha1.Company) string   { return c.Name + "-engine" }
func brokerPolicyName(c *crewletaiv1alpha1.Company) string    { return c.Name + "-broker" }
func seatsPolicyName(c *crewletaiv1alpha1.Company) string     { return c.Name + "-seats" }
func seatsServiceName(c *crewletaiv1alpha1.Company) string    { return c.Name + "-seats" }

// poolBootstrapName is the Secret a seat pool's pods start from. A PerSeat
// pool's seats share it: their document is the same, and each seat's
// StatefulSet gives its pods their seat in the environment. A Shared pool's
// document differs (its pods carry the pool's label, not a seat's), so a pool
// whose mode changed under its name has one Secret per mode while the
// running revision still pins a seat to its pods of the old one.
func poolBootstrapName(c *crewletaiv1alpha1.Company, pool string, mode crewletaiv1alpha1.SeatPoolMode) string {
	return scopedName(c, "bootstrap", pool, string(mode))
}

// sharedPoolStatefulSetName is the StatefulSet of a Shared pool's pods.
func sharedPoolStatefulSetName(c *crewletaiv1alpha1.Company, pool string) string {
	return scopedName(c, "seats", pool, "")
}

// perSeatStatefulSetName is the StatefulSet of one seat's pods in a PerSeat
// pool. It is named after the seat and not the pool, so a seat that moves
// from one PerSeat pool to another keeps its pods' names and identities.
func perSeatStatefulSetName(c *crewletaiv1alpha1.Company, handle string) string {
	return scopedName(c, "seat", handle, "")
}

// seatWorkloadName is the StatefulSet whose pods run a seat placed in pool:
// the data pods', a Shared pool's, or the seat's own in a PerSeat pool; empty
// for a pool the Company does not declare.
func seatWorkloadName(c *crewletaiv1alpha1.Company, pool, handle string) string {
	if pool == crewletaiv1alpha1.DataPool {
		return dataStatefulSetName(c)
	}
	for i := range c.Spec.Topology.SeatPools {
		p := &c.Spec.Topology.SeatPools[i]
		switch {
		case p.Name != pool:
		case p.Mode == crewletaiv1alpha1.SeatPoolPerSeat:
			return perSeatStatefulSetName(c, handle)
		default:
			return sharedPoolStatefulSetName(c, pool)
		}
	}
	return ""
}

// maxStatefulSetName is the longest a StatefulSet's name may be: its pods
// carry a controller-revision-hash label of the name, a dash and a hash of
// up to ten characters, and a label value holds 63.
const maxStatefulSetName = 52

// scopedName is the name of an object the operator makes for one pool or seat
// of a Company: the Company's name, kind and value, then eight hexadecimal
// digits of a hash of the Company's name, kind, value and salt, the start cut
// so that the whole fits maxStatefulSetName (a Company's name is up to 47
// characters, a pool's 20, a handle 63).
//
// The hash is what keeps the name the Company's own. Without it a pool or a
// handle could spell another Company's fixed name in the same namespace:
// Company a's pool keyring would make a-bootstrap-keyring, Company
// a-bootstrap's keyring Secret, and its seat data a-seat-data, Company
// a-seat's data StatefulSet, and whichever Company came first would leave the
// other stuck on a name it does not control. A fixed name ends in a word, and
// a scoped one in the hash, so the two never meet; two scoped names meet only
// where their hashes do, over inputs that differ.
func scopedName(c *crewletaiv1alpha1.Company, kind, value, salt string) string {
	// '/' is in no Kubernetes name, so the input is unambiguous.
	sum := sha256.Sum256([]byte(c.Name + "/" + kind + "/" + value + "/" + salt))
	suffix := hex.EncodeToString(sum[:4])
	start := c.Name + "-" + kind + "-" + value
	if room := maxStatefulSetName - len(suffix) - 1; len(start) > room {
		start = strings.TrimRight(start[:room], "-")
	}
	return start + "-" + suffix
}

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
	labels[managedByLabel] = FieldOwner
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
