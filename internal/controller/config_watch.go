package controller

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// The cache indexes the config reconciler looks objects up by.
const (
	// companyRefIndex indexes the parts of a company (Units, Seats,
	// LLMProviders, MCPServers, Integrations) by spec.companyRef.name, so a
	// reconcile lists a company's parts without listing the namespace.
	companyRefIndex = "spec.companyRef.name"

	// refIndex indexes a Company, its parts and EngineClasses by the
	// Secrets their specs reference, as "<namespace>/<name>", so a change
	// to a Secret finds the companies that read it without anyone listing
	// every object of every kind.
	refIndex = "crewlet.ai/secretRef"
)

// quietPeriod is how long a company's parts and the Secrets they reference
// must go unchanged before the operator applies a new revision.
//
// A platform that provisions a company writes its objects one request at a
// time, twenty Seats in as many requests, and each revision is a permanent
// entry in the engine's history and a rebuild of every seat. Waiting until
// the writes pause turns such a burst into one revision. Two seconds is
// longer than the gap between the writes of one provisioning run, and short
// enough that a person editing one object does not notice the wait.
const quietPeriod = 2 * time.Second

// changeClock remembers, per Company, when the last change to one of its
// parts or referenced Secrets arrived, so a reconcile can tell whether a
// burst of changes is still under way.
//
// It is kept in memory rather than read from the objects, because a
// deletion leaves no object to read a time from. Losing it on a restart
// costs at most one revision more than coalescing would have made.
type changeClock struct {
	mu   sync.Mutex
	last map[types.NamespacedName]time.Time
	now  func() time.Time
}

// touch records a change to the company's parts now.
func (c *changeClock) touch(company types.NamespacedName) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil {
		c.last = map[types.NamespacedName]time.Time{}
	}
	c.last[company] = c.clock()
}

// unsettled returns how much of the quiet period after the company's last
// change is still to run, and zero once it has run, forgetting the change
// then.
func (c *changeClock) unsettled(company types.NamespacedName) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	last, ok := c.last[company]
	if !ok {
		return 0
	}
	if wait := quietPeriod - c.clock().Sub(last); wait > 0 {
		return wait
	}
	delete(c.last, company)
	return 0
}

func (c *changeClock) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// companyRefOf returns the Company a part of a company names, or "" for an
// object of any other kind.
func companyRefOf(obj client.Object) string {
	switch o := obj.(type) {
	case *crewletaiv1alpha1.Unit:
		return o.Spec.CompanyRef.Name
	case *crewletaiv1alpha1.Seat:
		return o.Spec.CompanyRef.Name
	case *crewletaiv1alpha1.LLMProvider:
		return o.Spec.CompanyRef.Name
	case *crewletaiv1alpha1.MCPServer:
		return o.Spec.CompanyRef.Name
	case *crewletaiv1alpha1.Integration:
		return o.Spec.CompanyRef.Name
	}
	return ""
}

// indexCompanyRef is the companyRefIndex IndexerFunc.
func indexCompanyRef(obj client.Object) []string {
	if company := companyRefOf(obj); company != "" {
		return []string{company}
	}
	return nil
}

// indexSecretRefs is the refIndex IndexerFunc.
func indexSecretRefs(obj client.Object) []string {
	var out []string
	for _, k := range secretRefsOf(obj) {
		if key := k.Namespace + "/" + k.Name; !slices.Contains(out, key) {
			out = append(out, key)
		}
	}
	return out
}

// secretRefsOf returns every Secret reference in an object's spec, with the
// key when the reference names one and an empty Key when it names the whole
// Secret (a SecretReference, whose keys the field documents). A namespaced
// object's references are in its own namespace; an EngineClass's name their
// own.
//
// It walks the spec by reflection, finding the API's four reference types
// wherever they are. A list of the fields that hold credentials, written out
// by hand, would have to grow with every field the API adds, and a field it
// missed would be a Secret whose rotation never reaches the engine, without
// any test of the field itself failing.
func secretRefsOf(obj client.Object) []secretref.Key {
	spec := reflect.ValueOf(obj).Elem().FieldByName("Spec")
	if !spec.IsValid() {
		return nil
	}
	var out []secretref.Key
	walkSecretRefs(spec, obj.GetNamespace(), &out)
	return out
}

var (
	secretKeyReferenceType           = reflect.TypeFor[crewletaiv1alpha1.SecretKeyReference]()
	secretReferenceType              = reflect.TypeFor[crewletaiv1alpha1.SecretReference]()
	namespacedSecretKeyReferenceType = reflect.TypeFor[crewletaiv1alpha1.NamespacedSecretKeyReference]()
	namespacedSecretReferenceType    = reflect.TypeFor[crewletaiv1alpha1.NamespacedSecretReference]()
)

func walkSecretRefs(v reflect.Value, namespace string, out *[]secretref.Key) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			walkSecretRefs(v.Elem(), namespace, out)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			walkSecretRefs(v.Index(i), namespace, out)
		}
	case reflect.Struct:
		var k secretref.Key
		switch v.Type() {
		case secretKeyReferenceType:
			ref := v.Interface().(crewletaiv1alpha1.SecretKeyReference)
			k = secretref.Key{Namespace: namespace, Name: ref.Name, Key: ref.Key}
		case secretReferenceType:
			k = secretref.Key{Namespace: namespace, Name: v.Interface().(crewletaiv1alpha1.SecretReference).Name}
		case namespacedSecretKeyReferenceType:
			ref := v.Interface().(crewletaiv1alpha1.NamespacedSecretKeyReference)
			k = secretref.Key{Namespace: ref.Namespace, Name: ref.Name, Key: ref.Key}
		case namespacedSecretReferenceType:
			ref := v.Interface().(crewletaiv1alpha1.NamespacedSecretReference)
			k = secretref.Key{Namespace: ref.Namespace, Name: ref.Name}
		default:
			for i := range v.NumField() {
				if v.Type().Field(i).IsExported() {
					walkSecretRefs(v.Field(i), namespace, out)
				}
			}
			return
		}
		// An optional reference left unset is the zero struct.
		if k.Name != "" {
			*out = append(*out, k)
		}
	default:
		// Strings, numbers and the label maps hold no reference.
	}
}

// references reports whether ref, one of an object's references, is the
// reference a missing or oversized key k came from: the same key, or the
// whole Secret it is in.
func references(ref, k secretref.Key) bool {
	return ref.Namespace == k.Namespace && ref.Name == k.Name && (ref.Key == "" || ref.Key == k.Key)
}

// companyOfPart maps a part of a company to the Company it names, and
// records the change for the quiet period.
func (r *CompanyConfigReconciler) companyOfPart(_ context.Context, obj client.Object) []reconcile.Request {
	company := companyRefOf(obj)
	if company == "" {
		return nil
	}
	key := types.NamespacedName{Namespace: obj.GetNamespace(), Name: company}
	r.changes.touch(key)
	return []reconcile.Request{{NamespacedName: key}}
}

// companiesOfSecret maps a Secret to every Company that reads it: the ones
// whose own spec or whose parts reference it, and the ones whose class
// offers a platform model provider whose key it holds. Each change is
// recorded for the quiet period: a platform that rotates several keys
// rotates them one write at a time.
func (r *CompanyConfigReconciler) companiesOfSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	ref := client.MatchingFields{refIndex: obj.GetNamespace() + "/" + obj.GetName()}
	log := logf.FromContext(ctx).WithValues("secret", client.ObjectKeyFromObject(obj))
	var companies []types.NamespacedName
	add := func(namespace, name string) {
		key := types.NamespacedName{Namespace: namespace, Name: name}
		if !slices.Contains(companies, key) {
			companies = append(companies, key)
		}
	}

	// The map function cannot return an error. The cache answers these
	// lists from memory, so they fail only while it is not synced, before
	// any event is delivered; log rather than drop it silently.
	list := func(l client.ObjectList, opts ...client.ListOption) bool {
		if err := r.List(ctx, l, append(opts, ref)...); err != nil {
			log.Error(err, "Failed to list the objects a Secret change affects", "kind", fmt.Sprintf("%T", l))
			return false
		}
		return true
	}
	inNamespace := client.InNamespace(obj.GetNamespace())

	if companyList := (&crewletaiv1alpha1.CompanyList{}); list(companyList, inNamespace) {
		for i := range companyList.Items {
			add(companyList.Items[i].Namespace, companyList.Items[i].Name)
		}
	}
	for _, parts := range partLists() {
		if !list(parts, inNamespace) {
			continue
		}
		for _, part := range itemsOf(parts) {
			add(part.GetNamespace(), companyRefOf(part))
		}
	}
	if classes := (&crewletaiv1alpha1.EngineClassList{}); list(classes) {
		for i := range classes.Items {
			for _, company := range r.companiesOfClass(ctx, &classes.Items[i]) {
				add(company.Namespace, company.Name)
			}
		}
	}

	requests := make([]reconcile.Request, 0, len(companies))
	for _, key := range companies {
		r.changes.touch(key)
		requests = append(requests, reconcile.Request{NamespacedName: key})
	}
	return requests
}

// companiesOfClass maps an EngineClass to the Companies that run with it
// (the ones that name it, and the ones that name none, whose class it may be
// by default). A change to a class reaches the document through its
// platform model providers, so the Companies that do not use them are left
// out.
func (r *CompanyConfigReconciler) companiesOfClass(ctx context.Context, obj client.Object) []reconcile.Request {
	companies := &crewletaiv1alpha1.CompanyList{}
	if err := r.List(ctx, companies); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list the Companies an EngineClass change affects", "engineClass", obj.GetName())
		return nil
	}
	var requests []reconcile.Request
	for i := range companies.Items {
		c := &companies.Items[i]
		if c.Spec.EngineClassName != "" && c.Spec.EngineClassName != obj.GetName() {
			continue
		}
		if use := c.Spec.LLM.UsePlatformProviders; use == nil || !*use {
			continue
		}
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(c)})
	}
	return requests
}

// partLists returns an empty list of each kind of part of a company.
func partLists() []client.ObjectList {
	return []client.ObjectList{
		&crewletaiv1alpha1.UnitList{},
		&crewletaiv1alpha1.SeatList{},
		&crewletaiv1alpha1.LLMProviderList{},
		&crewletaiv1alpha1.MCPServerList{},
		&crewletaiv1alpha1.IntegrationList{},
	}
}

// itemsOf returns the objects of a list partLists returned.
func itemsOf(list client.ObjectList) []client.Object {
	var out []client.Object
	switch l := list.(type) {
	case *crewletaiv1alpha1.UnitList:
		for i := range l.Items {
			out = append(out, &l.Items[i])
		}
	case *crewletaiv1alpha1.SeatList:
		for i := range l.Items {
			out = append(out, &l.Items[i])
		}
	case *crewletaiv1alpha1.LLMProviderList:
		for i := range l.Items {
			out = append(out, &l.Items[i])
		}
	case *crewletaiv1alpha1.MCPServerList:
		for i := range l.Items {
			out = append(out, &l.Items[i])
		}
	case *crewletaiv1alpha1.IntegrationList:
		for i := range l.Items {
			out = append(out, &l.Items[i])
		}
	}
	return out
}
