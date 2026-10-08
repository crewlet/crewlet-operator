package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

var _ = Describe("Company controller", func() {
	key := types.NamespacedName{Name: "nimbus", Namespace: "default"}

	reconcileCompany := func() error {
		r := &CompanyReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		return err
	}

	It("reconciles an existing Company", func() {
		company := &crewletaiv1alpha1.Company{Name: key.Name, Namespace: key.Namespace}
		Expect(k8sClient.Create(ctx, company)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, company)).To(Succeed()) })

		Expect(reconcileCompany()).To(Succeed())
	})

	// A request can outlive its object: the workqueue still holds the key of
	// a Company deleted since it was queued, and that is not an error.
	It("reconciles a Company that no longer exists", func() {
		Expect(reconcileCompany()).To(Succeed())
	})
})
