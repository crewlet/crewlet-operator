package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

var _ = Describe("EngineClass controller", func() {
	// EngineClass is cluster-scoped, so its key has no namespace.
	key := types.NamespacedName{Name: "pro"}

	reconcileEngineClass := func() error {
		r := &EngineClassReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		return err
	}

	It("reconciles an existing EngineClass", func() {
		class := &crewletaiv1alpha1.EngineClass{Name: key.Name}
		Expect(k8sClient.Create(ctx, class)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, class)).To(Succeed()) })

		Expect(reconcileEngineClass()).To(Succeed())
	})

	It("reconciles an EngineClass that no longer exists", func() {
		Expect(reconcileEngineClass()).To(Succeed())
	})
})
