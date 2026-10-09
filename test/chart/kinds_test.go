package chart_test

import (
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crewletv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

const (
	clusterRoleKind = "ClusterRole"
	verbGet         = "get"
	subStatus       = "status"
)

// namespacedResources are the resources of every namespaced kind of the
// crewlet.ai API, read from the scheme and the API server rather than
// listed here, so that a kind added to the API is checked without anyone
// remembering this file.
func namespacedResources(t *testing.T) []string {
	t.Helper()
	var out []string
	for gvk := range scheme.AllKnownTypes() {
		if gvk.Group != crewletv1alpha1.GroupVersion.Group {
			continue
		}
		// The group's scheme holds the option types every group carries
		// (DeleteOptions, ListOptions) and the lists; an object is a kind.
		obj, err := scheme.New(gvk)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := obj.(client.Object); !ok {
			continue
		}
		mapping, err := admin.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			t.Fatalf("mapping %s: %v", gvk, err)
		}
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			out = append(out, mapping.Resource.Resource)
		}
	}
	if len(out) == 0 {
		t.Fatal("the scheme holds no namespaced crewlet.ai kind")
	}
	return out
}

// allowed asks the API server, as c, whether c may verb the resource (and
// subresource) in namespace.
func allowed(
	t *testing.T, c client.Client, namespace, verb string, resource schema.GroupResource, subresource string,
) bool {
	t.Helper()
	review := &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{
		ResourceAttributes: &authorizationv1.ResourceAttributes{
			Namespace: namespace, Verb: verb, Group: resource.Group, Resource: resource.Resource, Subresource: subresource,
		},
	}}
	if err := c.Create(ctx, review); err != nil {
		t.Fatal(err)
	}
	return review.Status.Allowed
}

// TestEveryKindIsGranted holds the roles that grant the crewlet.ai kinds by
// name to the API: a kind the roles forget is one a tenant's editors cannot
// write, and one a platform cannot provision.
func TestEveryKindIsGranted(t *testing.T) {
	tenant := namespace(t, "tenant-kinds", tenantLabels())
	bind := func(role, user string) {
		t.Helper()
		if err := admin.Create(ctx, &rbacv1.RoleBinding{
			Namespace: tenant.Name, Name: role,
			RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: clusterRoleKind, Name: role},
			Subjects: []rbacv1.Subject{{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: user}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	editRole, viewRole := release+"-crewlet-edit", release+"-crewlet-view"
	bind(editRole, "editor")
	bind(viewRole, "viewer")
	bind(tenantAccessRole, provisionerUser)
	editor, viewer, provisioner := as(t, "editor"), as(t, "viewer"), as(t, provisionerUser)

	for _, resource := range namespacedResources(t) {
		gr := schema.GroupResource{Group: crewletv1alpha1.GroupVersion.Group, Resource: resource}
		for _, c := range []struct {
			who                  string
			c                    client.Client
			namespace, verb, sub string
		}{
			{editRole, editor, tenant.Name, "create", ""},
			{editRole, editor, tenant.Name, "delete", ""},
			{viewRole, viewer, tenant.Name, "list", ""},
			{viewRole, viewer, tenant.Name, verbGet, subStatus},
			{tenantAccessRole + " in a tenant's namespace", provisioner, tenant.Name, "create", ""},
			{tenantAccessRole + " in a tenant's namespace", provisioner, tenant.Name, verbGet, subStatus},
			{"the tenant provisioner, across tenants", provisioner, "", "list", ""},
			{"the tenant provisioner, across tenants", provisioner, "", verbGet, subStatus},
		} {
			if !allowed(t, c.c, c.namespace, c.verb, gr, c.sub) {
				t.Errorf("%s may not %s %s (subresource %q)", c.who, c.verb, gr, c.sub)
			}
		}
	}
}
