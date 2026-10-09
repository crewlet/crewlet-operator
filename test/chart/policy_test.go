package chart_test

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crewletv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The values the tests give.
const (
	tenantName = "acme"
	defaultSA  = "default"
	privileged = "privileged"
)

// managedBy is the label the manager's policy requires.
var managedBy = map[string]string{"app.kubernetes.io/managed-by": "crewlet-operator"}

// expect checks a request's answer: want "" means it must succeed, anything
// else that it must fail with an error that says want.
func expect(t *testing.T, err error, want string) {
	t.Helper()
	switch {
	case want == "" && err != nil:
		t.Fatalf("refused, want allowed: %v", err)
	case want != "" && err == nil:
		t.Fatalf("allowed, want refused with %q", want)
	case want != "" && !strings.Contains(err.Error(), want):
		t.Fatalf("refused with %q, want %q", err, want)
	}
}

// awaitPolicy retries a write the policy named refuses until it is refused:
// the API server compiles a new ValidatingAdmissionPolicy in the
// background, and a request that arrives first is not checked against it.
// One the API server accepts meanwhile is undone with undo.
func awaitPolicy(t *testing.T, write func() error, undo func()) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := write()
		if err != nil && strings.Contains(err.Error(), "ValidatingAdmissionPolicy") {
			return
		}
		if err == nil {
			undo()
		}
		if time.Now().After(deadline) {
			t.Fatalf("the admission policy never took effect; last answer: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// namespace creates, as the cluster's administrator, a namespace with the
// labels given.
func namespace(t *testing.T, name string, labels map[string]string) *corev1.Namespace {
	t.Helper()
	ns := &corev1.Namespace{Name: name, Labels: labels}
	if err := admin.Create(ctx, ns); err != nil {
		t.Fatal(err)
	}
	return ns
}

// tenantLabels are the labels the chart's defaults require of a tenant's
// namespace.
func tenantLabels() map[string]string {
	return map[string]string{tenantLabel: tenantName, podSecurityEnforce: "restricted"}
}

func TestTenantProvisioner(t *testing.T) {
	p := as(t, provisionerUser)
	awaitPolicy(t,
		func() error { return p.Create(ctx, &corev1.Namespace{Name: "await-provisioner"}) },
		func() { _ = admin.Delete(ctx, &corev1.Namespace{Name: "await-provisioner"}) })

	t.Run("creates a namespace only as a tenant's, with the required labels", func(t *testing.T) {
		expect(t, p.Create(ctx, &corev1.Namespace{Name: "no-tenant"}), "writes only namespaces labelled "+tenantLabel)
		expect(t, p.Create(ctx, &corev1.Namespace{Name: "no-pss", Labels: map[string]string{tenantLabel: tenantName}}),
			"a tenant namespace is created with exactly these labels")
		raised := tenantLabels()
		raised[podSecurityEnforce] = privileged
		expect(t, p.Create(ctx, &corev1.Namespace{Name: privileged, Labels: raised}),
			"a tenant namespace is created with exactly these labels")
		extra := tenantLabels()
		extra["pod-security.kubernetes.io/warn"] = privileged
		expect(t, p.Create(ctx, &corev1.Namespace{Name: "extra-pss", Labels: extra}),
			"a tenant namespace is created with exactly these labels")
		expect(t, p.Create(ctx, &corev1.Namespace{Name: "tenant-create", Labels: tenantLabels()}), "")
	})

	t.Run("changes and deletes only tenants' namespaces, and never their protected labels", func(t *testing.T) {
		tenant := namespace(t, "tenant-change", tenantLabels())
		other := namespace(t, "not-a-tenant", nil)

		patch := func(ns *corev1.Namespace, mutate func(map[string]string)) error {
			got := &corev1.Namespace{}
			if err := admin.Get(ctx, client.ObjectKeyFromObject(ns), got); err != nil {
				t.Fatal(err)
			}
			base := got.DeepCopy()
			if got.Labels == nil {
				got.Labels = map[string]string{}
			}
			mutate(got.Labels)
			return p.Patch(ctx, got, client.MergeFrom(base))
		}
		expect(t, patch(tenant, func(l map[string]string) { l["example.com/tier"] = "pro" }), "")
		expect(t, patch(tenant, func(l map[string]string) { l[podSecurityEnforce] = privileged }),
			"may not set, change or remove a namespace label under pod-security.kubernetes.io/")
		expect(t, patch(tenant, func(l map[string]string) { delete(l, podSecurityEnforce) }),
			"may not set, change or remove a namespace label under pod-security.kubernetes.io/")
		expect(t, patch(tenant, func(l map[string]string) { delete(l, tenantLabel) }),
			"writes only namespaces labelled "+tenantLabel)
		expect(t, patch(other, func(l map[string]string) { l[tenantLabel] = tenantName }), "is not a tenant's")
		expect(t, p.Delete(ctx, other), "is not a tenant's")
		expect(t, p.Delete(ctx, tenant), "")
	})

	t.Run("binds the tenant-access role in tenants' namespaces only, and writes Secrets through it", func(t *testing.T) {
		tenant := namespace(t, "tenant-access", tenantLabels())
		other := namespace(t, "access-elsewhere", nil)
		secret := func(ns string, typ corev1.SecretType) *corev1.Secret {
			return &corev1.Secret{Namespace: ns, Name: "slack", Type: typ, StringData: map[string]string{"botToken": "x"}}
		}
		binding := func(ns, role string) *rbacv1.RoleBinding {
			return &rbacv1.RoleBinding{
				Namespace: ns, Name: "provisioner",
				RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: role},
				Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: provisionerName, Namespace: provisionerNS}},
			}
		}

		expect(t, p.Create(ctx, secret(tenant.Name, corev1.SecretTypeOpaque)), "forbidden")
		expect(t, p.Create(ctx, binding(other.Name, tenantAccessRole)), "is not a tenant's")
		expect(t, p.Create(ctx, binding(tenant.Name, "admin")), "binds only the ClusterRole "+tenantAccessRole)
		expect(t, p.Create(ctx, binding(tenant.Name, tenantAccessRole)), "")

		expect(t, p.Create(ctx, secret(tenant.Name, corev1.SecretTypeOpaque)), "")
		token := secret(tenant.Name, corev1.SecretTypeServiceAccountToken)
		token.Name = "token"
		token.Annotations = map[string]string{corev1.ServiceAccountNameKey: defaultSA}
		expect(t, p.Create(ctx, token), "may not write a service-account-token Secret")
		expect(t, p.Create(ctx, secret(other.Name, corev1.SecretTypeOpaque)), "forbidden")
		expect(t, p.Create(ctx, secret("kube-system", corev1.SecretTypeOpaque)), "forbidden")
		expect(t, p.Create(ctx, &crewletv1alpha1.Company{
			Namespace: tenant.Name, Name: tenantName,
			Spec: crewletv1alpha1.CompanySpec{CompanyName: "Acme"},
		}), "")
	})

	t.Run("holds no one else to it", func(t *testing.T) {
		expect(t, admin.Create(ctx, &corev1.Namespace{Name: "admin-made"}), "")
	})
}
