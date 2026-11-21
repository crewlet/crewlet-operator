package chart_test

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crewletv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/test/docs"
)

// The hosted cell's release, rendered with the values docs/guides/saas-cell.md
// gives, and the provisioner those values bind.
const (
	cellRelease     = "cell"
	cellProvisioner = "system:serviceaccount:platform:" + cellSA
	cellAccessRole  = cellRelease + "-crewlet-operator-tenant-access"
	cellGuide       = "saas-cell.md"
	cellClassLabel  = "crewlet.ai/cell-class"
	cellSA          = "provisioner"
	ringLabel       = "crewlet.ai/ring"
	quarantine      = "crewlet.ai/quarantine"
	tenantsAlone    = "admits tenants' namespaces alone"
	closedValues    = "may hold only these values"
	poolLabel       = "crewlet.ai/pool"
)

// cellManifests are the guide's here-documents: the chart's values, the ring
// classes, and one tenant as the provisioner writes it.
func cellManifests() ([]string, error) {
	manifests, err := docs.Manifests(filepath.Join("..", "..", "docs", "guides", cellGuide))
	if err != nil {
		return nil, err
	}
	if len(manifests) != 3 {
		return nil, fmt.Errorf("%s holds %d here-documents, want 3: the values, the classes and a tenant",
			cellGuide, len(manifests))
	}
	return manifests, nil
}

// cellValues writes the guide's values to a file for helm, and returns its path.
func cellValues() (string, error) {
	manifests, err := cellManifests()
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "cell-values-*.yaml")
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(manifests[0]); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// objectsOf decodes a multi-document manifest.
func objectsOf(t *testing.T, manifest string) []*unstructured.Unstructured {
	t.Helper()
	var out []*unstructured.Unstructured
	decoder := utilyaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(&obj.Object); err != nil {
			break
		}
		if len(obj.Object) > 0 {
			out = append(out, obj)
		}
	}
	if len(out) == 0 {
		t.Fatal("the manifest holds no object")
	}
	return out
}

func TestCellClasses(t *testing.T) {
	manifests, err := cellManifests()
	if err != nil {
		t.Fatal(err)
	}
	classes := objectsOf(t, manifests[1])
	if len(classes) != 4 {
		t.Fatalf("the guide applies %d classes, want one per tier and ring (4)", len(classes))
	}

	// variant is the guide's first class under another name, changed.
	variant := func(name string, mutate func(spec map[string]any)) *unstructured.Unstructured {
		c := classes[0].DeepCopy()
		c.SetName(name)
		spec, _, _ := unstructured.NestedMap(c.Object, "spec")
		mutate(spec)
		if err := unstructured.SetNestedMap(c.Object, spec, "spec"); err != nil {
			t.Fatal(err)
		}
		return c
	}
	byTag := variant("await-cell-class", func(spec map[string]any) {
		_ = unstructured.SetNestedField(spec, "ghcr.io/crewlet/crewlet:main", "engine", "image")
	})
	awaitPolicy(t,
		func() error { return admin.Create(ctx, byTag.DeepCopy()) },
		func() { _ = admin.Delete(ctx, byTag.DeepCopy()) })

	t.Run("admits the guide's classes", func(t *testing.T) {
		for _, c := range classes {
			expect(t, admin.Create(ctx, c.DeepCopy()), "")
		}
	})

	t.Run("refuses a cell class that loses a property, to administrators too", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			mutate func(spec map[string]any)
			want   string
		}{
			{"by-tag", func(spec map[string]any) {
				_ = unstructured.SetNestedField(spec, "ghcr.io/crewlet/crewlet:main", "engine", "image")
			}, "pins its engine image by digest"},
			{"open-policy", func(spec map[string]any) {
				_ = unstructured.SetNestedMap(spec, map[string]any{"mode": "None"}, "networkPolicy")
				_ = unstructured.SetNestedMap(spec, map[string]any{"mode": "None"}, "exposure", "webhooks")
			}, "isolates its engines"},
			{"any-namespace", func(spec map[string]any) {
				_ = unstructured.SetNestedMap(spec, map[string]any{
					"matchLabels": map[string]any{ringLabel: "r0"},
				}, "allowedNamespaces")
			}, tenantsAlone},
			{"no-selector", func(spec map[string]any) {
				unstructured.RemoveNestedField(spec, "allowedNamespaces")
			}, tenantsAlone},
			{"tenant-absent", func(spec map[string]any) {
				_ = unstructured.SetNestedMap(spec, map[string]any{
					"matchExpressions": []any{map[string]any{"key": tenantLabel, "operator": "DoesNotExist"}},
				}, "allowedNamespaces")
			}, tenantsAlone},
		} {
			t.Run(tc.name, func(t *testing.T) {
				expect(t, admin.Create(ctx, variant("refused-"+tc.name, tc.mutate)), tc.want)
			})
		}
	})

	t.Run("keeps a cell class one", func(t *testing.T) {
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(classes[0].GroupVersionKind())
		if err := admin.Get(ctx, client.ObjectKey{Name: classes[0].GetName()}, got); err != nil {
			t.Fatal(err)
		}
		labels := got.GetLabels()
		delete(labels, cellClassLabel)
		got.SetLabels(labels)
		// Both policies hold the class then: the cell one by its old labels,
		// the other one by its new.
		err := admin.Update(ctx, got)
		if err == nil || !strings.Contains(err.Error(), "a cell class stays one") &&
			!strings.Contains(err.Error(), "a class that is not a cell class admits no tenant's namespace") {
			t.Fatalf("taking the cell label off a cell class: %v, want it refused", err)
		}
	})

	t.Run("keeps every other class out of tenants' namespaces, and holds it to nothing else", func(t *testing.T) {
		plain := func(name string, allowed map[string]any) *unstructured.Unstructured {
			c := variant(name, func(spec map[string]any) {
				_ = unstructured.SetNestedField(spec, "ghcr.io/crewlet/crewlet:main", "engine", "image")
				if allowed == nil {
					unstructured.RemoveNestedField(spec, "allowedNamespaces")
				} else {
					_ = unstructured.SetNestedMap(spec, allowed, "allowedNamespaces")
				}
			})
			c.SetLabels(nil)
			return c
		}
		const msg = "a class that is not a cell class admits no tenant's namespace"
		tenants := func(operator string) map[string]any {
			return map[string]any{"matchExpressions": []any{map[string]any{"key": tenantLabel, "operator": operator}}}
		}
		expect(t, admin.Create(ctx, plain("plain-anywhere", nil)), msg)
		expect(t, admin.Create(ctx, plain("plain-tenants", tenants("Exists"))), msg)
		expect(t, admin.Create(ctx, plain("plain", tenants("DoesNotExist"))), "")
	})

	t.Run("refuses a cell class whose pods would not run on the tenant pool", func(t *testing.T) {
		expect(t, admin.Create(ctx, variant("unplaced", func(spec map[string]any) {
			unstructured.RemoveNestedField(spec, "podTemplate")
		})), "a cell class places its pods on the tenant pool")
	})
}

func TestCellTenant(t *testing.T) {
	manifests, err := cellManifests()
	if err != nil {
		t.Fatal(err)
	}
	objs := objectsOf(t, manifests[2])
	if len(objs) != 3 || objs[0].GetKind() != "Namespace" {
		t.Fatalf("the guide's tenant is %d objects, want a Namespace, a NetworkPolicy and a Company", len(objs))
	}
	nsName := objs[0].GetName()
	guideLabels := objs[0].GetLabels()
	p := as(t, cellProvisioner)

	labelsWith := func(change map[string]string, drop ...string) map[string]string {
		l := maps.Clone(guideLabels)
		maps.Copy(l, change)
		for _, k := range drop {
			delete(l, k)
		}
		return l
	}
	tenantID := guideLabels[tenantLabel]
	awaitPolicy(t,
		func() error {
			awaited := labelsWith(map[string]string{tenantLabel: "await"})
			return p.Create(ctx, &corev1.Namespace{Name: "await-cell", Labels: awaited})
		},
		func() { _ = admin.Delete(ctx, &corev1.Namespace{Name: "await-cell"}) })

	t.Run("creates a namespace only with the cell's labels and name", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			labels map[string]string
			want   string
		}{
			{"t-other", labelsWith(nil), "a tenant namespace is named t-<the value of " + tenantLabel + ">"},
			{nsName, labelsWith(nil, "crewlet.ai/org"), "carries every one of these labels from its creation on"},
			{nsName, labelsWith(map[string]string{ringLabel: "r9"}), closedValues},
			{nsName, labelsWith(map[string]string{"crewlet.ai/trust-tier": "t9"}), closedValues},
			{nsName, labelsWith(map[string]string{quarantine: "maybe"}), closedValues},
		} {
			expect(t, p.Create(ctx, &corev1.Namespace{Name: tc.name, Labels: tc.labels}), tc.want)
		}
		for _, obj := range objs[:1] {
			expect(t, p.Create(ctx, obj.DeepCopy()), "")
		}
	})

	t.Run("writes the namespace's policy and Company through the tenant-access role", func(t *testing.T) {
		expect(t, p.Create(ctx, &rbacv1.RoleBinding{
			Namespace: nsName, Name: cellSA,
			RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: clusterRoleKind, Name: cellAccessRole},
			Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: cellSA, Namespace: "platform"}},
		}), "")
		awaitGrant(t, func() error { return p.Create(ctx, objs[1].DeepCopy()) })
		expect(t, p.Create(ctx, objs[2].DeepCopy()), "")

		policy := func(
			name string, labels map[string]string, egress []networkingv1.NetworkPolicyEgressRule,
		) *networkingv1.NetworkPolicy {
			return &networkingv1.NetworkPolicy{
				Namespace: nsName, Name: name, Labels: labels,
				Spec: networkingv1.NetworkPolicySpec{
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
					Egress:      egress,
				},
			}
		}
		dns := intstr.FromInt32(53)
		https := intstr.FromInt32(443)
		expect(t, p.Create(ctx, policy("allow-dns", nil, []networkingv1.NetworkPolicyEgressRule{{
			Ports: []networkingv1.NetworkPolicyPort{{Port: &dns}},
		}})), "")
		expect(t, p.Create(ctx, policy("allow-all", nil, []networkingv1.NetworkPolicyEgressRule{{}})),
			"only NetworkPolicies that deny, or allow DNS alone")
		expect(t, p.Create(ctx, policy("allow-https", nil, []networkingv1.NetworkPolicyEgressRule{{
			Ports: []networkingv1.NetworkPolicyPort{{Port: &https}},
		}})), "only NetworkPolicies that deny, or allow DNS alone")
		expect(t, p.Create(ctx, policy("posing", map[string]string{"app.kubernetes.io/managed-by": "crewlet-operator"}, nil)),
			"leaves the operator's NetworkPolicies alone")

		pinned := &crewletv1alpha1.Company{
			Namespace: nsName, Name: "pinned",
			Spec: crewletv1alpha1.CompanySpec{
				CompanyName: "pinned", EngineClassName: "team-r1",
				Runtime: crewletv1alpha1.CompanyRuntime{Version: "1.2.3"},
			},
		}
		expect(t, p.Create(ctx, pinned), "spec.runtime.version is not set in a cell")
		expect(t, admin.Create(ctx, pinned.DeepCopy()), "spec.runtime.version is not set in a cell")
	})

	t.Run("runs every pod of the namespace on the tenant pool", func(t *testing.T) {
		pod := func(name string, mutate func(*corev1.PodSpec)) *corev1.Pod {
			p := &corev1.Pod{
				Namespace: nsName, Name: name,
				Spec: corev1.PodSpec{
					// What the namespace's Pod Security level restricted asks.
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   new(true),
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{{
						Name: "c", Image: "registry.example/c:1",
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: new(false),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
					NodeSelector: map[string]string{poolLabel: "tenant"},
					Tolerations: []corev1.Toleration{{
						Key: poolLabel, Value: "tenant", Effect: corev1.TaintEffectNoSchedule,
					}},
				},
			}
			mutate(&p.Spec)
			return p
		}
		expect(t, admin.Create(ctx, pod("placed", func(*corev1.PodSpec) {})), "")
		expect(t, admin.Create(ctx, pod("anywhere", func(s *corev1.PodSpec) { s.NodeSelector = nil })),
			"runs on the tenant pool: its spec.nodeSelector holds crewlet.ai/pool=tenant")
		expect(t, admin.Create(ctx, pod("system", func(s *corev1.PodSpec) {
			s.NodeSelector = map[string]string{poolLabel: "system"}
		})), "runs on the tenant pool")
		expect(t, admin.Create(ctx, pod("untolerated", func(s *corev1.PodSpec) { s.Tolerations = nil })),
			"tolerates the tenant pool's taint: crewlet.ai/pool=tenant:NoSchedule")
		expect(t, admin.Create(ctx, pod("tolerates-any", func(s *corev1.PodSpec) {
			s.Tolerations = []corev1.Toleration{{
				Key: poolLabel, Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule,
			}}
		})), "")
	})

	t.Run("moves a tenant between rings and tiers, never between tenants, organizations or cells", func(t *testing.T) {
		relabel := func(change map[string]string, drop ...string) error {
			got := &corev1.Namespace{}
			if err := admin.Get(ctx, client.ObjectKey{Name: nsName}, got); err != nil {
				t.Fatal(err)
			}
			base := got.DeepCopy()
			maps.Copy(got.Labels, change)
			for _, k := range drop {
				delete(got.Labels, k)
			}
			return p.Patch(ctx, got, client.MergeFrom(base))
		}
		expect(t, relabel(map[string]string{ringLabel: "r2", "crewlet.ai/trust-tier": "t2"}), "")
		expect(t, relabel(map[string]string{quarantine: "egress"}), "")
		expect(t, relabel(map[string]string{quarantine: "full"}), "")
		expect(t, relabel(nil, quarantine), "")
		expect(t, relabel(map[string]string{ringLabel: "r7"}), closedValues)
		expect(t, relabel(nil, ringLabel), "carries every one of these labels from its creation on")
		expect(t, relabel(map[string]string{tenantLabel: tenantID + "x"}), "may not set, change or remove these labels")
		expect(t, relabel(map[string]string{"crewlet.ai/org": "other"}), "may not set, change or remove these labels")
		rehomed := map[string]string{"crewlet.ai/cell": "aws-usw2-sh-prod-0001"}
		expect(t, relabel(rehomed), "may not set, change or remove these labels")
		expect(t, relabel(map[string]string{podSecurityEnforce: privileged}), "under pod-security.kubernetes.io/")
	})
}

func TestInternalCA(t *testing.T) {
	out, err := render("--set", "internalTLS.enabled=true")
	if err != nil {
		t.Fatal(err)
	}
	byKindName := map[string]*unstructured.Unstructured{}
	for _, obj := range objectsOf(t, string(out)) {
		if obj.GetAPIVersion() == "cert-manager.io/v1" {
			byKindName[obj.GetKind()+"/"+obj.GetName()] = obj
		}
	}
	const ca = cellRelease + "-crewlet-operator-internal-ca"
	if len(byKindName) != 3 {
		t.Fatalf("rendered %d cert-manager objects, want a root issuer, the CA and its issuer", len(byKindName))
	}
	cert := byKindName["Certificate/"+ca]
	if cert == nil || cert.GetNamespace() != "cert-manager" {
		t.Fatalf("the CA Certificate is not in cert-manager's namespace: %v", cert)
	}
	if isCA, _, _ := unstructured.NestedBool(cert.Object, "spec", "isCA"); !isCA {
		t.Fatal("the CA Certificate is not a CA")
	}
	issuer, _, _ := unstructured.NestedString(cert.Object, "spec", "issuerRef", "name")
	if byKindName["ClusterIssuer/"+issuer] == nil {
		t.Fatalf("the CA is issued by %q, which the chart does not render", issuer)
	}
	secret, _, _ := unstructured.NestedString(byKindName["ClusterIssuer/"+ca].Object, "spec", "ca", "secretName")
	if secret != ca {
		t.Fatalf("the CA issuer reads Secret %q, want the CA's own %q", secret, ca)
	}
	plain, err := render()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "cert-manager.io/") {
		t.Fatal("the chart renders cert-manager objects by default; it must install without cert-manager")
	}
}

// render is the chart as the cell's release renders it with the values given.
func render(values ...string) ([]byte, error) {
	args := append([]string{"template", cellRelease, filepath.Join("..", "..", "dist", "chart"),
		"--namespace", releaseNamespace}, values...)
	return exec.CommandContext(ctx, helmBinary(), args...).Output()
}
