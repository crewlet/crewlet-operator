package controller

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

var _ = Describe("Company controller", Ordered, func() {
	var namespace string

	reconcileCompany := func(name string) {
		GinkgoHelper()
		r := &CompanyReconciler{Client: k8sClient}
		_, err := r.Reconcile(ctx, reconcile.Request{Namespace: namespace, Name: name})
		Expect(err).NotTo(HaveOccurred())
	}

	// accepted reconciles the Company and returns its Accepted condition,
	// after checking that the status describes the Company's generation.
	accepted := func(company *crewletaiv1alpha1.Company) *metav1.Condition {
		GinkgoHelper()
		reconcileCompany(company.Name)
		got := &crewletaiv1alpha1.Company{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(company), got)).To(Succeed())
		Expect(got.Status.ObservedGeneration).To(Equal(got.Generation))
		condition := meta.FindStatusCondition(got.Status.Conditions, crewletaiv1alpha1.ConditionAccepted)
		Expect(condition).NotTo(BeNil(), "the Company has no Accepted condition")
		Expect(condition.ObservedGeneration).To(Equal(got.Generation))
		return condition
	}

	newCompany := func(name, class string) *crewletaiv1alpha1.Company {
		GinkgoHelper()
		company := &crewletaiv1alpha1.Company{
			Namespace: namespace,
			Name:      name,
			Spec:      crewletaiv1alpha1.CompanySpec{CompanyName: "Nimbus", EngineClassName: class},
		}
		Expect(k8sClient.Create(ctx, company)).To(Succeed())
		return company
	}

	// newClass creates an EngineClass and deletes it when the spec ends:
	// classes are cluster-scoped, so one spec's default class would otherwise
	// be every later spec's too.
	newClass := func(name string, isDefault bool, limits crewletaiv1alpha1.EngineLimits) {
		GinkgoHelper()
		class := &crewletaiv1alpha1.EngineClass{
			Name: name,
			Spec: crewletaiv1alpha1.EngineClassSpec{
				Engine: crewletaiv1alpha1.EngineSettings{Image: "ghcr.io/crewlet/crewlet:v0.1.0"},
				Data: crewletaiv1alpha1.DataPodSettings{
					Storage: crewletaiv1alpha1.DataStorage{Size: new(resource.MustParse("10Gi"))},
				},
				Limits: limits,
			},
		}
		if isDefault {
			class.Annotations = map[string]string{crewletaiv1alpha1.DefaultEngineClassAnnotation: "true"}
		}
		Expect(k8sClient.Create(ctx, class)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, class)).To(Succeed()) })
	}

	BeforeAll(func() {
		ns := &corev1.Namespace{GenerateName: "company-controller-"}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespace = ns.Name
	})

	It("accepts a Company whose EngineClass exists", func() {
		newClass("standard", false, crewletaiv1alpha1.EngineLimits{})
		condition := accepted(newCompany("named", "standard"))
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal(crewletaiv1alpha1.ReasonValid))
		Expect(condition.Message).To(ContainSubstring("standard"))
	})

	It("refuses a Company whose EngineClass does not exist", func() {
		condition := accepted(newCompany("missing", "absent"))
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(crewletaiv1alpha1.ReasonEngineClassNotFound))
		Expect(condition.Message).To(ContainSubstring("absent"))
	})

	It("accepts it once the EngineClass appears", func() {
		company := &crewletaiv1alpha1.Company{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "missing"}, company)).To(Succeed())
		newClass("absent", false, crewletaiv1alpha1.EngineLimits{})
		Expect(accepted(company).Status).To(Equal(metav1.ConditionTrue))
	})

	Context("a Company that names no EngineClass", func() {
		It("is refused while no class is the default", func() {
			newClass("not-default", false, crewletaiv1alpha1.EngineLimits{})
			condition := accepted(newCompany("no-default", ""))
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(crewletaiv1alpha1.ReasonEngineClassNotFound))
			Expect(condition.Message).To(ContainSubstring("no EngineClass is annotated"))
		})

		It("runs with the one default class", func() {
			newClass("the-default", true, crewletaiv1alpha1.EngineLimits{})
			condition := accepted(newCompany("one-default", ""))
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Message).To(ContainSubstring("the-default"))
		})

		It("is refused while several classes are the default", func() {
			newClass("default-a", true, crewletaiv1alpha1.EngineLimits{})
			newClass("default-b", true, crewletaiv1alpha1.EngineLimits{})
			condition := accepted(newCompany("two-defaults", ""))
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(crewletaiv1alpha1.ReasonEngineClassNotFound))
			Expect(condition.Message).To(ContainSubstring("default-a, default-b"))
		})
	})

	Context("the class's limits", func() {
		newSeat := func(name, company string) {
			GinkgoHelper()
			Expect(k8sClient.Create(ctx, &crewletaiv1alpha1.Seat{
				Namespace: namespace,
				Name:      name,
				Spec:      crewletaiv1alpha1.SeatSpec{CompanyRef: crewletaiv1alpha1.CompanyReference{Name: company}},
			})).To(Succeed())
		}
		newServer := func(name, company string) {
			GinkgoHelper()
			Expect(k8sClient.Create(ctx, &crewletaiv1alpha1.MCPServer{
				Namespace: namespace,
				Name:      name,
				Spec: crewletaiv1alpha1.MCPServerSpec{
					CompanyRef: crewletaiv1alpha1.CompanyReference{Name: company},
					Command:    "uvx",
				},
			})).To(Succeed())
		}

		It("count only the Company's own Seats", func() {
			newClass("two-seats", false, crewletaiv1alpha1.EngineLimits{MaxSeats: new(int32(2))})
			company := newCompany("seats", "two-seats")
			newSeat("ana", company.Name)
			newSeat("bob", company.Name)
			// Another company's seat in the same namespace counts for that
			// company, not this one.
			newSeat("eve", "another-company")
			Expect(accepted(company).Status).To(Equal(metav1.ConditionTrue))

			newSeat("cid", company.Name)
			condition := accepted(company)
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(crewletaiv1alpha1.ReasonLimitExceeded))
			Expect(condition.Message).To(ContainSubstring("3 Seats"))
			Expect(condition.Message).To(ContainSubstring("allows 2"))
		})

		It("refuse a Company with more MCPServers than the class allows", func() {
			newClass("no-servers", false, crewletaiv1alpha1.EngineLimits{MaxMCPServers: new(int32(0))})
			company := newCompany("servers", "no-servers")
			Expect(accepted(company).Status).To(Equal(metav1.ConditionTrue))

			newServer("github", company.Name)
			condition := accepted(company)
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(crewletaiv1alpha1.ReasonLimitExceeded))
			Expect(condition.Message).To(ContainSubstring(fmt.Sprintf("%d MCPServers", 1)))
		})
	})

	// A request can outlive its object: the workqueue still holds the key of
	// a Company deleted since it was queued, and that is not an error.
	It("reconciles a Company that no longer exists", func() {
		reconcileCompany("never-created")
	})
})

var _ = Describe("Company controller event mapping", func() {
	It("maps a Seat and an MCPServer to the Company they name", func() {
		const tenant, nimbus = "tenant", "nimbus"
		seat := &crewletaiv1alpha1.Seat{
			Namespace: tenant,
			Name:      "ana",
			Spec:      crewletaiv1alpha1.SeatSpec{CompanyRef: crewletaiv1alpha1.CompanyReference{Name: nimbus}},
		}
		server := &crewletaiv1alpha1.MCPServer{
			Namespace: tenant,
			Name:      "github",
			Spec:      crewletaiv1alpha1.MCPServerSpec{CompanyRef: crewletaiv1alpha1.CompanyReference{Name: nimbus}},
		}
		want := []reconcile.Request{{Namespace: tenant, Name: nimbus}}
		Expect(companyOfChild(ctx, seat)).To(Equal(want))
		Expect(companyOfChild(ctx, server)).To(Equal(want))
	})

	It("maps an EngineClass to the Companies that name it or name none", func() {
		ns := &corev1.Namespace{GenerateName: "class-mapping-"}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		for name, class := range map[string]string{"names-it": "mapped", "names-none": "", "names-another": "other"} {
			Expect(k8sClient.Create(ctx, &crewletaiv1alpha1.Company{
				Namespace: ns.Name,
				Name:      name,
				Spec:      crewletaiv1alpha1.CompanySpec{CompanyName: "Nimbus", EngineClassName: class},
			})).To(Succeed())
		}
		r := &CompanyReconciler{Client: k8sClient}
		var got []string
		for _, req := range r.companiesOfClass(ctx, &crewletaiv1alpha1.EngineClass{Name: "mapped"}) {
			if req.Namespace == ns.Name {
				got = append(got, req.Name)
			}
		}
		Expect(got).To(ConsistOf("names-it", "names-none"))
	})
})
