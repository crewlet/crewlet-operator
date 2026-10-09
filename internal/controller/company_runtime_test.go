package controller

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/yaml"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// operatorNamespace stands for the namespace the operator runs in.
const operatorNamespace = "crewlet-system"

// operatorPodLabels stand for the labels of the operator's pods.
var operatorPodLabels = map[string]string{"app.kubernetes.io/name": "crewlet-operator"}

// The Company every runtime spec creates, the names of what the operator
// creates for it, and of what the specs create around it.
const (
	companyName        = "nimbus"
	companyDisplayName = "Nimbus"
	engineImageV020    = "ghcr.io/crewlet/crewlet:v0.2.0"
	keyringName        = "nimbus-keyring"
	tokensName         = "nimbus-api-tokens"
	accessName         = "nimbus-api-access"
	engineName         = "nimbus-engine"
	webhooksRoute      = "nimbus-webhooks"
	gatewayNamespace   = "gateways"
	publicGateway      = "public"
	internalGateway    = "internal"
	otlpSecret         = "otlp"
	platformTokenVar   = "PLATFORM_TOKEN"
	webhooksHostname   = "{{uid}}.hooks.example.com"
	dataName           = "nimbus-data"
	dataClaimName      = "data-nimbus-data-0"
	bootstrapName      = "nimbus-bootstrap"
	objectsSecret      = "nimbus-objects"
)

// newReconciler is the Company reconciler as the manager builds it, on a
// cluster that serves the Gateway API. The suite's client reads from the API
// server directly, so it stands for both the cached client and the
// APIReader.
func newReconciler() *CompanyReconciler {
	return &CompanyReconciler{
		Client:            k8sClient,
		APIReader:         k8sClient,
		GatewayAPI:        true,
		OperatorNamespace: operatorNamespace,
		OperatorPodLabels: operatorPodLabels,
	}
}

// egressRules describes a NetworkPolicy's egress rules, a line for each
// rule's ports and for each peer it admits, so that a test can state them.
func egressRules(policy *networkingv1.NetworkPolicy) []string {
	var out []string
	for _, rule := range policy.Spec.Egress {
		if len(rule.Ports) > 0 {
			ports := make([]string, 0, len(rule.Ports))
			for _, p := range rule.Ports {
				ports = append(ports, fmt.Sprintf("%s/%s", *p.Protocol, p.Port))
			}
			out = append(out, "ports "+strings.Join(ports, " "))
		}
		for _, peer := range rule.To {
			switch {
			case peer.IPBlock != nil:
				out = append(out, fmt.Sprintf("to %s except %v", peer.IPBlock.CIDR, peer.IPBlock.Except))
			case peer.NamespaceSelector == nil:
				out = append(out, fmt.Sprintf("to pods {%s} in the policy's namespace", selectorText(peer.PodSelector)))
			default:
				out = append(out, fmt.Sprintf("to pods {%s} in namespaces {%s}",
					selectorText(peer.PodSelector), selectorText(peer.NamespaceSelector)))
			}
		}
	}
	return out
}

// relayURL is the public address a Company states when something outside the
// operator forwards its webhooks.
const relayURL = "https://relay.example.com/nimbus"

// ingressRules is a NetworkPolicy's ingress as one line per port and peer,
// such as "TCP/8081 from pods {any} in namespaces {...}", and "TCP/8080 from
// anywhere" for a rule that names no peer.
func ingressRules(policy *networkingv1.NetworkPolicy) []string {
	var out []string
	for _, rule := range policy.Spec.Ingress {
		for _, p := range rule.Ports {
			port := fmt.Sprintf("%s/%s", *p.Protocol, p.Port)
			if len(rule.From) == 0 {
				out = append(out, port+" from anywhere")
			}
			for _, peer := range rule.From {
				if peer.NamespaceSelector == nil {
					out = append(out, fmt.Sprintf("%s from pods {%s} in the policy's namespace", port, selectorText(peer.PodSelector)))
					continue
				}
				out = append(out, fmt.Sprintf("%s from pods {%s} in namespaces {%s}",
					port, selectorText(peer.PodSelector), selectorText(peer.NamespaceSelector)))
			}
		}
	}
	return out
}

// fromNamespaces is how ingressRules writes a peer of every pod in the
// namespaces named.
func fromNamespaces(port int, names ...string) string {
	return fmt.Sprintf("TCP/%d from pods {any} in namespaces {%s}", port, selectorText(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{Key: corev1.LabelMetadataName, Operator: metav1.LabelSelectorOpIn, Values: names}},
	}))
}

// envNamed is the values of the variables of that name in an environment.
func envNamed(env []corev1.EnvVar, name string) []string {
	var out []string
	for _, e := range env {
		if e.Name == name {
			out = append(out, e.Value)
		}
	}
	return out
}

// selectorText is a peer's selector as text; "any" for one that is unset or
// empty, which a NetworkPolicy peer reads as selecting everything.
func selectorText(s *metav1.LabelSelector) string {
	if s == nil || (len(s.MatchLabels) == 0 && len(s.MatchExpressions) == 0) {
		return "any"
	}
	return metav1.FormatLabelSelector(s)
}

var _ = Describe("Company runtime", func() {
	var namespace string

	BeforeEach(func() {
		ns := &corev1.Namespace{GenerateName: "runtime-"}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespace = ns.Name
	})

	// newClass creates an EngineClass named after the spec's namespace (classes
	// are cluster-scoped) and deletes it when the spec ends.
	newClass := func(mutate ...func(*crewletaiv1alpha1.EngineClass)) *crewletaiv1alpha1.EngineClass {
		GinkgoHelper()
		class := &crewletaiv1alpha1.EngineClass{
			Name: namespace,
			Spec: crewletaiv1alpha1.EngineClassSpec{
				Engine: crewletaiv1alpha1.EngineSettings{Image: engineImageV020},
				Data: crewletaiv1alpha1.DataPodSettings{
					Storage: crewletaiv1alpha1.DataStorage{Size: new(resource.MustParse("10Gi"))},
				},
				Retention: crewletaiv1alpha1.RetentionSettings{BackupOwner: "platform-oncall"},
			},
		}
		for _, m := range mutate {
			m(class)
		}
		Expect(k8sClient.Create(ctx, class)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, class))).To(Succeed()) })
		return class
	}
	updateClass := func(class *crewletaiv1alpha1.EngineClass, mutate func(*crewletaiv1alpha1.EngineClass)) {
		GinkgoHelper()
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(class), class)).To(Succeed())
		mutate(class)
		Expect(k8sClient.Update(ctx, class)).To(Succeed())
	}

	newCompany := func(class string, mutate ...func(*crewletaiv1alpha1.Company)) *crewletaiv1alpha1.Company {
		GinkgoHelper()
		company := &crewletaiv1alpha1.Company{
			Namespace: namespace,
			Name:      companyName,
			Spec:      crewletaiv1alpha1.CompanySpec{CompanyName: companyDisplayName, EngineClassName: class},
		}
		for _, m := range mutate {
			m(company)
		}
		Expect(k8sClient.Create(ctx, company)).To(Succeed())
		return company
	}
	updateCompany := func(company *crewletaiv1alpha1.Company, mutate func(*crewletaiv1alpha1.Company)) {
		GinkgoHelper()
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(company), company)).To(Succeed())
		mutate(company)
		Expect(k8sClient.Update(ctx, company)).To(Succeed())
	}

	reconcileWith := func(r *CompanyReconciler, company *crewletaiv1alpha1.Company) ctrl.Result {
		GinkgoHelper()
		result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(company)})
		Expect(err).NotTo(HaveOccurred())
		return result
	}
	// reconciled reconciles the Company and returns it as it is afterwards.
	reconciled := func(company *crewletaiv1alpha1.Company) *crewletaiv1alpha1.Company {
		GinkgoHelper()
		reconcileWith(newReconciler(), company)
		got := &crewletaiv1alpha1.Company{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(company), got)).To(Succeed())
		return got
	}
	condition := func(company *crewletaiv1alpha1.Company, t string) metav1.Condition {
		GinkgoHelper()
		c := meta.FindStatusCondition(company.Status.Conditions, t)
		Expect(c).NotTo(BeNil(), "the Company has no %s condition", t)
		Expect(c.ObservedGeneration).To(Equal(company.Generation), "%s describes an older generation", t)
		return *c
	}
	get := func(obj client.Object, name string) {
		GinkgoHelper()
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj)).To(Succeed())
	}
	absent := func(obj client.Object, name string) {
		GinkgoHelper()
		err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "%T %s exists (err=%v)", obj, name, err)
	}
	statefulSet := func() *appsv1.StatefulSet {
		GinkgoHelper()
		sts := &appsv1.StatefulSet{}
		get(sts, dataName)
		return sts
	}
	engine := func(sts *appsv1.StatefulSet) corev1.Container {
		GinkgoHelper()
		Expect(sts.Spec.Template.Spec.Containers).To(HaveLen(1))
		return sts.Spec.Template.Spec.Containers[0]
	}
	// rollOut stands in for the StatefulSet controller and the kubelet, which
	// envtest does not run: it reports every replica of the current template
	// ready.
	rollOut := func() {
		GinkgoHelper()
		sts := statefulSet()
		n := *sts.Spec.Replicas
		sts.Status = appsv1.StatefulSetStatus{
			ObservedGeneration: sts.Generation,
			Replicas:           n,
			ReadyReplicas:      n,
			AvailableReplicas:  n,
			UpdatedReplicas:    n,
			CurrentReplicas:    n,
			CurrentRevision:    fmt.Sprintf("nimbus-data-%d", sts.Generation),
			UpdateRevision:     fmt.Sprintf("nimbus-data-%d", sts.Generation),
		}
		Expect(k8sClient.Status().Update(ctx, sts)).To(Succeed())
	}
	// expectedBootstrap is what render.Bootstrap makes of the Company and its
	// class, with the keyring and tokens the Company's Secrets hold.
	expectedBootstrap := func(company *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass) *render.BootstrapDocument {
		GinkgoHelper()
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(class), class)).To(Succeed())
		keyring, tokens := &corev1.Secret{}, &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: company.Namespace, Name: keyringName}, keyring)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: company.Namespace, Name: tokensName}, tokens)).To(Succeed())
		doc, err := render.Bootstrap(&render.BootstrapInput{
			Company: company,
			Class:   class,
			Keyring: keyringOf(keyring),
			Tokens:  tokensOf(tokens),
			// Every data StatefulSet here is created from newClass's
			// volume, and keeps it when the class changes later.
			VolumeSize: resource.MustParse("10Gi"),
		})
		Expect(err).NotTo(HaveOccurred())
		return doc
	}
	ownedByCompany := func(obj client.Object, company *crewletaiv1alpha1.Company) {
		GinkgoHelper()
		owner := metav1.GetControllerOf(obj)
		Expect(owner).NotTo(BeNil(), "%T %s has no controller", obj, obj.GetName())
		Expect(owner.UID).To(Equal(company.UID))
		Expect(owner.Kind).To(Equal("Company"))
		Expect(obj.GetLabels()).To(HaveKeyWithValue(crewletaiv1alpha1.CompanyLabel, company.Name))
		Expect(obj.GetLabels()).To(HaveKeyWithValue("app.kubernetes.io/managed-by", FieldOwner))
	}

	It("creates every object of an accepted Company's engine", func() {
		class := newClass()
		company := reconciled(newCompany(class.Name))
		Expect(company.Finalizers).To(ContainElement(crewletaiv1alpha1.CompanyFinalizer))

		By("generating the keyring in the form Crewlet's keygen prints")
		keyring := &corev1.Secret{}
		get(keyring, keyringName)
		ownedByCompany(keyring, company)
		Expect(keyring.Annotations).To(HaveKeyWithValue(KeyringActiveKeyAnnotation, "key-1"))
		Expect(keyring.Data).To(HaveLen(1))
		material, err := base64.StdEncoding.DecodeString(string(keyring.Data["key-1"]))
		Expect(err).NotTo(HaveOccurred())
		Expect(material).To(HaveLen(32))

		By("generating two different API tokens")
		tokens := &corev1.Secret{}
		get(tokens, tokensName)
		ownedByCompany(tokens, company)
		Expect(slices.Sorted(maps.Keys(tokens.Data))).To(Equal([]string{render.TokenAccess, render.TokenOperator}))
		Expect(len(tokens.Data[render.TokenOperator])).To(BeNumerically(">=", 43))
		Expect(tokens.Data[render.TokenOperator]).NotTo(Equal(tokens.Data[render.TokenAccess]))

		By("publishing the access token and the API's address, and not the operator's token")
		access := &corev1.Secret{}
		get(access, accessName)
		ownedByCompany(access, company)
		Expect(access.Data).To(Equal(map[string][]byte{
			crewletaiv1alpha1.APIAccessTokenKey: tokens.Data[render.TokenAccess],
			crewletaiv1alpha1.APIAccessURLKey:   []byte("http://nimbus-engine." + namespace + ".svc:8080"),
		}))
		Expect(company.Status.APIAccessSecretRef.Name).To(Equal(accessName))
		Expect(company.Status.Endpoints.API).To(Equal("http://nimbus-engine." + namespace + ".svc:8080"))

		By("rendering the bootstrap document, credentials included, into a Secret")
		want := expectedBootstrap(company, class)
		bootstrap := &corev1.Secret{}
		get(bootstrap, bootstrapName)
		ownedByCompany(bootstrap, company)
		Expect(bootstrap.Data).To(Equal(map[string][]byte{configFile: want.YAML}))
		Expect(string(want.YAML)).To(ContainSubstring(string(tokens.Data[render.TokenOperator])))
		Expect(string(want.YAML)).To(ContainSubstring(string(keyring.Data["key-1"])))

		By("creating a ServiceAccount whose token is never mounted")
		sa := &corev1.ServiceAccount{}
		get(sa, engineName)
		ownedByCompany(sa, company)
		Expect(*sa.AutomountServiceAccountToken).To(BeFalse())

		By("creating the engine Service and the headless data Service")
		dataSelector := map[string]string{
			"app.kubernetes.io/name":       "crewlet",
			"app.kubernetes.io/instance":   companyName,
			"app.kubernetes.io/component":  "data",
			crewletaiv1alpha1.CompanyLabel: companyName,
		}
		engineSvc := &corev1.Service{}
		get(engineSvc, engineName)
		ownedByCompany(engineSvc, company)
		Expect(engineSvc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		Expect(engineSvc.Spec.Selector).To(Equal(dataSelector))
		Expect(engineSvc.Spec.Ports).To(HaveLen(2))
		Expect(engineSvc.Spec.Ports[0].Name).To(Equal("http"))
		Expect(engineSvc.Spec.Ports[0].Port).To(Equal(int32(8080)))
		Expect(engineSvc.Spec.Ports[0].TargetPort).To(Equal(intstr.FromString("http")))
		Expect(engineSvc.Spec.Ports[1].Name).To(Equal(publicPortName))
		Expect(engineSvc.Spec.Ports[1].Port).To(Equal(int32(8081)))
		Expect(engineSvc.Spec.Ports[1].TargetPort).To(Equal(intstr.FromString(publicPortName)))
		dataSvc := &corev1.Service{}
		get(dataSvc, dataName)
		Expect(dataSvc.Spec.ClusterIP).To(Equal(corev1.ClusterIPNone))
		Expect(dataSvc.Spec.PublishNotReadyAddresses).To(BeTrue())
		Expect(dataSvc.Spec.Selector).To(Equal(dataSelector))

		By("creating the data StatefulSet")
		sts := statefulSet()
		ownedByCompany(sts, company)
		Expect(*sts.Spec.Replicas).To(Equal(int32(1)))
		Expect(sts.Spec.ServiceName).To(Equal(dataName))
		Expect(sts.Spec.Selector.MatchLabels).To(Equal(dataSelector))
		Expect(sts.Spec.PodManagementPolicy).To(Equal(appsv1.ParallelPodManagement))
		Expect(sts.Spec.UpdateStrategy.Type).To(Equal(appsv1.RollingUpdateStatefulSetStrategyType))
		Expect(sts.Spec.PersistentVolumeClaimRetentionPolicy).To(Equal(&appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
			WhenDeleted: appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
		}))
		Expect(sts.Spec.VolumeClaimTemplates).To(HaveLen(1))
		claim := sts.Spec.VolumeClaimTemplates[0]
		Expect(claim.Name).To(Equal("data"))
		Expect(claim.Spec.Resources.Requests.Storage().String()).To(Equal("10Gi"))
		Expect(claim.Labels).To(HaveKeyWithValue(crewletaiv1alpha1.CompanyLabel, companyName))

		pod := sts.Spec.Template
		Expect(pod.Labels).To(HaveKeyWithValue(crewletaiv1alpha1.CompanyLabel, companyName))
		Expect(pod.Annotations).To(HaveKeyWithValue(BootstrapHashAnnotation, hashOf(bootstrap.Data)))
		Expect(pod.Spec.ServiceAccountName).To(Equal(engineName))
		Expect(*pod.Spec.AutomountServiceAccountToken).To(BeFalse())
		Expect(*pod.Spec.EnableServiceLinks).To(BeFalse())
		Expect(*pod.Spec.TerminationGracePeriodSeconds).To(Equal(int64(300)))
		Expect(*pod.Spec.SecurityContext.RunAsNonRoot).To(BeTrue())
		Expect(*pod.Spec.SecurityContext.RunAsUser).To(Equal(int64(10001)))
		Expect(pod.Spec.SecurityContext.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))

		container := engine(sts)
		Expect(container.Image).To(Equal(engineImageV020))
		Expect(container.Args).To(Equal([]string{"run", "-config", "/etc/crewlet/crewlet.yaml"}))
		Expect(*container.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
		Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
		Expect(container.SecurityContext.Capabilities.Drop).To(Equal([]corev1.Capability{"ALL"}))
		Expect(container.LivenessProbe.HTTPGet.Path).To(Equal("/health"))
		Expect(container.LivenessProbe.PeriodSeconds*container.LivenessProbe.FailureThreshold).To(BeNumerically(">", 45),
			"liveness restarts the engine before its watchdog's 45 seconds")
		Expect(container.StartupProbe.HTTPGet.Path).To(Equal("/health"))
		Expect(container.ReadinessProbe.HTTPGet.Path).To(Equal("/ready"))
		Expect(container.ReadinessProbe.HTTPGet.Port).To(Equal(intstr.FromString("http")), "probed on the public port, which serves no probe")
		Expect(container.Ports).To(ConsistOf(
			corev1.ContainerPort{Name: portName, ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
			corev1.ContainerPort{Name: publicPortName, ContainerPort: 8081, Protocol: corev1.ProtocolTCP},
		))
		Expect(container.Lifecycle.PreStop.Sleep.Seconds).To(BeNumerically(">", 0))
		// The environment starts with exactly what the bootstrap document
		// references, wired as render says.
		Expect(container.Env[:len(want.Env)]).To(Equal(want.Env))
		Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "TURSO_GO_CACHE_DIR", Value: "/var/lib/crewlet/cache"}))
		for _, env := range container.Env {
			Expect(env.Name).NotTo(HavePrefix("OTEL_"), "no telemetry without a class's telemetry")
			Expect(env.Name).NotTo(BeElementOf("CREWLET_SANDBOX_OTEL_RECEIVER_URL", "CREWLET_MCP_BRIDGE_URL"))
			// The engine expands ${NAME} in its company document from
			// its environment: no credential may be there.
			Expect(env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil).To(BeTrue(),
				"%s reads Secret %v into the engine's environment", env.Name, env.ValueFrom)
		}
		Expect(container.VolumeMounts).To(ContainElements(
			corev1.VolumeMount{Name: "data", MountPath: "/var/lib/crewlet"},
			corev1.VolumeMount{Name: configVolumeName, MountPath: "/etc/crewlet", ReadOnly: true},
		))
		Expect(pod.Spec.Volumes).To(ContainElement(corev1.Volume{Name: configVolumeName,
			Secret: &corev1.SecretVolumeSource{
				SecretName:  bootstrapName,
				Items:       []corev1.KeyToPath{{Key: configFile, Path: configFile}},
				DefaultMode: new(int32(0o440)),
			},
		}))

		By("isolating the data pods")
		policy := &networkingv1.NetworkPolicy{}
		get(policy, engineName)
		ownedByCompany(policy, company)
		Expect(policy.Spec.PodSelector.MatchLabels).To(Equal(dataSelector))
		Expect(policy.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}))
		// The operator's pods, and not the rest of its namespace, to the
		// API port alone; nothing to the public port, which nothing routes.
		Expect(ingressRules(policy)).To(ConsistOf(fmt.Sprintf("TCP/8080 from pods {%s} in namespaces {%s}",
			selectorText(&metav1.LabelSelector{MatchLabels: operatorPodLabels}),
			selectorText(&metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: corev1.LabelMetadataName, Operator: metav1.LabelSelectorOpIn, Values: []string{operatorNamespace},
			}}}))))
		By("letting the engine reach DNS, its own namespace and outside the cluster, but no metadata endpoint")
		Expect(egressRules(policy)).To(ConsistOf(
			"ports UDP/53 TCP/53",
			"to pods {any} in the policy's namespace",
			"to "+anyIPv4+" except [169.254.169.254/32]",
			"to "+anyIPv6+" except [fd00:ec2::254/128]",
		))

		By("routing nothing from outside the cluster")
		absent(&networkingv1.Ingress{}, webhooksRoute)
		absent(&gatewayv1.HTTPRoute{}, webhooksRoute)

		By("reporting the pods as not ready yet")
		Expect(company.Status.ObservedGeneration).To(Equal(company.Generation))
		Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonPodsNotReady))
		exposed := condition(company, crewletaiv1alpha1.ConditionExposed)
		Expect(exposed.Status).To(Equal(metav1.ConditionTrue))
		Expect(exposed.Reason).To(Equal(crewletaiv1alpha1.ReasonNoRoutes))
		ready := condition(company, crewletaiv1alpha1.ConditionReady)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(crewletaiv1alpha1.ReasonPodsNotReady))
		Expect(company.Status.Endpoints.PublicBaseURL).To(BeEmpty())
		Expect(company.Status.Endpoints.DashboardBaseURL).To(BeEmpty())
		Expect(exposed.Message).To(ContainSubstring("compose no link for a person"))
	})

	It("admits the class's API consumers and narrows the engine's egress as the class says", func() {
		platform := metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: "platform"}}
		class := newClass(func(c *crewletaiv1alpha1.EngineClass) {
			c.Spec.NetworkPolicy = crewletaiv1alpha1.NetworkPolicySettings{
				Mode: crewletaiv1alpha1.NetworkPolicyIsolated,
				APIConsumers: []crewletaiv1alpha1.NetworkPolicyPeer{{
					NamespaceSelector: &platform,
					PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app": "control-plane"}},
				}},
				Egress: crewletaiv1alpha1.NetworkPolicyEgress{
					ExceptCIDRs: []string{"10.0.0.0/8", "fd00:10::/48", "192.168.0.0/16"},
					To:          []crewletaiv1alpha1.NetworkPolicyPeer{{NamespaceSelector: &platform}},
				},
			}
		})
		reconciled(newCompany(class.Name))

		policy := &networkingv1.NetworkPolicy{}
		get(policy, engineName)
		Expect(ingressRules(policy)).To(ConsistOf(
			HavePrefix("TCP/8080 from pods {"+selectorText(&metav1.LabelSelector{MatchLabels: operatorPodLabels})),
			"TCP/8080 from pods {app=control-plane} in namespaces {kubernetes.io/metadata.name=platform}",
		), "admits more than the operator and the class's consumer to the API port, or a consumer to the public port")
		Expect(egressRules(policy)).To(ConsistOf(
			"ports UDP/53 TCP/53",
			"to pods {any} in the policy's namespace",
			"to "+anyIPv4+" except [169.254.169.254/32 10.0.0.0/8 192.168.0.0/16]",
			"to "+anyIPv6+" except [fd00:ec2::254/128 fd00:10::/48]",
			"to pods {any} in namespaces {kubernetes.io/metadata.name=platform}",
		))
	})

	It("changes nothing on a second reconcile", func() {
		class := newClass(func(c *crewletaiv1alpha1.EngineClass) {
			c.Spec.Exposure.Webhooks = crewletaiv1alpha1.WebhookExposure{
				Mode:     crewletaiv1alpha1.WebhookExposureGateway,
				Gateway:  crewletaiv1alpha1.GatewayExposure{ParentRefs: []crewletaiv1alpha1.GatewayParentReference{{Namespace: gatewayNamespace, Name: publicGateway}}},
				Hostname: webhooksHostname,
			}
			c.Spec.Telemetry = crewletaiv1alpha1.TelemetrySettings{OTLPEndpoint: "http://collector.observability:4318"}
		})
		company := newCompany(class.Name, func(c *crewletaiv1alpha1.Company) {
			c.Spec.Runtime.Env = []crewletaiv1alpha1.EnvVar{{Name: "TEAM", Value: new("platform")}}
		})
		reconciled(company)

		objects := []struct {
			name string
			obj  client.Object
		}{
			{keyringName, &corev1.Secret{}},
			{tokensName, &corev1.Secret{}},
			{accessName, &corev1.Secret{}},
			{bootstrapName, &corev1.Secret{}},
			{engineName, &corev1.Service{}},
			{dataName, &corev1.Service{}},
			{webhooksRoute, &gatewayv1.HTTPRoute{}},
			{engineName, &networkingv1.NetworkPolicy{}},
		}
		versions := func() map[string]string {
			GinkgoHelper()
			out := map[string]string{}
			for _, o := range objects {
				get(o.obj, o.name)
				out[fmt.Sprintf("%T/%s", o.obj, o.name)] = o.obj.GetResourceVersion()
			}
			out["StatefulSet"] = statefulSet().ResourceVersion
			sa := &corev1.ServiceAccount{}
			get(sa, engineName)
			out["ServiceAccount"] = sa.ResourceVersion
			c := &crewletaiv1alpha1.Company{}
			get(c, companyName)
			out["Company"] = c.ResourceVersion
			return out
		}
		before := versions()
		reconciled(company)
		Expect(versions()).To(Equal(before))
	})

	It("never regenerates the keyring or the API tokens", func() {
		class := newClass()
		company := reconciled(newCompany(class.Name))
		keyring, tokens := &corev1.Secret{}, &corev1.Secret{}
		get(keyring, keyringName)
		get(tokens, tokensName)

		updateClass(class, func(c *crewletaiv1alpha1.EngineClass) { c.Spec.Retention.BackupOwner = "storage-team" })
		updateCompany(company, func(c *crewletaiv1alpha1.Company) { c.Spec.Mission = "Ship it." })
		reconciled(company)

		keyringAfter, tokensAfter := &corev1.Secret{}, &corev1.Secret{}
		get(keyringAfter, keyringName)
		get(tokensAfter, tokensName)
		Expect(keyringAfter.Data).To(Equal(keyring.Data))
		Expect(keyringAfter.ResourceVersion).To(Equal(keyring.ResourceVersion))
		Expect(tokensAfter.Data).To(Equal(tokens.Data))
		Expect(tokensAfter.ResourceVersion).To(Equal(tokens.ResourceVersion))
	})

	It("adopts a keyring kept from an earlier Company without changing it", func() {
		class := newClass()
		material := base64.StdEncoding.EncodeToString(make([]byte, 32))
		kept := &corev1.Secret{
			Namespace:   namespace,
			Name:        keyringName,
			Annotations: map[string]string{KeyringActiveKeyAnnotation: "k7"},
			Data:        map[string][]byte{"k6": []byte(material), "k7": []byte(material)},
		}
		Expect(k8sClient.Create(ctx, kept)).To(Succeed())

		company := reconciled(newCompany(class.Name))
		keyring := &corev1.Secret{}
		get(keyring, keyringName)
		ownedByCompany(keyring, company)
		Expect(keyring.Data).To(Equal(kept.Data))
		Expect(keyring.Annotations).To(HaveKeyWithValue(KeyringActiveKeyAnnotation, "k7"))

		bootstrap := &corev1.Secret{}
		get(bootstrap, bootstrapName)
		Expect(bootstrap.Data[configFile]).To(Equal(expectedBootstrap(company, class).YAML))
		Expect(string(bootstrap.Data[configFile])).To(ContainSubstring("active_key_id: k7"))
	})

	It("uses API tokens another controller supplies as they are, and holds the pods back while one is unusable", func() {
		// 32 random bytes in base64 and in hex: the forms a platform's
		// secret store would generate.
		suppliedOperator := "q83vEjRWeJq83vEjRWeJq83vEjRWeJq83vEjRWeJq80="
		suppliedAccess := strings.Repeat("0123456789abcdef", 4)
		class := newClass()
		// Any object can be a Secret's controller; a ConfigMap stands for
		// the ExternalSecret a platform would use.
		supplier := &corev1.ConfigMap{Namespace: namespace, Name: "token-supplier"}
		Expect(k8sClient.Create(ctx, supplier)).To(Succeed())
		supplied := &corev1.Secret{
			Namespace: namespace,
			Name:      tokensName,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "v1", Kind: "ConfigMap", Name: supplier.Name, UID: supplier.UID, Controller: new(true),
			}},
			Data: map[string][]byte{render.TokenOperator: []byte(suppliedOperator)},
		}
		Expect(k8sClient.Create(ctx, supplied)).To(Succeed())

		company := reconciled(newCompany(class.Name))
		runtimeReady := condition(company, crewletaiv1alpha1.ConditionRuntimeReady)
		Expect(runtimeReady.Reason).To(Equal(crewletaiv1alpha1.ReasonAPITokensInvalid))
		Expect(runtimeReady.Message).To(ContainSubstring("it has no key " + render.TokenAccess))
		absent(&appsv1.StatefulSet{}, dataName)
		absent(&corev1.Secret{}, accessName)

		tokens := &corev1.Secret{}
		get(tokens, tokensName)
		Expect(metav1.GetControllerOf(tokens).UID).To(Equal(supplier.UID), "adopted a Secret another controller owns")
		By("supplying an access token too short to guard the engine")
		tokens.Data[render.TokenAccess] = []byte("supplied-access-token")
		Expect(k8sClient.Update(ctx, tokens)).To(Succeed())
		company = reconciled(company)
		runtimeReady = condition(company, crewletaiv1alpha1.ConditionRuntimeReady)
		Expect(runtimeReady.Reason).To(Equal(crewletaiv1alpha1.ReasonAPITokensInvalid))
		Expect(runtimeReady.Message).To(ContainSubstring("its access token is 21 characters long"))
		absent(&appsv1.StatefulSet{}, dataName)
		absent(&corev1.Secret{}, accessName)

		By("supplying one with a ${NAME} the engine would expand")
		tokens.Data[render.TokenAccess] = []byte(strings.Repeat("a", 40) + "${PATH}")
		Expect(k8sClient.Update(ctx, tokens)).To(Succeed())
		company = reconciled(company)
		Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Message).
			To(ContainSubstring("its access token holds characters outside base64 and base64url"))
		absent(&appsv1.StatefulSet{}, dataName)

		By("supplying a usable one")
		tokens.Data[render.TokenAccess] = []byte(suppliedAccess)
		Expect(k8sClient.Update(ctx, tokens)).To(Succeed())
		company = reconciled(company)
		Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonPodsNotReady))
		access := &corev1.Secret{}
		get(access, accessName)
		Expect(string(access.Data[crewletaiv1alpha1.APIAccessTokenKey])).To(Equal(suppliedAccess))
		get(tokens, tokensName)
		Expect(string(tokens.Data[render.TokenOperator])).To(Equal(suppliedOperator))
	})

	It("holds the pods back on a keyring it cannot read, and never rewrites it", func() {
		class := newClass()
		broken := &corev1.Secret{
			Namespace: namespace,
			Name:      keyringName,
			Data:      map[string][]byte{"key-1": []byte("material")},
		}
		Expect(k8sClient.Create(ctx, broken)).To(Succeed())

		company := reconciled(newCompany(class.Name))
		runtimeReady := condition(company, crewletaiv1alpha1.ConditionRuntimeReady)
		Expect(runtimeReady.Status).To(Equal(metav1.ConditionFalse))
		Expect(runtimeReady.Reason).To(Equal(crewletaiv1alpha1.ReasonKeyringInvalid))
		Expect(runtimeReady.Message).To(ContainSubstring(KeyringActiveKeyAnnotation))
		absent(&appsv1.StatefulSet{}, dataName)
		keyring := &corev1.Secret{}
		get(keyring, keyringName)
		Expect(keyring.Data).To(Equal(broken.Data))
		Expect(keyring.Annotations).NotTo(HaveKey(KeyringActiveKeyAnnotation))
	})

	It("never generates a keyring while the company's data exists", func() {
		class := newClass()
		company := reconciled(newCompany(class.Name))
		rollOut()
		before := statefulSet().Spec.Template.Annotations
		claim := &corev1.PersistentVolumeClaim{
			Namespace: namespace,
			Name:      dataClaimName,
			Labels:    selectorFor(company, componentData),
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
				},
			},
		}
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())

		By("holding the pods back while the StatefulSet and its claim exist")
		keyring := &corev1.Secret{}
		get(keyring, keyringName)
		Expect(k8sClient.Delete(ctx, keyring)).To(Succeed())
		company = reconciled(company)
		absent(&corev1.Secret{}, keyringName)
		runtimeReady := condition(company, crewletaiv1alpha1.ConditionRuntimeReady)
		Expect(runtimeReady.Reason).To(Equal(crewletaiv1alpha1.ReasonKeyringMissing))
		Expect(runtimeReady.Message).To(ContainSubstring("StatefulSet nimbus-data"))
		Expect(runtimeReady.Message).To(ContainSubstring("PersistentVolumeClaim data-nimbus-data-0"))
		Expect(statefulSet().Spec.Template.Annotations).To(Equal(before), "the pods rolled onto another keyring")

		By("holding them back on the claim alone")
		Expect(k8sClient.Delete(ctx, statefulSet())).To(Succeed())
		company = reconciled(company)
		absent(&corev1.Secret{}, keyringName)
		absent(&appsv1.StatefulSet{}, dataName)
		Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonKeyringMissing))

		By("generating one once the claim is being deleted, with the data it holds")
		Expect(k8sClient.Delete(ctx, claim)).To(Succeed())
		company = reconciled(company)
		get(&corev1.Secret{}, keyringName)
		Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonPodsNotReady))
	})

	It("waits for the Secrets of an earlier Company of the same name to be collected", func() {
		class := newClass()
		// What a deletionPolicy Delete of an earlier nimbus leaves until the
		// garbage collector, which envtest does not run, deletes it.
		stale := &corev1.Secret{
			Namespace: namespace,
			Name:      keyringName,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: crewletaiv1alpha1.GroupVersion.String(), Kind: "Company", Name: companyName,
				UID: "0e4d7c1a-5b8e-4a3e-9d8f-1c2b3a4d5e6f", Controller: new(true),
			}},
			Annotations: map[string]string{KeyringActiveKeyAnnotation: keyringKeyID},
			Data:        map[string][]byte{keyringKeyID: []byte(base64.StdEncoding.EncodeToString(make([]byte, 32)))},
		}
		Expect(k8sClient.Create(ctx, stale)).To(Succeed())

		company := reconciled(newCompany(class.Name))
		runtimeReady := condition(company, crewletaiv1alpha1.ConditionRuntimeReady)
		Expect(runtimeReady.Reason).To(Equal(crewletaiv1alpha1.ReasonPreviousCompanyObjects))
		Expect(runtimeReady.Message).To(ContainSubstring(keyringName))
		absent(&appsv1.StatefulSet{}, dataName)
		kept := &corev1.Secret{}
		get(kept, keyringName)
		Expect(kept.OwnerReferences).To(Equal(stale.OwnerReferences), "took over a Secret that is about to be collected")

		Expect(k8sClient.Delete(ctx, kept)).To(Succeed())
		company = reconciled(company)
		keyring := &corev1.Secret{}
		get(keyring, keyringName)
		ownedByCompany(keyring, company)
		Expect(keyring.Data).NotTo(Equal(stale.Data))
		Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonPodsNotReady))
	})

	It("reports the engine ready once its pods are, and the Company once its document is applied", func() {
		class := newClass()
		company := reconciled(newCompany(class.Name))
		rollOut()
		company = reconciled(company)

		runtimeReady := condition(company, crewletaiv1alpha1.ConditionRuntimeReady)
		Expect(runtimeReady.Status).To(Equal(metav1.ConditionTrue))
		Expect(runtimeReady.Reason).To(Equal(crewletaiv1alpha1.ReasonPodsReady))
		Expect(company.Status.Engine).To(Equal(crewletaiv1alpha1.EngineStatus{Image: engineImageV020, Version: "v0.2.0"}))
		Expect(company.Status.Topology.Data).To(Equal(crewletaiv1alpha1.DataTopologyStatus{
			Replicas: new(int32(1)), ReadyReplicas: new(int32(1)), UpdatedReplicas: new(int32(1)),
		}))
		ready := condition(company, crewletaiv1alpha1.ConditionReady)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(crewletaiv1alpha1.ReasonConfigPending))

		By("reading ConfigApplied as the config reconciler writes it")
		base := company.DeepCopy()
		meta.SetStatusCondition(&company.Status.Conditions, metav1.Condition{
			Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionTrue,
			Reason: crewletaiv1alpha1.ReasonApplied, Message: "Revision r1 is active.", ObservedGeneration: company.Generation,
		})
		Expect(k8sClient.Status().Patch(ctx, company, client.MergeFrom(base))).To(Succeed())
		company = reconciled(company)
		ready = condition(company, crewletaiv1alpha1.ConditionReady)
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(ready.Reason).To(Equal(crewletaiv1alpha1.ReasonAvailable))
		// The runtime reconciler wrote its status without dropping the
		// config reconciler's condition.
		Expect(condition(company, crewletaiv1alpha1.ConditionConfigApplied).Reason).To(Equal(crewletaiv1alpha1.ReasonApplied))
	})

	It("scales a suspended Company to zero and keeps its volumes", func() {
		class := newClass()
		company := reconciled(newCompany(class.Name))
		rollOut()
		updateCompany(company, func(c *crewletaiv1alpha1.Company) { c.Spec.Suspend = new(true) })
		company = reconciled(company)

		sts := statefulSet()
		Expect(*sts.Spec.Replicas).To(BeZero())
		Expect(sts.Spec.PersistentVolumeClaimRetentionPolicy.WhenScaled).To(Equal(appsv1.RetainPersistentVolumeClaimRetentionPolicyType))
		Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonSuspended))
		ready := condition(company, crewletaiv1alpha1.ConditionReady)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(crewletaiv1alpha1.ReasonSuspended))

		updateCompany(company, func(c *crewletaiv1alpha1.Company) { c.Spec.Suspend = new(false) })
		reconciled(company)
		Expect(*statefulSet().Spec.Replicas).To(Equal(int32(1)))
	})

	Context("suspending a Company that is not accepted", func() {
		// expectStopped checks the invariant: a suspended Company's data
		// StatefulSet has no replicas whatever Accepted says, and Ready says
		// the engine is stopped only once it is.
		expectStopped := func(company *crewletaiv1alpha1.Company, reason string) {
			GinkgoHelper()
			Expect(condition(company, crewletaiv1alpha1.ConditionAccepted).Reason).To(Equal(reason))
			Expect(*statefulSet().Spec.Replicas).To(BeZero())
			Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonSuspended))
			ready := condition(company, crewletaiv1alpha1.ConditionReady)
			Expect(ready.Reason).To(Equal(crewletaiv1alpha1.ReasonSuspended))
			Expect(ready.Message).To(ContainSubstring("its engine is stopped"))
		}

		It("stops the engine of a Company over its class's limits", func() {
			class := newClass(func(c *crewletaiv1alpha1.EngineClass) { c.Spec.Limits.MaxSeats = new(int32(1)) })
			company := reconciled(newCompany(class.Name))
			rollOut()
			for _, name := range []string{"ana", "bob"} {
				Expect(k8sClient.Create(ctx, &crewletaiv1alpha1.Seat{
					Namespace: namespace, Name: name,
					Spec: crewletaiv1alpha1.SeatSpec{CompanyRef: crewletaiv1alpha1.CompanyReference{Name: companyName}},
				})).To(Succeed())
			}
			updateCompany(company, func(c *crewletaiv1alpha1.Company) { c.Spec.Suspend = new(true) })
			expectStopped(reconciled(company), crewletaiv1alpha1.ReasonLimitExceeded)
		})

		It("stops the engine of a Company whose class is gone", func() {
			class := newClass()
			company := reconciled(newCompany(class.Name))
			rollOut()
			Expect(k8sClient.Delete(ctx, class)).To(Succeed())
			updateCompany(company, func(c *crewletaiv1alpha1.Company) { c.Spec.Suspend = new(true) })
			expectStopped(reconciled(company), crewletaiv1alpha1.ReasonEngineClassNotFound)
		})
	})

	It("does not say a suspended Company's engine is stopped before it is", func() {
		company := &crewletaiv1alpha1.Company{Spec: crewletaiv1alpha1.CompanySpec{Suspend: new(true)}}
		company.Generation = 2
		company.Status.Conditions = []metav1.Condition{{
			Type: crewletaiv1alpha1.ConditionRuntimeReady, Status: metav1.ConditionTrue,
			Reason: crewletaiv1alpha1.ReasonPodsReady, ObservedGeneration: 1,
		}}
		ready := summarize(company)
		Expect(ready.Reason).To(Equal(crewletaiv1alpha1.ReasonSuspended))
		Expect(ready.Message).NotTo(ContainSubstring("is stopped"))
	})

	It("rolls the pod template when the class changes, and keeps the volumes the StatefulSet was created with", func() {
		class := newClass()
		company := reconciled(newCompany(class.Name))
		before := statefulSet()

		updateClass(class, func(c *crewletaiv1alpha1.EngineClass) {
			c.Spec.Engine.Image = "ghcr.io/crewlet/crewlet:v0.3.0"
			c.Spec.Data.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
			}
			c.Spec.Retention.BackupOwner = "storage-team"
			c.Spec.Data.Storage.Size = new(resource.MustParse("50Gi"))
			c.Spec.PodTemplate.Spec.Tolerations = []corev1.Toleration{{Key: "crewlet.ai/tenant", Operator: corev1.TolerationOpExists}}
		})
		company = reconciled(company)

		after := statefulSet()
		Expect(after.Generation).To(BeNumerically(">", before.Generation))
		Expect(engine(after).Image).To(Equal("ghcr.io/crewlet/crewlet:v0.3.0"))
		Expect(engine(after).Resources.Requests).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("1Gi")))
		Expect(after.Spec.Template.Spec.Tolerations).To(ConsistOf(corev1.Toleration{Key: "crewlet.ai/tenant", Operator: corev1.TolerationOpExists}))
		bootstrap := &corev1.Secret{}
		get(bootstrap, bootstrapName)
		Expect(bootstrap.Data[configFile]).To(Equal(expectedBootstrap(company, class).YAML))
		Expect(after.Spec.Template.Annotations[BootstrapHashAnnotation]).To(Equal(hashOf(bootstrap.Data)))
		Expect(after.Spec.Template.Annotations[BootstrapHashAnnotation]).NotTo(Equal(before.Spec.Template.Annotations[BootstrapHashAnnotation]))
		Expect(after.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage().String()).To(Equal("10Gi"))
		Expect(string(bootstrap.Data[configFile])).To(ContainSubstring("store_max_bytes: 8053063680"),
			"the event stream is three quarters of the 10Gi volume the pod keeps, not of the class's new 50Gi")
	})

	It("rolls the pods when a value they read at start changes, and only then", func() {
		class := newClass()
		reconciled(newCompany(class.Name))
		hash := func() string { return statefulSet().Spec.Template.Annotations[BootstrapHashAnnotation] }
		before := hash()
		company := &crewletaiv1alpha1.Company{}
		get(company, companyName)

		By("labelling the keyring, which changes nothing the pods read")
		keyring := &corev1.Secret{}
		get(keyring, keyringName)
		keyring.Labels["backup.example.com/schedule"] = "daily"
		Expect(k8sClient.Update(ctx, keyring)).To(Succeed())
		reconciled(company)
		Expect(hash()).To(Equal(before), "a label rolled the pods")

		By("replacing the access token")
		replaced := "revoked-and-replaced-by-hand-" + strings.Repeat("r", 20)
		tokens := &corev1.Secret{}
		get(tokens, tokensName)
		tokens.Data[render.TokenAccess] = []byte(replaced)
		Expect(k8sClient.Update(ctx, tokens)).To(Succeed())
		reconciled(company)

		Expect(hash()).NotTo(Equal(before))
		access := &corev1.Secret{}
		get(access, accessName)
		Expect(string(access.Data[crewletaiv1alpha1.APIAccessTokenKey])).To(Equal(replaced))
	})

	It("names the operator the document's only writer under Enforce, and rolls the pods when that changes", func() {
		class := newClass()
		company := reconciled(newCompany(class.Name))
		Expect(company.Spec.Config.ManagementPolicy).To(Equal(crewletaiv1alpha1.ManagementPolicyEnforce), "the API's default")
		hash := func() string { return statefulSet().Spec.Template.Annotations[BootstrapHashAnnotation] }
		// writers is api.auth.company_writers in the crewlet.yaml the
		// pods start from, and the hash the pod template carries is of
		// that Secret.
		writers := func() []string {
			GinkgoHelper()
			bootstrap := &corev1.Secret{}
			get(bootstrap, bootstrapName)
			Expect(bootstrap.Data[configFile]).To(Equal(expectedBootstrap(company, class).YAML))
			Expect(hash()).To(Equal(hashOf(bootstrap.Data)))
			var tierA struct {
				API struct {
					Auth struct {
						CompanyWriters []string `json:"company_writers"`
					} `json:"auth"`
				} `json:"api"`
			}
			Expect(yaml.Unmarshal(bootstrap.Data[configFile], &tierA)).To(Succeed())
			return tierA.API.Auth.CompanyWriters
		}
		Expect(writers()).To(Equal([]string{render.TokenOperator}))
		enforced := hash()

		By("observing, which lets the engine's own users write and restarts the engine to say so")
		updateCompany(company, func(c *crewletaiv1alpha1.Company) {
			c.Spec.Config.ManagementPolicy = crewletaiv1alpha1.ManagementPolicyObserve
		})
		company = reconciled(company)
		Expect(writers()).To(BeEmpty())
		observed := hash()
		Expect(observed).NotTo(Equal(enforced))

		By("seeding, whose crewlet.yaml is Observe's, so the pods are left running")
		updateCompany(company, func(c *crewletaiv1alpha1.Company) {
			c.Spec.Config.ManagementPolicy = crewletaiv1alpha1.ManagementPolicySeed
		})
		company = reconciled(company)
		Expect(writers()).To(BeEmpty())
		Expect(hash()).To(Equal(observed))

		By("enforcing again")
		updateCompany(company, func(c *crewletaiv1alpha1.Company) {
			c.Spec.Config.ManagementPolicy = crewletaiv1alpha1.ManagementPolicyEnforce
		})
		company = reconciled(company)
		Expect(writers()).To(Equal([]string{render.TokenOperator}))
		Expect(hash()).To(Equal(enforced))
	})

	Context("the engine version", func() {
		overridable := func(c *crewletaiv1alpha1.EngineClass) { c.Spec.Engine.AllowVersionOverride = new(true) }
		version := func(v string) func(*crewletaiv1alpha1.Company) {
			return func(c *crewletaiv1alpha1.Company) { c.Spec.Runtime.Version = v }
		}

		It("refuses a downgrade below what the pods last ran ready, and keeps running that", func() {
			class := newClass(overridable)
			company := reconciled(newCompany(class.Name, version("v0.3.0")))
			Expect(engine(statefulSet()).Image).To(Equal("ghcr.io/crewlet/crewlet:v0.3.0"))
			rollOut()
			company = reconciled(company)
			Expect(company.Status.Engine.Version).To(Equal("v0.3.0"))

			updateCompany(company, version("v0.2.9"))
			company = reconciled(company)
			runtimeReady := condition(company, crewletaiv1alpha1.ConditionRuntimeReady)
			Expect(runtimeReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(runtimeReady.Reason).To(Equal(crewletaiv1alpha1.ReasonDowngradeRefused))
			Expect(runtimeReady.Message).To(ContainSubstring("v0.3.0"))
			Expect(engine(statefulSet()).Image).To(Equal("ghcr.io/crewlet/crewlet:v0.3.0"))

			updateCompany(company, version("v0.4.0"))
			company = reconciled(company)
			Expect(engine(statefulSet()).Image).To(Equal("ghcr.io/crewlet/crewlet:v0.4.0"))
			Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonPodsNotReady))
		})

		It("keeps the floor on the data volume claims when the status is lost", func() {
			class := newClass(overridable)
			company := reconciled(newCompany(class.Name, version("v0.3.0")))
			claim := &corev1.PersistentVolumeClaim{
				Namespace: namespace,
				Name:      dataClaimName,
				Labels:    labelsFor(company, componentData),
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
					},
				},
			}
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())
			rollOut()
			company = reconciled(company)
			Expect(company.Status.Engine.Version).To(Equal("v0.3.0"))
			get(claim, claim.Name)
			Expect(claim.Annotations).To(HaveKeyWithValue(EngineVersionAnnotation, "v0.3.0"))

			By("losing the status, as a restore from a backup does")
			base := company.DeepCopy()
			company.Status.Engine = crewletaiv1alpha1.EngineStatus{}
			Expect(k8sClient.Status().Patch(ctx, company, client.MergeFrom(base))).To(Succeed())
			updateCompany(company, version("v0.2.9"))
			company = reconciled(company)
			runtimeReady := condition(company, crewletaiv1alpha1.ConditionRuntimeReady)
			Expect(runtimeReady.Reason).To(Equal(crewletaiv1alpha1.ReasonDowngradeRefused))
			Expect(runtimeReady.Message).To(ContainSubstring("v0.3.0"))
			Expect(engine(statefulSet()).Image).To(Equal("ghcr.io/crewlet/crewlet:v0.3.0"))
		})

		It("lets a version that never became ready be taken back", func() {
			class := newClass(overridable)
			company := reconciled(newCompany(class.Name, version("v0.3.0")))
			rollOut()
			reconciled(company)
			updateCompany(company, version("v0.9.0"))
			reconciled(company)
			updateCompany(company, version("v0.3.0"))
			company = reconciled(company)
			Expect(engine(statefulSet()).Image).To(Equal("ghcr.io/crewlet/crewlet:v0.3.0"))
			Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).NotTo(Equal(crewletaiv1alpha1.ReasonDowngradeRefused))
		})

		It("refuses a version the class does not let the Company choose", func() {
			class := newClass()
			company := reconciled(newCompany(class.Name, version("v0.3.0")))
			runtimeReady := condition(company, crewletaiv1alpha1.ConditionRuntimeReady)
			Expect(runtimeReady.Reason).To(Equal(crewletaiv1alpha1.ReasonVersionOverrideNotAllowed))
			absent(&appsv1.StatefulSet{}, dataName)
		})
	})

	Context("the platform's Secrets", func() {
		var platform string
		BeforeEach(func() {
			ns := &corev1.Namespace{GenerateName: "platform-"}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			platform = ns.Name
		})
		// ownObjectKeys gives the Company keys of its own for its class's
		// bucket.
		ownObjectKeys := func(c *crewletaiv1alpha1.Company) {
			c.Spec.Objects.CredentialsSecretRef = crewletaiv1alpha1.SecretReference{Name: objectsSecret}
		}
		s3Class := func(c *crewletaiv1alpha1.EngineClass) {
			c.Spec.Objects = crewletaiv1alpha1.ObjectSettings{
				Backend: crewletaiv1alpha1.ObjectBackendS3,
				S3: crewletaiv1alpha1.S3ObjectSettings{
					Region: "eu-central-1", Bucket: "tenants",
				},
			}
			c.Spec.Telemetry = crewletaiv1alpha1.TelemetrySettings{
				OTLPEndpoint:     "https://otlp.example.com",
				HeadersSecretRef: crewletaiv1alpha1.NamespacedSecretKeyReference{Namespace: platform, Name: otlpSecret, Key: "headers"},
			}
			c.Spec.PodTemplate.Spec.Env = []crewletaiv1alpha1.ClassEnvVar{
				{Name: "PLATFORM_URL", Value: new("https://platform.example.com")},
				{Name: platformTokenVar, ValueFrom: crewletaiv1alpha1.NamespacedSecretValueSource{
					SecretKeyRef: crewletaiv1alpha1.NamespacedSecretKeyReference{Namespace: platform, Name: otlpSecret, Key: "token"},
				}},
			}
		}

		It("holds the pods back until the Secrets they read exist, then gives the pods their values", func() {
			class := newClass(s3Class)
			company := reconciled(newCompany(class.Name, ownObjectKeys))
			runtimeReady := condition(company, crewletaiv1alpha1.ConditionRuntimeReady)
			Expect(runtimeReady.Reason).To(Equal(crewletaiv1alpha1.ReasonSecretNotFound))
			Expect(runtimeReady.Message).To(ContainSubstring(platform + "/" + otlpSecret))
			Expect(runtimeReady.Message).To(ContainSubstring(namespace + "/" + objectsSecret))
			absent(&appsv1.StatefulSet{}, dataName)
			absent(&corev1.Secret{}, bootstrapName)

			Expect(k8sClient.Create(ctx, &corev1.Secret{Namespace: namespace, Name: objectsSecret, Data: map[string][]byte{
				crewletaiv1alpha1.S3AccessKeyIDKey: []byte("AKIA"), crewletaiv1alpha1.S3SecretAccessKeyKey: []byte("s3cr3t"),
			}})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Secret{Namespace: platform, Name: otlpSecret, Data: map[string][]byte{
				"headers": []byte("authorization=Bearer x"), "token": []byte("platform-token"),
			}})).To(Succeed())
			company = reconciled(company)

			By("writing the object store's keys into crewlet.yaml, and the environment's values beside it")
			bootstrap := &corev1.Secret{}
			get(bootstrap, bootstrapName)
			ownedByCompany(bootstrap, company)
			Expect(bootstrap.Data).To(HaveLen(3))
			Expect(bootstrap.Data).To(HaveKeyWithValue("otlpHeaders", []byte("authorization=Bearer x")))
			Expect(bootstrap.Data).To(HaveKeyWithValue("env."+platformTokenVar, []byte("platform-token")))
			Expect(string(bootstrap.Data[configFile])).To(ContainSubstring("access_key_id: AKIA"))
			Expect(string(bootstrap.Data[configFile])).To(ContainSubstring("secret_access_key: s3cr3t"))

			fromBootstrap := func(key string) *corev1.EnvVarSource {
				return &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{Name: bootstrapName, Key: key}}
			}
			sts := statefulSet()
			Expect(engine(sts).Env).To(ContainElements(
				corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "https://otlp.example.com"},
				corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_HEADERS", ValueFrom: fromBootstrap("otlpHeaders")},
				corev1.EnvVar{Name: "PLATFORM_URL", Value: "https://platform.example.com"},
				corev1.EnvVar{Name: platformTokenVar, ValueFrom: fromBootstrap("env." + platformTokenVar)},
			))
			for _, e := range engine(sts).Env {
				Expect(e.Name).NotTo(HavePrefix("CREWLET_S3_"), "the object store's keys are in the engine's environment")
			}
			Expect(sts.Spec.Template.Annotations[BootstrapHashAnnotation]).To(Equal(hashOf(bootstrap.Data)))
			Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonPodsNotReady))

			By("rolling the pods when a platform value changes")
			before := sts.Spec.Template.Annotations[BootstrapHashAnnotation]
			headers := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: platform, Name: otlpSecret}, headers)).To(Succeed())
			headers.Data["headers"] = []byte("authorization=Bearer y")
			Expect(k8sClient.Update(ctx, headers)).To(Succeed())
			reconciled(company)
			Expect(statefulSet().Spec.Template.Annotations[BootstrapHashAnnotation]).NotTo(Equal(before))
			Expect(envNamed(engine(statefulSet()).Env, "CREWLET_SANDBOX_OTEL_RECEIVER_URL")).To(BeEmpty(),
				"a sandbox is given a receiver while the company has no address it could reach")

			By("wiring the sandbox telemetry receiver at the company's public address once it has one")
			updateClass(class, func(c *crewletaiv1alpha1.EngineClass) {
				c.Spec.Exposure.Webhooks = crewletaiv1alpha1.WebhookExposure{Mode: crewletaiv1alpha1.WebhookExposureExternal}
			})
			updateCompany(company, func(c *crewletaiv1alpha1.Company) { c.Spec.Exposure.PublicBaseURL = relayURL })
			reconciled(company)
			Expect(envNamed(engine(statefulSet()).Env, "CREWLET_SANDBOX_OTEL_RECEIVER_URL")).To(Equal([]string{relayURL}))

			By("letting the Company's own variable replace the class's")
			updateCompany(company, func(c *crewletaiv1alpha1.Company) {
				c.Spec.Runtime.Env = []crewletaiv1alpha1.EnvVar{{Name: platformTokenVar, Value: new("mine")}}
			})
			reconciled(company)
			get(bootstrap, bootstrapName)
			Expect(bootstrap.Data).NotTo(HaveKey("env." + platformTokenVar))
			env := engine(statefulSet()).Env
			Expect(env).To(ContainElement(corev1.EnvVar{Name: platformTokenVar, Value: "mine"}))
			Expect(slices.ContainsFunc(env, func(e corev1.EnvVar) bool { return e.Name == platformTokenVar && e.ValueFrom != nil })).To(BeFalse())

			By("dropping the values when the class stops giving them")
			updateClass(class, func(c *crewletaiv1alpha1.EngineClass) {
				c.Spec.Objects = crewletaiv1alpha1.ObjectSettings{Backend: crewletaiv1alpha1.ObjectBackendNATS}
				c.Spec.Telemetry = crewletaiv1alpha1.TelemetrySettings{}
				c.Spec.PodTemplate.Spec.Env = nil
			})
			reconciled(company)
			get(bootstrap, bootstrapName)
			Expect(bootstrap.Data).To(HaveLen(1))
			Expect(envNamed(engine(statefulSet()).Env, "CREWLET_SANDBOX_OTEL_RECEIVER_URL")).To(BeEmpty(),
				"a receiver with no collector to forward to")
			Expect(string(bootstrap.Data[configFile])).NotTo(ContainSubstring("AKIA"))
		})

		It("maps a Secret to the Companies whose class or own object keys read it, or whose keyring or tokens it is", func() {
			class := newClass(s3Class)
			newCompany(class.Name, ownObjectKeys)
			companiesOf := func(secretNamespace, name string) []reconcile.Request {
				var got []reconcile.Request
				for _, req := range newReconciler().companiesOfSecret(ctx, &corev1.Secret{Namespace: secretNamespace, Name: name}) {
					if req.Namespace == namespace {
						got = append(got, req)
					}
				}
				return got
			}
			nimbus := reconcile.Request{Namespace: namespace, Name: companyName}
			Expect(companiesOf(platform, otlpSecret)).To(ConsistOf(nimbus))
			Expect(companiesOf(namespace, otlpSecret)).To(BeEmpty())
			Expect(companiesOf(namespace, tokensName)).To(ConsistOf(nimbus))
			Expect(companiesOf(namespace, keyringName)).To(ConsistOf(nimbus))
			Expect(companiesOf(namespace, objectsSecret)).To(ConsistOf(nimbus))
		})
	})

	Context("exposure", func() {
		gatewayWebhooks := func(c *crewletaiv1alpha1.EngineClass) {
			c.Spec.Exposure.Webhooks = crewletaiv1alpha1.WebhookExposure{
				Mode: crewletaiv1alpha1.WebhookExposureGateway,
				Gateway: crewletaiv1alpha1.GatewayExposure{ParentRefs: []crewletaiv1alpha1.GatewayParentReference{
					{Namespace: gatewayNamespace, Name: publicGateway},
				}},
				Hostname: webhooksHostname,
			}
		}
		// accept stands in for the Gateway's controller.
		accept := func(name, gatewayNamespace, gateway string) {
			GinkgoHelper()
			route := &gatewayv1.HTTPRoute{}
			get(route, name)
			route.Status.Parents = []gatewayv1.RouteParentStatus{{
				ParentRef:      gatewayv1.ParentReference{Namespace: new(gatewayv1.Namespace(gatewayNamespace)), Name: gatewayv1.ObjectName(gateway)},
				ControllerName: "example.com/gateway-controller",
				Conditions: []metav1.Condition{{
					Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue, Reason: "Accepted",
					Message: "Accepted.", LastTransitionTime: metav1.Now(), ObservedGeneration: route.Generation,
				}},
			}}
			Expect(k8sClient.Status().Update(ctx, route)).To(Succeed())
		}

		It("routes the class's Gateways to the engine's public listener alone", func() {
			class := newClass(gatewayWebhooks)
			company := reconciled(newCompany(class.Name))
			host := string(company.UID) + ".hooks.example.com"

			route := &gatewayv1.HTTPRoute{}
			get(route, webhooksRoute)
			ownedByCompany(route, company)
			Expect(route.Spec.Hostnames).To(Equal([]gatewayv1.Hostname{gatewayv1.Hostname(host)}))
			Expect(route.Spec.ParentRefs).To(HaveLen(1))
			Expect(string(*route.Spec.ParentRefs[0].Namespace)).To(Equal(gatewayNamespace))
			Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal(publicGateway))
			Expect(route.Spec.Rules).To(HaveLen(1))
			Expect(route.Spec.Rules[0].Matches).To(HaveLen(1))
			Expect(*route.Spec.Rules[0].Matches[0].Path.Type).To(Equal(gatewayv1.PathMatchPathPrefix))
			Expect(*route.Spec.Rules[0].Matches[0].Path.Value).To(Equal("/"),
				"the public listener draws the line between public routes and the rest")
			Expect(route.Spec.Rules[0].BackendRefs).To(HaveLen(1))
			Expect(string(route.Spec.Rules[0].BackendRefs[0].Name)).To(Equal(engineName))
			Expect(*route.Spec.Rules[0].BackendRefs[0].Port).To(Equal(gatewayv1.PortNumber(8081)),
				"the webhooks' route reaches the admin API's port")

			Expect(company.Status.Endpoints.PublicBaseURL).To(Equal("https://" + host))
			exposed := condition(company, crewletaiv1alpha1.ConditionExposed)
			Expect(exposed.Status).To(Equal(metav1.ConditionFalse))
			Expect(exposed.Reason).To(Equal(crewletaiv1alpha1.ReasonRouteNotAccepted))

			By("admitting the Gateway's namespace to the public port, and not to the API's")
			policy := &networkingv1.NetworkPolicy{}
			get(policy, engineName)
			Expect(ingressRules(policy)).To(ContainElement(fromNamespaces(8081, gatewayNamespace)))
			Expect(ingressRules(policy)).NotTo(ContainElement(fromNamespaces(8080, gatewayNamespace)))
			Expect(ingressRules(policy)).To(HaveLen(2), "the operator to the API port, the Gateway to the public port")

			accept(webhooksRoute, gatewayNamespace, publicGateway)
			company = reconciled(company)
			exposed = condition(company, crewletaiv1alpha1.ConditionExposed)
			Expect(exposed.Status).To(Equal(metav1.ConditionTrue))
			Expect(exposed.Reason).To(Equal(crewletaiv1alpha1.ReasonRouteAccepted))
			Expect(company.Status.Endpoints.ExposedAPI).To(BeEmpty())
		})

		It("tells the engine the tool bridge's address while a provider runs a CLI in mode Agent", func() {
			class := newClass(gatewayWebhooks)
			company := reconciled(newCompany(class.Name))
			host := string(company.UID) + ".hooks.example.com"
			paths := func() []string {
				GinkgoHelper()
				route := &gatewayv1.HTTPRoute{}
				get(route, webhooksRoute)
				Expect(route.Spec.Rules).To(HaveLen(1))
				out := make([]string, 0, len(route.Spec.Rules[0].Matches))
				for _, m := range route.Spec.Rules[0].Matches {
					Expect(*m.Path.Type).To(Equal(gatewayv1.PathMatchPathPrefix))
					out = append(out, *m.Path.Value)
				}
				return out
			}
			bridgeURL := func() []string {
				GinkgoHelper()
				return envNamed(engine(statefulSet()).Env, "CREWLET_MCP_BRIDGE_URL")
			}
			// The route is the same either way: the engine mounts the
			// bridge on its public listener only when given its address.
			Expect(paths()).To(Equal([]string{"/"}))
			Expect(bridgeURL()).To(BeEmpty())

			By("a provider of another company, and one of this company in mode Text, serving no bridge")
			provider := func(name, company string, mode crewletaiv1alpha1.CLIAgentMode) *crewletaiv1alpha1.LLMProvider {
				p := &crewletaiv1alpha1.LLMProvider{
					Namespace: namespace,
					Name:      name,
					Spec: crewletaiv1alpha1.LLMProviderSpec{
						CompanyRef: crewletaiv1alpha1.CompanyReference{Name: company},
						LLMProviderSettings: crewletaiv1alpha1.LLMProviderSettings{
							Type: crewletaiv1alpha1.LLMProviderCLIAgent, Model: "sonnet",
						},
						CLI: crewletaiv1alpha1.CLIAgentSettings{
							Agent: crewletaiv1alpha1.CLIAgentClaudeCode,
							Mode:  mode,
							Auth: crewletaiv1alpha1.CLIAgentAuth{
								TokenSecretRef: crewletaiv1alpha1.SecretKeyReference{Name: "claude", Key: "oauthToken"},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, p)).To(Succeed())
				DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, p))).To(Succeed()) })
				return p
			}
			provider("other-agent", "another-company", crewletaiv1alpha1.CLIAgentModeAgent)
			text := provider("claude-text", companyName, crewletaiv1alpha1.CLIAgentModeText)
			company = reconciled(company)
			Expect(paths()).To(Equal([]string{"/"}))
			Expect(bridgeURL()).To(BeEmpty())

			By("switching this company's provider to mode Agent")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(text), text)).To(Succeed())
			text.Spec.CLI.Mode = crewletaiv1alpha1.CLIAgentModeAgent
			Expect(k8sClient.Update(ctx, text)).To(Succeed())
			company = reconciled(company)
			Expect(paths()).To(Equal([]string{"/"}))
			Expect(bridgeURL()).To(Equal([]string{"https://" + host}))

			By("deleting it")
			Expect(k8sClient.Delete(ctx, text)).To(Succeed())
			reconciled(company)
			Expect(paths()).To(Equal([]string{"/"}))
			Expect(bridgeURL()).To(BeEmpty())
		})

		It("moves the route to an Ingress when the class does", func() {
			class := newClass(gatewayWebhooks)
			company := reconciled(newCompany(class.Name, func(c *crewletaiv1alpha1.Company) {
				c.Spec.Exposure.PublicBaseURL = "https://hooks.nimbus.example.com"
			}))
			Expect(company.Status.Endpoints.PublicBaseURL).To(Equal("https://hooks.nimbus.example.com"))

			updateClass(class, func(c *crewletaiv1alpha1.EngineClass) {
				ingress := &crewletaiv1alpha1.IngressExposure{
					ClassName:            "nginx",
					Annotations:          map[string]string{"cert-manager.io/cluster-issuer": "public"},
					ControllerNamespaces: []string{"ingress-nginx"},
				}
				ingress.TLS.SecretName = "{{name}}-webhooks-tls"
				c.Spec.Exposure.Webhooks = crewletaiv1alpha1.WebhookExposure{
					Mode: crewletaiv1alpha1.WebhookExposureIngress, Ingress: ingress, Hostname: "{{name}}.{{namespace}}.hooks.example.com",
				}
			})
			company = reconciled(company)
			absent(&gatewayv1.HTTPRoute{}, webhooksRoute)

			host := "nimbus." + namespace + ".hooks.example.com"
			ingress := &networkingv1.Ingress{}
			get(ingress, webhooksRoute)
			ownedByCompany(ingress, company)
			Expect(ingress.Annotations).To(HaveKeyWithValue("cert-manager.io/cluster-issuer", "public"))
			Expect(*ingress.Spec.IngressClassName).To(Equal("nginx"))
			Expect(ingress.Spec.TLS).To(Equal([]networkingv1.IngressTLS{{Hosts: []string{host}, SecretName: "nimbus-webhooks-tls"}}))
			prefix := networkingv1.PathTypePrefix
			Expect(ingress.Spec.Rules).To(Equal([]networkingv1.IngressRule{{
				Host: host,
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{
						Path: "/", PathType: &prefix,
						Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
							Name: engineName, Port: networkingv1.ServiceBackendPort{Number: 8081},
						}},
					}},
				},
			}}))
			// The Company's own address still wins over the class's host name.
			Expect(company.Status.Endpoints.PublicBaseURL).To(Equal("https://hooks.nimbus.example.com"))
			Expect(condition(company, crewletaiv1alpha1.ConditionExposed).Reason).To(Equal(crewletaiv1alpha1.ReasonRouteNotAccepted))

			policy := &networkingv1.NetworkPolicy{}
			get(policy, engineName)
			Expect(ingressRules(policy)).To(ContainElement(fromNamespaces(8081, "ingress-nginx")))
			Expect(ingressRules(policy)).NotTo(ContainElement(fromNamespaces(8081, gatewayNamespace)), "the Gateway's namespace outlived its route")
			Expect(ingressRules(policy)).NotTo(ContainElement(fromNamespaces(8080, "ingress-nginx")))

			// The Ingress controller publishes the address it serves on.
			ingress.Status.LoadBalancer.Ingress = []networkingv1.IngressLoadBalancerIngress{{IP: "203.0.113.10"}}
			Expect(k8sClient.Status().Update(ctx, ingress)).To(Succeed())
			company = reconciled(company)
			Expect(condition(company, crewletaiv1alpha1.ConditionExposed).Reason).To(Equal(crewletaiv1alpha1.ReasonRouteAccepted))
		})

		It("needs the Company's address when webhooks are forwarded from outside", func() {
			class := newClass(func(c *crewletaiv1alpha1.EngineClass) {
				c.Spec.Exposure.Webhooks = crewletaiv1alpha1.WebhookExposure{Mode: crewletaiv1alpha1.WebhookExposureExternal}
			})
			company := reconciled(newCompany(class.Name))
			exposed := condition(company, crewletaiv1alpha1.ConditionExposed)
			Expect(exposed.Status).To(Equal(metav1.ConditionFalse))
			Expect(exposed.Reason).To(Equal(crewletaiv1alpha1.ReasonPublicBaseURLMissing))
			Expect(condition(company, crewletaiv1alpha1.ConditionReady).Reason).NotTo(Equal(crewletaiv1alpha1.ReasonAvailable))

			updateCompany(company, func(c *crewletaiv1alpha1.Company) { c.Spec.Exposure.PublicBaseURL = relayURL })
			company = reconciled(company)
			exposed = condition(company, crewletaiv1alpha1.ConditionExposed)
			Expect(exposed.Status).To(Equal(metav1.ConditionTrue))
			Expect(exposed.Reason).To(Equal(crewletaiv1alpha1.ReasonNoRoutes))
			Expect(company.Status.Endpoints.PublicBaseURL).To(Equal(relayURL))
			absent(&gatewayv1.HTTPRoute{}, webhooksRoute)
			absent(&networkingv1.Ingress{}, webhooksRoute)

			By("dropping a trailing '/', as the engine does from the base of its own addresses")
			updateCompany(company, func(c *crewletaiv1alpha1.Company) {
				c.Spec.Exposure.PublicBaseURL = "https://relay.example.com/nimbus//"
			})
			company = reconciled(company)
			Expect(company.Status.Endpoints.PublicBaseURL).To(Equal(relayURL))

			By("admitting nothing to the public port until the class names its forwarders")
			policy := &networkingv1.NetworkPolicy{}
			get(policy, engineName)
			for _, rule := range ingressRules(policy) {
				Expect(rule).To(HavePrefix("TCP/8080 "))
			}
			tunnel := metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: "tunnel"}}
			updateClass(class, func(c *crewletaiv1alpha1.EngineClass) {
				c.Spec.Exposure.Webhooks.External.Forwarders = []crewletaiv1alpha1.NetworkPolicyPeer{{
					NamespaceSelector: &tunnel,
					PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app": "cloudflared"}},
				}}
			})
			reconciled(company)
			get(policy, engineName)
			Expect(ingressRules(policy)).To(ContainElement(
				"TCP/8081 from pods {app=cloudflared} in namespaces {kubernetes.io/metadata.name=tunnel}"))
			Expect(ingressRules(policy)).NotTo(ContainElement(
				"TCP/8080 from pods {app=cloudflared} in namespaces {kubernetes.io/metadata.name=tunnel}"), "a forwarder reaches the admin API")
		})

		It("routes the whole API through an internal Gateway and reports its HTTPS address", func() {
			Expect(k8sClient.Create(ctx, &gatewayv1.Gateway{
				Namespace: namespace,
				Name:      internalGateway,
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: internalGateway,
					Listeners: []gatewayv1.Listener{{
						Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
						TLS: &gatewayv1.ListenerTLSConfig{CertificateRefs: []gatewayv1.SecretObjectReference{{Name: "internal-tls"}}},
					}},
				},
			})).To(Succeed())
			class := newClass(func(c *crewletaiv1alpha1.EngineClass) {
				c.Spec.Exposure.API = crewletaiv1alpha1.APIExposure{
					Mode:     crewletaiv1alpha1.APIExposureGateway,
					Gateway:  crewletaiv1alpha1.GatewayExposure{ParentRefs: []crewletaiv1alpha1.GatewayParentReference{{Name: internalGateway}}},
					Hostname: "{{uid}}.api.internal.example.com",
				}
			})
			company := reconciled(newCompany(class.Name))
			route := &gatewayv1.HTTPRoute{}
			get(route, "nimbus-api")
			Expect(*route.Spec.Rules[0].Matches[0].Path.Value).To(Equal("/"))
			Expect(*route.Spec.Rules[0].BackendRefs[0].Port).To(Equal(gatewayv1.PortNumber(8080)))
			apiURL := "https://" + string(company.UID) + ".api.internal.example.com"
			Expect(company.Status.Endpoints.ExposedAPI).To(BeEmpty(), "set before the route was accepted")
			Expect(company.Status.Endpoints.DashboardBaseURL).To(Equal(apiURL),
				"the dashboard's address is the class's and the Gateway's, and does not wait for the route to be accepted")

			By("admitting the internal Gateway's namespace to the API port, and not to the public port")
			policy := &networkingv1.NetworkPolicy{}
			get(policy, engineName)
			Expect(ingressRules(policy)).To(ContainElement(fromNamespaces(8080, namespace)))
			Expect(ingressRules(policy)).NotTo(ContainElement(fromNamespaces(8081, namespace)))

			accept("nimbus-api", namespace, internalGateway)
			company = reconciled(company)
			Expect(company.Status.Endpoints.ExposedAPI).To(Equal(apiURL))
			Expect(company.Status.Endpoints.DashboardBaseURL).To(Equal(apiURL), "people reach the dashboard where the API is routed")
			exposed := condition(company, crewletaiv1alpha1.ConditionExposed)
			Expect(exposed.Reason).To(Equal(crewletaiv1alpha1.ReasonRouteAccepted))
			Expect(exposed.Message).NotTo(ContainSubstring("no link"))

			By("keeping the dashboard's address while the Gateway withdraws its acceptance")
			route = &gatewayv1.HTTPRoute{}
			get(route, "nimbus-api")
			route.Status.Parents[0].Conditions[0].Status = metav1.ConditionFalse
			route.Status.Parents[0].Conditions[0].Reason = "Pending"
			Expect(k8sClient.Status().Update(ctx, route)).To(Succeed())
			company = reconciled(company)
			Expect(company.Status.Endpoints.ExposedAPI).To(BeEmpty(), "a route no longer accepted still reads as exposed")
			Expect(company.Status.Endpoints.DashboardBaseURL).To(Equal(apiURL),
				"a flip of the route's acceptance changed the address rendered into the company document")

			By("taking the Company's own dashboard address over the route's, without its trailing '/'")
			updateCompany(company, func(c *crewletaiv1alpha1.Company) {
				c.Spec.Exposure.DashboardBaseURL = "https://crewlet.sso.example.com/nimbus/"
			})
			company = reconciled(company)
			Expect(company.Status.Endpoints.DashboardBaseURL).To(Equal("https://crewlet.sso.example.com/nimbus"))
		})

		It("takes the dashboard's address from an API Ingress whose controller publishes none", func() {
			class := newClass(func(c *crewletaiv1alpha1.EngineClass) {
				c.Spec.Exposure.API = crewletaiv1alpha1.APIExposure{
					Mode:     crewletaiv1alpha1.APIExposureIngress,
					Ingress:  &crewletaiv1alpha1.IngressExposure{ClassName: "internal", ControllerNamespaces: []string{"ingress-internal"}},
					Hostname: "{{name}}.{{namespace}}.api.internal.example.com",
				}
			})
			company := reconciled(newCompany(class.Name))
			get(&networkingv1.Ingress{}, "nimbus-api")
			Expect(company.Status.Endpoints.ExposedAPI).To(BeEmpty(), "an Ingress with no published address reads as exposed")
			Expect(company.Status.Endpoints.DashboardBaseURL).To(Equal("http://nimbus."+namespace+".api.internal.example.com"),
				"an Ingress that terminates no TLS serves the dashboard over http, whether or not its controller publishes an address")
		})

		It("reports a Gateway route as not exposed on a cluster without the Gateway API", func() {
			class := newClass(gatewayWebhooks)
			company := newCompany(class.Name)
			r := newReconciler()
			r.GatewayAPI = false
			reconcileWith(r, company)
			get(company, companyName)
			exposed := condition(company, crewletaiv1alpha1.ConditionExposed)
			Expect(exposed.Status).To(Equal(metav1.ConditionFalse))
			Expect(exposed.Reason).To(Equal(crewletaiv1alpha1.ReasonGatewayAPINotInstalled))
			absent(&gatewayv1.HTTPRoute{}, webhooksRoute)
		})

		It("finds the Gateway API on a cluster that serves it", func() {
			installed, err := GatewayAPIInstalled(k8sClient.RESTMapper())
			Expect(err).NotTo(HaveOccurred())
			Expect(installed).To(BeTrue())
		})
	})

	Context("objects of its names that it does not own", func() {
		// unchanged checks that the operator neither wrote obj nor made it
		// the Company's, so the garbage collector will not take it either.
		unchanged := func(obj client.Object) {
			GinkgoHelper()
			before := obj.GetResourceVersion()
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			Expect(obj.GetResourceVersion()).To(Equal(before), "%T %s was written", obj, obj.GetName())
			Expect(obj.GetOwnerReferences()).To(BeEmpty())
		}

		It("leaves a namespace's own NetworkPolicy of the policy's name alone, and says so", func() {
			class := newClass()
			denyAll := &networkingv1.NetworkPolicy{
				Namespace: namespace, Name: engineName,
				Spec: networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}},
			}
			Expect(k8sClient.Create(ctx, denyAll)).To(Succeed())
			company := reconciled(newCompany(class.Name))
			unchanged(denyAll)
			runtimeReady := condition(company, crewletaiv1alpha1.ConditionRuntimeReady)
			Expect(runtimeReady.Reason).To(Equal(crewletaiv1alpha1.ReasonNameConflict))
			Expect(runtimeReady.Message).To(ContainSubstring("NetworkPolicy " + namespace + "/" + engineName))
			absent(&appsv1.StatefulSet{}, dataName)
		})

		It("leaves a user's Service and Secret of the derived names alone", func() {
			class := newClass()
			service := &corev1.Service{
				Namespace: namespace, Name: engineName,
				Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 443}}},
			}
			Expect(k8sClient.Create(ctx, service)).To(Succeed())
			access := &corev1.Secret{Namespace: namespace, Name: accessName, Data: map[string][]byte{crewletaiv1alpha1.APIAccessTokenKey: []byte("mine")}}
			Expect(k8sClient.Create(ctx, access)).To(Succeed())

			company := reconciled(newCompany(class.Name))
			unchanged(access)
			Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonNameConflict))
			Expect(k8sClient.Delete(ctx, access)).To(Succeed())
			company = reconciled(company)
			unchanged(service)
			runtimeReady := condition(company, crewletaiv1alpha1.ConditionRuntimeReady)
			Expect(runtimeReady.Reason).To(Equal(crewletaiv1alpha1.ReasonNameConflict))
			Expect(runtimeReady.Message).To(ContainSubstring("Service " + namespace + "/" + engineName))
		})

		It("reports a route of another owner as not exposed, and runs the engine", func() {
			class := newClass(func(c *crewletaiv1alpha1.EngineClass) {
				c.Spec.Exposure.Webhooks = crewletaiv1alpha1.WebhookExposure{
					Mode:     crewletaiv1alpha1.WebhookExposureGateway,
					Gateway:  crewletaiv1alpha1.GatewayExposure{ParentRefs: []crewletaiv1alpha1.GatewayParentReference{{Namespace: gatewayNamespace, Name: publicGateway}}},
					Hostname: webhooksHostname,
				}
			})
			route := &gatewayv1.HTTPRoute{Namespace: namespace, Name: webhooksRoute}
			Expect(k8sClient.Create(ctx, route)).To(Succeed())
			company := reconciled(newCompany(class.Name))
			unchanged(route)
			exposed := condition(company, crewletaiv1alpha1.ConditionExposed)
			Expect(exposed.Status).To(Equal(metav1.ConditionFalse))
			Expect(exposed.Reason).To(Equal(crewletaiv1alpha1.ReasonNameConflict))
			Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonPodsNotReady))
		})

		It("takes over what an earlier Company of the same name left", func() {
			class := newClass()
			// Written as the operator wrote it for that Company: with
			// server-side apply, under the operator's field manager.
			left := corev1ac.Service(engineName, namespace).
				WithOwnerReferences(metav1ac.OwnerReference().
					WithAPIVersion(crewletaiv1alpha1.GroupVersion.String()).WithKind("Company").WithName(companyName).
					WithUID("0e4d7c1a-5b8e-4a3e-9d8f-1c2b3a4d5e6f").WithController(true)).
				WithSpec(corev1ac.ServiceSpec().WithPorts(corev1ac.ServicePort().WithName("https").WithPort(443)))
			Expect(k8sClient.Apply(ctx, left, client.FieldOwner(FieldOwner))).To(Succeed())
			company := reconciled(newCompany(class.Name))
			service := &corev1.Service{}
			get(service, engineName)
			ownedByCompany(service, company)
			Expect(service.OwnerReferences).To(HaveLen(1), "the earlier Company would still own it")
			Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonPodsNotReady))
		})
	})

	It("removes the network policy when the class stops isolating the engine", func() {
		class := newClass()
		company := reconciled(newCompany(class.Name))
		get(&networkingv1.NetworkPolicy{}, engineName)
		updateClass(class, func(c *crewletaiv1alpha1.EngineClass) {
			c.Spec.NetworkPolicy = crewletaiv1alpha1.NetworkPolicySettings{Mode: crewletaiv1alpha1.NetworkPolicyNone}
		})
		reconciled(company)
		absent(&networkingv1.NetworkPolicy{}, engineName)
	})

	Context("deletion", func() {
		// A data pod and its volume claim as the StatefulSet controller would
		// have made them; envtest runs neither it nor the garbage collector,
		// so the spec makes them and deletes the pod itself.
		dataPod := func() *corev1.Pod {
			GinkgoHelper()
			pod := &corev1.Pod{
				Namespace: namespace,
				Name:      "nimbus-data-0",
				Labels:    selectorFor(&crewletaiv1alpha1.Company{Name: companyName}, componentData),
				Spec: corev1.PodSpec{
					ServiceAccountName:           engineName,
					AutomountServiceAccountToken: new(false),
					Containers:                   []corev1.Container{{Name: engineContainer, Image: engineImageV020}},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			return pod
		}
		dataClaim := func() *corev1.PersistentVolumeClaim {
			GinkgoHelper()
			claim := &corev1.PersistentVolumeClaim{
				Namespace: namespace,
				Name:      dataClaimName,
				Labels:    selectorFor(&crewletaiv1alpha1.Company{Name: companyName}, componentData),
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
					},
				},
			}
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())
			return claim
		}
		// deleteAndDrain deletes the Company and checks that it is held
		// until its data StatefulSet and data pod are gone.
		deleteAndDrain := func(company *crewletaiv1alpha1.Company) {
			GinkgoHelper()
			retain := company.Spec.DeletionPolicy != crewletaiv1alpha1.DeletionPolicyDelete
			pod := dataPod()
			Expect(k8sClient.Delete(ctx, company)).To(Succeed())
			result := reconcileWith(newReconciler(), company)
			Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)), "released before the data pod stopped")
			get(company, companyName)
			Expect(company.Finalizers).To(ContainElement(crewletaiv1alpha1.CompanyFinalizer))

			if retain {
				By("orphaning the StatefulSet's claims, standing in for the garbage collector")
				// The Orphan policy has the garbage collector take the
				// StatefulSet's owner references off its pods and claims
				// before it deletes it, so that no claim the StatefulSet
				// controller marked under Delete goes with it.
				sts := statefulSet()
				Expect(sts.DeletionTimestamp).NotTo(BeNil())
				Expect(sts.Finalizers).To(ContainElement(metav1.FinalizerOrphanDependents))
				sts.Finalizers = nil
				Expect(k8sClient.Update(ctx, sts)).To(Succeed())
			}
			absent(&appsv1.StatefulSet{}, dataName)
			result = reconcileWith(newReconciler(), company)
			Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)), "released before the data pod stopped")
			if !retain {
				// The garbage collector deletes the pods with the StatefulSet.
				Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			}
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))
			}).Should(BeTrue(), "the orphaned data pod was not deleted")
			Expect(reconcileWith(newReconciler(), company)).To(Equal(ctrl.Result{}))
			absent(&crewletaiv1alpha1.Company{}, companyName)
		}

		It("keeps the volumes and the keyring with deletionPolicy Retain", func() {
			class := newClass()
			company := reconciled(newCompany(class.Name))
			claim := dataClaim()
			deleteAndDrain(company)

			kept := &corev1.PersistentVolumeClaim{}
			get(kept, claim.Name)
			Expect(kept.DeletionTimestamp).To(BeNil())
			keyring := &corev1.Secret{}
			get(keyring, keyringName)
			Expect(keyring.OwnerReferences).To(BeEmpty(), "the garbage collector would delete the keyring with the Company")
		})

		It("deletes the volumes with deletionPolicy Delete", func() {
			class := newClass()
			company := reconciled(newCompany(class.Name, func(c *crewletaiv1alpha1.Company) {
				c.Spec.DeletionPolicy = crewletaiv1alpha1.DeletionPolicyDelete
			}))
			Expect(statefulSet().Spec.PersistentVolumeClaimRetentionPolicy.WhenDeleted).
				To(Equal(appsv1.DeletePersistentVolumeClaimRetentionPolicyType))
			claim := dataClaim()
			deleteAndDrain(company)

			// The claim's protection finalizer holds it while no controller
			// runs to release it; the deletion has been asked for either way.
			gone := &corev1.PersistentVolumeClaim{}
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(claim), gone)
			if !apierrors.IsNotFound(err) {
				Expect(err).NotTo(HaveOccurred())
				Expect(gone.DeletionTimestamp).NotTo(BeNil())
			}
			keyring := &corev1.Secret{}
			get(keyring, keyringName)
			ownedByCompany(keyring, company)
		})
	})
})

var _ = Describe("Company runtime event filters", func() {
	It("re-runs a Company when its ConfigApplied condition changes, and not for other status writes", func() {
		withConfigApplied := func(status metav1.ConditionStatus) *crewletaiv1alpha1.Company {
			c := &crewletaiv1alpha1.Company{}
			if status != "" {
				c.Status.Conditions = []metav1.Condition{{Type: crewletaiv1alpha1.ConditionConfigApplied, Status: status, Reason: "R"}}
			}
			return c
		}
		changed := configReported()
		update := func(old, updated *crewletaiv1alpha1.Company) bool {
			return changed.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated})
		}
		Expect(update(withConfigApplied(""), withConfigApplied(metav1.ConditionTrue))).To(BeTrue())
		Expect(update(withConfigApplied(metav1.ConditionFalse), withConfigApplied(metav1.ConditionTrue))).To(BeTrue())
		Expect(update(withConfigApplied(metav1.ConditionTrue), withConfigApplied(metav1.ConditionTrue))).To(BeFalse())
		Expect(update(withConfigApplied(""), withConfigApplied(""))).To(BeFalse())
	})
})

var _ = Describe("Company status writes", func() {
	var namespace string
	BeforeEach(func() {
		ns := &corev1.Namespace{GenerateName: "status-"}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespace = ns.Name
	})

	It("stamps the config reconciler's conditions with the generation it rendered, after a spec change", func() {
		company := &crewletaiv1alpha1.Company{
			Namespace: namespace, Name: companyName,
			Spec: crewletaiv1alpha1.CompanySpec{CompanyName: companyDisplayName},
		}
		Expect(k8sClient.Create(ctx, company)).To(Succeed())
		read := company.DeepCopy()

		// The spec changes while the reconcile that read generation 1 runs.
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(company), company)).To(Succeed())
		company.Spec.Mission = "Answer every customer within a day."
		Expect(k8sClient.Update(ctx, company)).To(Succeed())
		Expect(company.Generation).To(Equal(read.Generation + 1))

		run := &configRun{
			CompanyConfigReconciler: &CompanyConfigReconciler{Client: k8sClient, APIReader: k8sClient},
			company:                 read,
			configApplied: &metav1.Condition{
				Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionTrue,
				Reason: crewletaiv1alpha1.ReasonApplied, Message: "Applied revision r1.",
			},
		}
		Expect(run.patchCompanyStatus(ctx)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(company), company)).To(Succeed())
		applied := meta.FindStatusCondition(company.Status.Conditions, crewletaiv1alpha1.ConditionConfigApplied)
		Expect(applied).NotTo(BeNil())
		Expect(applied.ObservedGeneration).To(Equal(read.Generation), "claims a generation that was never rendered")
	})

	It("reconciles a Company again, without an error, when its status write conflicts", func() {
		class := &crewletaiv1alpha1.EngineClass{
			Name: namespace,
			Spec: crewletaiv1alpha1.EngineClassSpec{
				Engine: crewletaiv1alpha1.EngineSettings{Image: engineImageV020},
				Data: crewletaiv1alpha1.DataPodSettings{
					Storage: crewletaiv1alpha1.DataStorage{Size: new(resource.MustParse("10Gi"))},
				},
			},
		}
		Expect(k8sClient.Create(ctx, class)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, class)).To(Succeed()) })
		company := &crewletaiv1alpha1.Company{
			Namespace: namespace, Name: companyName,
			Spec: crewletaiv1alpha1.CompanySpec{CompanyName: companyDisplayName, EngineClassName: class.Name},
		}
		Expect(k8sClient.Create(ctx, company)).To(Succeed())

		// The other reconciler's write lands between this one's read and
		// its write.
		conflicting, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		r := newReconciler()
		r.Client = interceptor.NewClient(conflicting, interceptor.Funcs{
			SubResourcePatch: func(_ context.Context, _ client.Client, _ string, obj client.Object, _ client.Patch, _ ...client.SubResourcePatchOption) error {
				return apierrors.NewConflict(crewletaiv1alpha1.GroupVersion.WithResource("companies").GroupResource(), obj.GetName(), errors.New("the object has been modified"))
			},
		})
		result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(company)})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))
	})
})
