package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// llmKey is the key of the Secret <company>-llm that holds the test
// companies' model provider key.
const llmKey = "apiKey"

// testEngines resolves each Company to an engine a spec started, in place of
// the data pod a cluster would run.
type testEngines struct {
	mu      sync.Mutex
	engines map[types.NamespacedName]*enginetest.Engine
	// applies, for a Company, is called with each document the reconciler
	// writes to its engine (a dry run is no write), before the engine sees
	// it: what the cluster holds at that moment is what a spec asserts.
	applies map[types.NamespacedName]func(document []byte)
}

// watchApplies calls hook with each document the reconciler writes to the
// Company's engine, before the engine has it.
func (t *testEngines) watchApplies(key types.NamespacedName, hook func(document []byte)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.applies == nil {
		t.applies = map[types.NamespacedName]func([]byte){}
	}
	t.applies[key] = hook
}

// applyWatch hands each PUT /config that is not a dry run to hook, then sends
// it on.
type applyWatch struct {
	base http.RoundTripper
	hook func(document []byte)
}

func (w *applyWatch) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodPut && req.URL.Path == "/config" && req.URL.Query().Get("dry_run") == "" && req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		document, err := io.ReadAll(body)
		if err != nil {
			return nil, err
		}
		w.hook(document)
	}
	return w.base.RoundTrip(req)
}

func (t *testEngines) set(key types.NamespacedName, e *enginetest.Engine) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.engines == nil {
		t.engines = map[types.NamespacedName]*enginetest.Engine{}
	}
	t.engines[key] = e
}

func (t *testEngines) Engine(_ context.Context, c *crewletaiv1alpha1.Company) (*engine.Client, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.engines[client.ObjectKeyFromObject(c)]
	if !ok {
		return nil, fmt.Errorf("no engine runs for Company %s/%s", c.Namespace, c.Name)
	}
	if hook, ok := t.applies[client.ObjectKeyFromObject(c)]; ok {
		hc := engine.NewHTTPClient()
		hc.Transport = &applyWatch{base: hc.Transport, hook: hook}
		return engine.New(e.URL, e.OperatorToken, engine.WithHTTPClient(hc)), nil
	}
	return e.Client(), nil
}

// eventLog records the events the reconciler emits.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) Eventf(regarding, _ runtime.Object, eventType, reason, _, note string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	name := regarding.(client.Object).GetName()
	l.events = append(l.events, name+" "+eventType+" "+reason+" "+fmt.Sprintf(note, args...))
}

// of returns the events recorded on the Company named.
func (l *eventLog) of(company string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, e := range l.events {
		if rest, ok := strings.CutPrefix(e, company+" "); ok {
			out = append(out, rest)
		}
	}
	return out
}

// revision is one entry of the engine's revision history.
type revision struct {
	ID        string `json:"revision_id"`
	CreatedBy string `json:"created_by"`
	Summary   string `json:"summary"`
	Active    bool   `json:"is_active"`
}

// engineGet reads one of the engine's own routes, as the operator's token.
// The engine client has no method for these, because the operator never
// needs them: they are how the specs see what the engine holds.
func engineGet(e *enginetest.Engine, path string, out any) int {
	GinkgoHelper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.URL+path, nil)
	Expect(err).NotTo(HaveOccurred())
	req.Header.Set("Authorization", "Bearer "+e.OperatorToken)
	resp, err := http.DefaultClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	defer func() { Expect(resp.Body.Close()).To(Succeed()) }()
	if resp.StatusCode == http.StatusOK && out != nil {
		Expect(json.NewDecoder(resp.Body).Decode(out)).To(Succeed())
	}
	return resp.StatusCode
}

// revisions is the engine's revision history, newest first.
func revisions(e *enginetest.Engine) []revision {
	GinkgoHelper()
	var out []revision
	Expect(engineGet(e, "/config/revisions?limit=200", &out)).To(Equal(http.StatusOK))
	return out
}

// storedValue reveals one value of the engine's secret store, the
// break-glass read no operator code makes.
func storedValue(e *enginetest.Engine, name string) string {
	GinkgoHelper()
	var out struct {
		Value string `json:"value"`
	}
	if engineGet(e, "/secrets/"+name+"?reveal=true", &out) != http.StatusOK {
		return ""
	}
	return out.Value
}

// storeListing is the engine's secret store, by name.
func storeListing(e *enginetest.Engine) map[string]engine.SecretInfo {
	GinkgoHelper()
	listed, err := e.Client().ListSecrets(ctx)
	Expect(err).NotTo(HaveOccurred())
	out := map[string]engine.SecretInfo{}
	for _, s := range listed {
		out[s.Name] = s
	}
	return out
}

// The specs run the config reconciler in a manager, as cmd/main.go does, so
// its watches, indexes and quiet period are what drive it, against a real
// engine per Company that enginetest starts on the machine. The runtime
// reconciler does not run: no data pod could start here. The specs write the
// status it would write instead (Accepted, RuntimeReady, the public base
// URL), and say so.
var _ = Describe("Company config", Ordered, func() {
	const settle = 60 * time.Second
	const baseURL = "https://hooks.example.com/c/nimbus"
	var (
		namespace string
		class     *crewletaiv1alpha1.EngineClass
		engines   = &testEngines{}
		log       = &eventLog{}
	)

	BeforeAll(func() {
		ns := &corev1.Namespace{GenerateName: "config-"}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespace = ns.Name
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, &corev1.Namespace{Name: operatorNamespace}))).To(Succeed())

		// The sample class, under a name of this suite's own and not the
		// default, so that the other specs' Companies never run with it.
		_, class = sampleParts(namespace)
		class.Name = namespace
		class.Annotations = nil
		Expect(k8sClient.Create(ctx, class)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, class)).To(Succeed()) })

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
			Cache: func() cache.Options {
				opts := CacheOptions()
				opts.DefaultNamespaces = map[string]cache.Config{namespace: {}, operatorNamespace: {}}
				return opts
			}(),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect((&CompanyConfigReconciler{
			Client:    mgr.GetClient(),
			APIReader: mgr.GetAPIReader(),
			Engines:   engines,
			Recorder:  log,
			// Short, so that the specs of drift do not wait five minutes
			// for the engine to be looked at again.
			ResyncInterval: 2 * time.Second,
		}).SetupWithManager(mgr)).To(Succeed())

		managerCtx, stop := context.WithCancel(ctx)
		done := make(chan error)
		go func() {
			defer GinkgoRecover()
			done <- mgr.Start(managerCtx)
		}()
		DeferCleanup(func() {
			stop()
			Expect(<-done).To(Succeed())
		})
	})

	// --- fixtures ---------------------------------------------------- //

	// standInAt writes the status the runtime reconciler would: the Company
	// is accepted, its data pod runs (not ready: an engine with no document
	// never is), and vendors reach it at base, its public address, or at
	// none when base is empty.
	standInAt := func(company *crewletaiv1alpha1.Company, base string) {
		GinkgoHelper()
		Eventually(func(g Gomega) {
			got := &crewletaiv1alpha1.Company{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(company), got)).To(Succeed())
			before := got.DeepCopy()
			for _, c := range []metav1.Condition{
				{Type: crewletaiv1alpha1.ConditionAccepted, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonValid},
				{Type: crewletaiv1alpha1.ConditionRuntimeReady, Status: metav1.ConditionFalse, Reason: crewletaiv1alpha1.ReasonPodsNotReady},
			} {
				c.ObservedGeneration = got.Generation
				meta.SetStatusCondition(&got.Status.Conditions, c)
			}
			got.Status.Endpoints.PublicBaseURL = base
			g.Expect(k8sClient.Status().Patch(ctx, got, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))).To(Succeed())
		}).WithTimeout(settle).Should(Succeed())
	}
	// standIn is standInAt the suite's public address.
	standIn := func(company *crewletaiv1alpha1.Company) {
		GinkgoHelper()
		standInAt(company, baseURL)
	}

	// startEngine starts the engine a Company's document goes to. The
	// engine's document is not managed unless opts say so, as the data pod's
	// is not until it restarts onto a crewlet.yaml that makes it so.
	startEngine := func(company string, opts ...enginetest.Option) *enginetest.Engine {
		GinkgoHelper()
		e := enginetest.Start(GinkgoT(), opts...)
		engines.set(types.NamespacedName{Namespace: namespace, Name: company}, e)
		return e
	}

	secret := func(name string, data map[string]string) *corev1.Secret {
		GinkgoHelper()
		s := &corev1.Secret{Namespace: namespace, Name: name, StringData: data}
		Expect(k8sClient.Create(ctx, s)).To(Succeed())
		return s
	}

	// company creates a small company: one OpenAI-compatible provider whose
	// key is in the Secret <name>-llm, and the Seats named, in the root of
	// the chart.
	company := func(name string, policy crewletaiv1alpha1.ManagementPolicy, seats ...string) *crewletaiv1alpha1.Company {
		GinkgoHelper()
		c := &crewletaiv1alpha1.Company{
			Namespace: namespace, Name: name,
			Spec: crewletaiv1alpha1.CompanySpec{
				CompanyName: "Acme " + name, EngineClassName: class.Name,
				Config: crewletaiv1alpha1.ConfigSettings{ManagementPolicy: policy},
			},
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		Expect(k8sClient.Create(ctx, &crewletaiv1alpha1.LLMProvider{
			Namespace: namespace, Name: name + "-llm",
			Spec: crewletaiv1alpha1.LLMProviderSpec{
				CompanyRef: crewletaiv1alpha1.CompanyReference{Name: name},
				LLMProviderSettings: crewletaiv1alpha1.LLMProviderSettings{
					Type: crewletaiv1alpha1.LLMProviderOpenAICompatible, Model: "test-model",
					// Port 9 on the loopback interface: nothing answers, so
					// no seat ever reaches a model.
					BaseURL: "http://127.0.0.1:9/v1",
				},
				APIKeySecretRefs: []crewletaiv1alpha1.SecretKeyReference{{Name: name + "-llm", Key: llmKey}},
			},
		})).To(Succeed())
		for _, s := range seats {
			newSeat(namespace, name, s)
		}
		return c
	}

	conditionOf := func(obj client.Object, t string) func(Gomega) metav1.Condition {
		return func(g Gomega) metav1.Condition {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			c := meta.FindStatusCondition(conditionsOfObject(obj), t)
			g.Expect(c).NotTo(BeNil(), "%T %s has no %s condition", obj, obj.GetName(), t)
			return *c
		}
	}
	reasonOf := func(obj client.Object, t string) func(Gomega) string {
		return func(g Gomega) string { return conditionOf(obj, t)(g).Reason }
	}
	// applied waits until the Company's document is applied and returns
	// the Company then.
	applied := func(c *crewletaiv1alpha1.Company, reason string) *crewletaiv1alpha1.Company {
		GinkgoHelper()
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)).WithTimeout(settle).Should(Equal(reason))
		return c
	}
	seat := func(name string) *crewletaiv1alpha1.Seat {
		return &crewletaiv1alpha1.Seat{Namespace: namespace, Name: name}
	}
	// rendered renders the Company's document from the objects as the API
	// server holds them, defaults included, with the Company's own class.
	rendered := func(c *crewletaiv1alpha1.Company) *render.Document {
		GinkgoHelper()
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(c), c)).To(Succeed())
		own := &crewletaiv1alpha1.EngineClass{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: c.Spec.EngineClassName}, own)).To(Succeed())
		in := &render.Input{Company: c, Class: own, StoreName: secretsync.StoreName, ProvisionedName: secretsync.ProvisionedName, PublicBaseURL: c.Status.Endpoints.PublicBaseURL}
		in.Units = listOf[crewletaiv1alpha1.Unit](&crewletaiv1alpha1.UnitList{}, namespace, c.Name)
		in.Seats = listOf[crewletaiv1alpha1.Seat](&crewletaiv1alpha1.SeatList{}, namespace, c.Name)
		in.LLMProviders = listOf[crewletaiv1alpha1.LLMProvider](&crewletaiv1alpha1.LLMProviderList{}, namespace, c.Name)
		in.MCPServers = listOf[crewletaiv1alpha1.MCPServer](&crewletaiv1alpha1.MCPServerList{}, namespace, c.Name)
		in.Integrations = listOf[crewletaiv1alpha1.Integration](&crewletaiv1alpha1.IntegrationList{}, namespace, c.Name)
		in.Schedules = listOf[crewletaiv1alpha1.Schedule](&crewletaiv1alpha1.ScheduleList{}, namespace, c.Name)
		doc, err := render.Company(in)
		Expect(err).NotTo(HaveOccurred())
		return doc
	}
	// runsRendered asserts that the engine runs the Company's document.
	runsRendered := func(e *enginetest.Engine, c *crewletaiv1alpha1.Company) engine.ActiveConfig {
		GinkgoHelper()
		active, err := e.Client().ActiveConfig(ctx)
		Expect(err).NotTo(HaveOccurred())
		diffs, err := render.Diff(rendered(c).YAML, active.Document)
		Expect(err).NotTo(HaveOccurred())
		Expect(diffs).To(BeEmpty(), "the engine's document differs from the rendered one:\n%s", active.Document)
		return active
	}

	// --- specs -------------------------------------------------------- //

	It("applies the example company whole: its document, its Secret values and every part's status", func() {
		const name = "nimbus"
		e := startEngine(name)

		By("creating the Nimbus samples and a Secret for every key they reference")
		objects, _ := sampleParts(namespace)
		var nimbus *crewletaiv1alpha1.Company
		for _, obj := range objects {
			if c, ok := obj.(*crewletaiv1alpha1.Company); ok {
				nimbus = c
				c.Spec.EngineClassName = class.Name
			}
		}
		values := map[string]string{}
		probe := inputOf(append([]client.Object{class}, objects...))
		probeDoc, err := render.Company(probe)
		Expect(err).NotTo(HaveOccurred())
		bySecret := map[types.NamespacedName]map[string]string{}
		for _, k := range probeDoc.Refs {
			ref := types.NamespacedName{Namespace: k.Namespace, Name: k.Name}
			if bySecret[ref] == nil {
				bySecret[ref] = map[string]string{}
			}
			value := "value-of-" + k.Name + "-" + k.Key
			bySecret[ref][k.Key] = value
			values[secretsync.StoreName(k)] = value
		}
		for ref, data := range bySecret {
			s := &corev1.Secret{Namespace: ref.Namespace, Name: ref.Name, StringData: data}
			Expect(k8sClient.Create(ctx, s)).To(Succeed())
			DeferCleanup(func() { Expect(k8sClient.Delete(ctx, s)).To(Succeed()) })
		}
		for _, obj := range objects {
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		}
		standIn(nimbus)

		By("waiting for the document to be applied")
		applied(nimbus, crewletaiv1alpha1.ReasonApplied)
		By("reporting the engine's alarms")
		Eventually(reasonOf(nimbus, crewletaiv1alpha1.ConditionEngineHealthy)).WithTimeout(settle).
			Should(BeElementOf(crewletaiv1alpha1.ReasonNoAlarms, crewletaiv1alpha1.ReasonAlarms, crewletaiv1alpha1.ReasonAlarmsNotReported))
		active := runsRendered(e, nimbus)
		history := revisions(e)
		Expect(history).To(HaveLen(1), "one write, the whole document")
		Expect(history[0].CreatedBy).To(Equal(engine.TokenLogin(enginetest.OperatorTokenID)))
		Expect(history[0].Summary).To(HavePrefix("crewlet-operator: Company/nimbus"))
		Expect(nimbus.Status.Config.RevisionID).To(Equal(active.RevisionID))
		Expect(nimbus.Status.Config.Epoch).NotTo(BeNil())
		health, err := e.Client().Health(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(health.AppliedEpoch).To(BeNumerically(">=", *nimbus.Status.Config.Epoch),
			"ConfigApplied is True while the engine has not applied the epoch the write returned")
		Expect(nimbus.Status.Config.AppliedHash).NotTo(BeEmpty())
		Expect(nimbus.Status.Config.AppliedAt).NotTo(BeNil())
		Expect(nimbus.Status.Config.Objects).To(Equal(crewletaiv1alpha1.AppliedObjectCounts{
			Units: new(int32(2)), Seats: new(int32(3)), LLMProviders: new(int32(2)), MCPServers: new(int32(2)), Integrations: new(int32(3)),
			Schedules: new(int32(2)),
		}))
		Expect(conditionOf(nimbus, crewletaiv1alpha1.ConditionSecretsSynced)(Default).Reason).To(Equal(crewletaiv1alpha1.ReasonSynced))

		By("finding every referenced value in the engine's secret store, written by the operator")
		listed := storeListing(e)
		Expect(slices.Sorted(maps.Keys(listed))).To(Equal(slices.Sorted(maps.Keys(values))))
		for storeName, value := range values {
			Expect(listed[storeName].Source).To(Equal(secretsync.Source))
			Expect(storedValue(e, storeName)).To(Equal(value), "the value of %s", storeName)
		}

		By("reading every part's status")
		for _, obj := range objects {
			if _, ok := obj.(*crewletaiv1alpha1.Company); ok {
				continue
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			for _, t := range []string{crewletaiv1alpha1.ConditionAccepted, crewletaiv1alpha1.ConditionResolvedRefs, crewletaiv1alpha1.ConditionApplied} {
				c := meta.FindStatusCondition(conditionsOfObject(obj), t)
				Expect(c).NotTo(BeNil(), "%T %s has no %s condition", obj, obj.GetName(), t)
				Expect(c.Status).To(Equal(metav1.ConditionTrue), "%T %s: %s is %s: %s", obj, obj.GetName(), t, c.Reason, c.Message)
				Expect(c.ObservedGeneration).To(Equal(obj.GetGeneration()))
			}
			Expect(meta.FindStatusCondition(conditionsOfObject(obj), crewletaiv1alpha1.ConditionApplied).Message).To(ContainSubstring(active.RevisionID))
		}
		ana := seat("ana")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ana), ana)).To(Succeed())
		Expect(ana.Status.Handle).To(Equal("ana"))
		Expect(ana.Status.SeatID).To(Equal(render.SeatID("Nimbus", "ana")))
		Expect(ana.Status.Placement.Pool).To(Equal(crewletaiv1alpha1.DataPool))
		Expect(ana.Status.Integrations.Slack.RequestURL).To(Equal(baseURL + "/webhooks/slack/ana"))
		Expect(ana.Status.Integrations.GitHub.WebhookURL).To(Equal(baseURL + "/webhooks/github/ana"))
		jane := seat("jane")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(jane), jane)).To(Succeed())
		Expect(jane.Status.Handle).To(Equal("jane"))
		Expect(jane.Status.Placement).To(BeZero(), "the engine does not run a Human seat")
		Expect(jane.Status.SeatID).To(BeEmpty(), "the engine gives a Human seat no agent id")
		Expect(jane.Status.Integrations).To(BeZero(), "a Human seat has no vendor apps of its own")

		// The samples' two Units: a department, and a team in it.
		engineering := &crewletaiv1alpha1.Unit{Namespace: namespace, Name: "engineering"}
		platform := &crewletaiv1alpha1.Unit{Namespace: namespace, Name: "platform"}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(engineering), engineering)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(platform), platform)).To(Succeed())
		Expect(engineering.Status.Depth).To(Equal(new(int32(0))))
		Expect(engineering.Status.Seats).To(Equal(new(int32(1))))
		Expect(platform.Status.Depth).To(Equal(new(int32(1))))
		Expect(platform.Status.Seats).To(Equal(new(int32(1))))

		By("reporting each Integration's health as the engine judges it, once the engine has applied the revision")
		tools, err := e.Client().Integrations(ctx)
		Expect(err).NotTo(HaveOccurred())
		for _, obj := range objects {
			in, ok := obj.(*crewletaiv1alpha1.Integration)
			if !ok {
				continue
			}
			Eventually(func(g Gomega) {
				tools, err = e.Client().Integrations(ctx)
				g.Expect(err).NotTo(HaveOccurred())
				var state engine.IntegrationState
				for _, t := range tools {
					if t.Key == healthTool(render.Vendor(in)) {
						state = t.State
					}
				}
				g.Expect(state).NotTo(BeEmpty())
				g.Expect(state).NotTo(Equal(engine.IntegrationNotInUse), "the engine has not applied the revision yet")
				want := crewletaiv1alpha1.ReasonCheckFailed
				if state == engine.IntegrationConnected {
					want = crewletaiv1alpha1.ReasonCheckPassed
				}
				g.Expect(reasonOf(in, crewletaiv1alpha1.ConditionHealthy)(g)).To(Equal(want))
			}).WithTimeout(settle).Should(Succeed())
			want := ""
			if vendor := render.Vendor(in); companyWebhook(vendor) {
				want = baseURL + "/webhooks/" + vendor
			}
			Expect(in.Status.WebhookURL).To(Equal(want))
		}

		By("recording the apply, with no Secret value in any event")
		events := log.of(name)
		Expect(events).To(ContainElement(HavePrefix("Normal Activated Activated revision " + active.RevisionID + " as epoch")))
		Expect(events).To(ContainElement("Normal Applied The engine applied revision " + active.RevisionID +
			fmt.Sprintf(" (epoch %d).", *nimbus.Status.Config.Epoch)))
		Expect(events).To(ContainElement(HavePrefix("Normal SecretsSynced")))
		for _, ev := range events {
			for _, value := range values {
				Expect(ev).NotTo(ContainSubstring(value))
			}
		}
	})

	It("turns a new Seat into one revision, and a burst of Seats into one more", func() {
		const name = "burst"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k1"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)
		Expect(revisions(e)).To(HaveLen(1))

		By("adding one Seat")
		newSeat(namespace, name, "bo")
		Eventually(reasonOf(seat(name+"-bo"), crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		Expect(revisions(e)).To(HaveLen(2))
		runsRendered(e, c)

		By("adding ten Seats within the quiet period")
		start := time.Now()
		for i := range 10 {
			newSeat(namespace, name, fmt.Sprintf("crew-%d", i))
		}
		Expect(time.Since(start)).To(BeNumerically("<", quietPeriod), "the burst took longer than the quiet period, and proves nothing")
		for i := range 10 {
			Eventually(reasonOf(seat(fmt.Sprintf("%s-crew-%d", name, i)), crewletaiv1alpha1.ConditionApplied)).
				WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		}
		Expect(revisions(e)).To(HaveLen(3), "ten Seats written in one burst are one revision")
		runsRendered(e, c)
		Consistently(func() []revision { return revisions(e) }).WithTimeout(3 * quietPeriod).Should(HaveLen(3))
	})

	It("pushes a rotated Secret value and has the engine read it, without changing the document", func() {
		const name = "rotate"
		e := startEngine(name)
		key := secret(name+"-llm", map[string]string{llmKey: "first"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)
		storeName := secretsync.StoreName(secretref.Key{Namespace: namespace, Name: name + "-llm", Key: llmKey})
		Expect(storedValue(e, storeName)).To(Equal("first"))
		before, err := e.Client().ActiveConfig(ctx)
		Expect(err).NotTo(HaveOccurred())
		hash := c.Status.Config.AppliedHash

		By("rewriting the Secret")
		updateLive(key, func(s *corev1.Secret) { s.Data = map[string][]byte{llmKey: []byte("second")} })
		Eventually(func() string { return storedValue(e, storeName) }).WithTimeout(settle).Should(Equal("second"))
		Eventually(func() []revision { return revisions(e) }).WithTimeout(settle).Should(HaveLen(2),
			"the engine reads a rotated value only when a revision activates")
		after := runsRendered(e, c)
		Expect(after.Document).To(Equal(before.Document), "a rotation changes no byte of the document")
		Expect(revisions(e)[0].Summary).To(Equal("crewlet-operator: Secret " + namespace + "/" + name + "-llm"))
		Eventually(func(g Gomega) string {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(c), c)).To(Succeed())
			return c.Status.Config.AppliedHash
		}).WithTimeout(settle).ShouldNot(Equal(hash))

		By("changing only the Secret's labels")
		updatedAt := storeListing(e)[storeName].UpdatedAt
		updateLive(key, func(s *corev1.Secret) { s.Labels = map[string]string{"rotated-by": "a-test"} })
		Consistently(func() []revision { return revisions(e) }).WithTimeout(3 * quietPeriod).Should(HaveLen(2))
		Expect(storeListing(e)[storeName].UpdatedAt).To(Equal(updatedAt), "an unchanged value is not written again")
	})

	It("reports a missing Secret on the part that references it and applies nothing until it exists", func() {
		const name = "missing"
		e := startEngine(name)
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		standIn(c)
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionSecretsSynced)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonSecretNotFound))
		synced := conditionOf(c, crewletaiv1alpha1.ConditionSecretsSynced)(Default)
		Expect(synced.Status).To(Equal(metav1.ConditionFalse))
		Expect(synced.Message).To(ContainSubstring("Secret " + namespace + "/" + name + "-llm does not exist"))
		Expect(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonSecretNotFound))
		provider := &crewletaiv1alpha1.LLMProvider{Namespace: namespace, Name: name + "-llm"}
		Expect(reasonOf(provider, crewletaiv1alpha1.ConditionResolvedRefs)(Default)).To(Equal(crewletaiv1alpha1.ReasonSecretNotFound))
		Expect(reasonOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionResolvedRefs)(Default)).To(Equal(crewletaiv1alpha1.ReasonResolved))
		Expect(reasonOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonBlocked))
		_, err := e.Client().ActiveConfig(ctx)
		Expect(err).To(MatchError(engine.ErrNoActiveRevision))

		By("creating the Secret")
		secret(name+"-llm", map[string]string{"wrongKey": "x"})
		Eventually(func(g Gomega) string {
			return conditionOf(provider, crewletaiv1alpha1.ConditionResolvedRefs)(g).Message
		}).WithTimeout(settle).Should(ContainSubstring(`has no key "` + llmKey + `"`))
		updateLive(&corev1.Secret{Namespace: namespace, Name: name + "-llm"}, func(s *corev1.Secret) {
			s.Data = map[string][]byte{llmKey: []byte("k")}
		})
		applied(c, crewletaiv1alpha1.ReasonApplied)
		runsRendered(e, c)
	})

	It("neither reads nor overwrites a secret sync state Secret it did not create", func() {
		const name = "squatted"
		e := startEngine(name)
		mine := secret(name+"-secret-sync", map[string]string{"note": "a Secret of the namespace's own"})
		secret(name+"-llm", map[string]string{llmKey: "k"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce)
		standIn(c)
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionSecretsSynced)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonNameConflict))
		Expect(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonNameConflict))
		got := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(mine), got)).To(Succeed())
		Expect(got.Data).To(Equal(map[string][]byte{"note": []byte("a Secret of the namespace's own")}))
		Expect(got.OwnerReferences).To(BeEmpty())
		Expect(storeListing(e)).To(BeEmpty(), "pushed values without a record of them")
	})

	It("reports a document the engine refuses on the Seat at fault and keeps the last good revision", func() {
		const name = "invalid"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)
		good, err := e.Client().ActiveConfig(ctx)
		Expect(err).NotTo(HaveOccurred())

		By("adding a Seat with a sandbox, which the Company does not offer: only the engine sees the two together")
		coder := newSeat(namespace, name, "coder", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Sandbox = &crewletaiv1alpha1.SeatSandbox{CodingAgent: crewletaiv1alpha1.CodingAgentClaudeCode}
		})
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInvalid))
		Expect(conditionOf(coder, crewletaiv1alpha1.ConditionApplied)(Default).Message).To(ContainSubstring("sandbox"))
		// The parts' status is written before the Company's.
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInvalid))
		Expect(reasonOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonInRevision),
			"ana's generation still runs; only the new Seat is held back")
		active, err := e.Client().ActiveConfig(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(active.RevisionID).To(Equal(good.RevisionID))
		Expect(revisions(e)).To(HaveLen(1))
		Expect(log.of(name)).To(ContainElement(HavePrefix("Warning Invalid")))

		By("taking the sandbox away again")
		updateLive(coder, func(s *crewletaiv1alpha1.Seat) { s.Spec.Sandbox = nil })
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		applied(c, crewletaiv1alpha1.ReasonApplied)
		runsRendered(e, c)
	})

	It("reports a seat's code work on the Seat, applying a seat Crewlet applies and holding one it refuses", func() {
		const name = "codework"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		secret(name+"-e2b", map[string]string{llmKey: "k"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		updateLive(c, func(c *crewletaiv1alpha1.Company) {
			c.Spec.Sandbox = crewletaiv1alpha1.SandboxSettings{E2B: crewletaiv1alpha1.E2BSandbox{
				APIKeySecretRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-e2b", Key: llmKey},
			}}
		})
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)
		ana := seat(name + "-ana")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ana), ana)).To(Succeed())
		Expect(ana.Status.Conditions).NotTo(ContainElement(HaveField("Type", crewletaiv1alpha1.ConditionCodeWorkRunnable)),
			"a seat with no sandbox runs no code work")

		By("adding a Seat whose code work runs ClaudeCode, the default coding agent, on the OpenAICompatible provider")
		coder := newSeat(namespace, name, "coder", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Sandbox = &crewletaiv1alpha1.SeatSandbox{}
		})
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionCodeWorkRunnable)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonSandboxModelUnsupported))
		codeWork := conditionOf(coder, crewletaiv1alpha1.ConditionCodeWorkRunnable)(Default)
		Expect(codeWork.Status).To(Equal(metav1.ConditionFalse))
		Expect(codeWork.ObservedGeneration).To(Equal(coder.Generation))
		Expect(codeWork.Message).To(ContainSubstring("ClaudeCode speaks the Anthropic API only"))
		Expect(codeWork.Message).To(ContainSubstring("spec.sandbox.codingAgent: OpenCode"))
		By("applying the company with the seat all the same, as Crewlet applies it")
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		applied(c, crewletaiv1alpha1.ReasonApplied)
		active := runsRendered(e, c)
		Expect(string(active.Document)).To(ContainSubstring("handle: " + coder.Name))
		Expect(reasonOf(coder, crewletaiv1alpha1.ConditionReady)(Default)).
			To(BeElementOf(crewletaiv1alpha1.ReasonLeaseHeld, crewletaiv1alpha1.ReasonUnplaced), "code work does not count towards Ready")
		Expect(log.of(coder.Name)).To(ContainElement(HavePrefix("Warning " + crewletaiv1alpha1.ReasonSandboxModelUnsupported)))

		By("giving the seat a coding agent that speaks the provider's wire")
		updateLive(coder, func(s *crewletaiv1alpha1.Seat) { s.Spec.Sandbox.CodingAgent = crewletaiv1alpha1.CodingAgentOpenCode })
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionCodeWorkRunnable)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonCodeWorkSupported))
		Expect(conditionOf(coder, crewletaiv1alpha1.ConditionCodeWorkRunnable)(Default).Status).To(Equal(metav1.ConditionTrue))
		Eventually(func(g Gomega) int64 {
			return conditionOf(coder, crewletaiv1alpha1.ConditionApplied)(g).ObservedGeneration
		}).WithTimeout(settle).Should(Equal(coder.Generation))
		applied(c, crewletaiv1alpha1.ReasonApplied)
		good := runsRendered(e, c)

		By("running its code work on a CLIAgent provider of another CLI, which Crewlet refuses on every write")
		Expect(k8sClient.Create(ctx, &crewletaiv1alpha1.LLMProvider{
			Namespace: namespace, Name: name + "-codex",
			Spec: crewletaiv1alpha1.LLMProviderSpec{
				CompanyRef:          crewletaiv1alpha1.CompanyReference{Name: name},
				LLMProviderSettings: crewletaiv1alpha1.LLMProviderSettings{Type: crewletaiv1alpha1.LLMProviderCLIAgent, Model: "gpt-5"},
				CLI: crewletaiv1alpha1.CLIAgentSettings{
					Agent: crewletaiv1alpha1.CLIAgentCodex, Auth: crewletaiv1alpha1.CLIAgentAuth{Mode: crewletaiv1alpha1.CLIAgentAuthAPIKey},
				},
				APIKeySecretRefs: []crewletaiv1alpha1.SecretKeyReference{{Name: name + "-llm", Key: llmKey}},
			},
		})).To(Succeed())
		updateLive(coder, func(s *crewletaiv1alpha1.Seat) {
			s.Spec.LLM.Sandbox = []crewletaiv1alpha1.LLMProviderReference{{Name: name + "-codex"}}
		})
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonCodingAgentMismatch))
		Expect(reasonOf(coder, crewletaiv1alpha1.ConditionCodeWorkRunnable)(Default)).To(Equal(crewletaiv1alpha1.ReasonCodingAgentMismatch))
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonCodingAgentMismatch))
		held, err := e.Client().ActiveConfig(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(held.RevisionID).To(Equal(good.RevisionID), "the document is refused before the engine is asked")

		By("taking the sandbox away")
		updateLive(coder, func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Sandbox = nil
			s.Spec.LLM.Sandbox = nil
		})
		Eventually(func(g Gomega) []metav1.Condition {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(coder), coder)).To(Succeed())
			return coder.Status.Conditions
		}).WithTimeout(settle).ShouldNot(ContainElement(HaveField("Type", crewletaiv1alpha1.ConditionCodeWorkRunnable)))
		applied(c, crewletaiv1alpha1.ReasonApplied)
		runsRendered(e, c)
	})

	It("applies a Schedule into its seat's entry, reports what is wrong with one on it, and removes a deleted one", func() {
		const name = "sched"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		person := newSeat(namespace, name, "jo", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Kind = crewletaiv1alpha1.SeatKindHuman
			s.Spec.Contact.SlackUserID = "U0JO"
		})
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)

		By("adding a Schedule of a seat")
		standup := &crewletaiv1alpha1.Schedule{
			Namespace: namespace, Name: name + "-standup",
			Spec: crewletaiv1alpha1.ScheduleSpec{
				CompanyRef: crewletaiv1alpha1.CompanyReference{Name: name},
				Target:     crewletaiv1alpha1.ScheduleTarget{SeatRef: crewletaiv1alpha1.LocalObjectReference{Name: name + "-ana"}},
				Schedule:   "30 9 * * 1-5",
				TimeZone:   "Europe/Berlin",
				Prompt:     "Post your standup.",
			},
		}
		Expect(k8sClient.Create(ctx, standup)).To(Succeed())
		Eventually(reasonOf(standup, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		Expect(reasonOf(standup, crewletaiv1alpha1.ConditionResolvedRefs)(Default)).To(Equal(crewletaiv1alpha1.ReasonResolved))
		active := runsRendered(e, c)
		Expect(string(active.Document)).To(ContainSubstring("name: " + standup.Name))
		Expect(c.Status.Config.Objects.Schedules).To(Equal(new(int32(1))))

		By("giving it a cron expression of five fields the engine refuses")
		updateLive(standup, func(s *crewletaiv1alpha1.Schedule) { s.Spec.Schedule = "61 9 * * 1-5" })
		Eventually(reasonOf(standup, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInvalid))
		Expect(conditionOf(standup, crewletaiv1alpha1.ConditionApplied)(Default).Message).To(ContainSubstring("cron"))
		Expect(reasonOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonInRevision),
			"the seat's own generation still runs; the Schedule is what holds the document back")

		By("pointing it at a person, whom the engine runs no turn for")
		updateLive(standup, func(s *crewletaiv1alpha1.Schedule) {
			s.Spec.Schedule = "30 9 * * 1-5"
			s.Spec.Target.SeatRef.Name = person.Name
		})
		Eventually(reasonOf(standup, crewletaiv1alpha1.ConditionResolvedRefs)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonHumanSeat))
		Expect(conditionOf(standup, crewletaiv1alpha1.ConditionResolvedRefs)(Default).Message).To(ContainSubstring(person.Name))

		By("pointing it back, then deleting it")
		updateLive(standup, func(s *crewletaiv1alpha1.Schedule) { s.Spec.Target.SeatRef.Name = name + "-ana" })
		Eventually(reasonOf(standup, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		Expect(k8sClient.Delete(ctx, standup)).To(Succeed())
		Eventually(func(g Gomega) {
			active, err := e.Client().ActiveConfig(ctx)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(active.Document)).NotTo(ContainSubstring(standup.Name))
		}).WithTimeout(settle).Should(Succeed())
		applied(c, crewletaiv1alpha1.ReasonApplied)
		runsRendered(e, c)
	})

	It("pauses and resumes a Seat as spec.paused asks, undoes a person's change to it, and reports a pause it does not own", func() {
		const name = "pause"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana", "bo")
		ana, bo := seat(name+"-ana"), seat(name+"-bo")
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)
		// A person, through the engine's own API with a token of their own.
		person := engine.New(e.URL, e.AccessToken)
		enginePause := func(handle string) func(Gomega) engine.SeatPause {
			return func(g Gomega) engine.SeatPause {
				pauses, err := e.Client().SeatPauses(ctx)
				g.Expect(err).NotTo(HaveOccurred())
				return pauses[handle]
			}
		}

		By("reporting a seat nobody paused as running")
		Eventually(reasonOf(ana, crewletaiv1alpha1.ConditionPaused)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonSeatRunning))
		Expect(ana.Status.Pause).To(BeZero())

		By("pausing a Seat that asks to be")
		updateLive(ana, func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Paused = new(true)
			s.Spec.PauseReason = "Deploy freeze"
		})
		Eventually(reasonOf(ana, crewletaiv1alpha1.ConditionPaused)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonSeatPaused))
		Expect(ana.Status.Pause.PausedBy).To(Equal(engine.TokenLogin(enginetest.OperatorTokenID)))
		Expect(ana.Status.Pause.Reason).To(Equal("Deploy freeze"))
		Expect(ana.Status.Pause.PausedAt).NotTo(BeNil())
		Expect(enginePause(name + "-ana")(Default).Paused).To(BeTrue())
		Expect(revisions(e)).To(HaveLen(1), "a pause is not a revision of the document")

		By("pausing it again after a person resumed it in the engine")
		_, err := person.ResumeSeat(ctx, name+"-ana")
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) bool { return enginePause(name + "-ana")(g).Paused }).WithTimeout(settle).Should(BeTrue())
		Eventually(func() []string { return log.of(ana.Name) }).WithTimeout(settle).
			Should(ContainElement(HavePrefix("Warning Drifted")))

		By("reporting, and leaving alone, a pause a person takes of a Seat that does not set spec.paused")
		_, err = person.PauseSeat(ctx, name+"-bo", "Looking into a bad answer", false)
		Expect(err).NotTo(HaveOccurred())
		Eventually(reasonOf(bo, crewletaiv1alpha1.ConditionPaused)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonSeatPaused))
		Expect(bo.Status.Pause.PausedBy).To(Equal(engine.TokenLogin(enginetest.AccessTokenID)))
		Expect(bo.Status.Pause.Reason).To(Equal("Looking into a bad answer"))
		Consistently(func(g Gomega) bool { return enginePause(name + "-bo")(g).Paused }).WithTimeout(3 * time.Second).Should(BeTrue())

		By("resuming the Seat that asks to be")
		updateLive(ana, func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Paused = new(false)
			s.Spec.PauseReason = ""
		})
		Eventually(reasonOf(ana, crewletaiv1alpha1.ConditionPaused)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonSeatRunning))
		Expect(ana.Status.Pause).To(BeZero())
		Expect(enginePause(name + "-ana")(Default).Paused).To(BeFalse())
		Expect(log.of(ana.Name)).To(ContainElement("Normal SeatResumed Resumed the seat in the engine, as spec.paused asks."))
	})

	It("overwrites an edit made through the engine's API under Enforce, and says so", func() {
		const name = "enforce"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)

		By("editing the document as a person in the dashboard would")
		editThroughEngine(e, "goal: Edited in the dashboard.")
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonDriftCorrected))
		runsRendered(e, c)
		history := revisions(e)
		Expect(history).To(HaveLen(3))
		Expect(history[0].CreatedBy).To(Equal(engine.TokenLogin(enginetest.OperatorTokenID)))
		Expect(history[1].CreatedBy).To(Equal(engine.TokenLogin(enginetest.AccessTokenID)))
		Expect(log.of(name)).To(ContainElement(HavePrefix("Warning Drifted The engine ran revision " + history[1].ID)))
	})

	It("compares and never writes under Observe", func() {
		const name = "observe"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyObserve, "ana")
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonDrifted)
		Expect(conditionOf(c, crewletaiv1alpha1.ConditionSecretsSynced)(Default).Status).To(Equal(metav1.ConditionUnknown))
		Expect(reasonOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonDrifted))

		By("writing the rendered document into the engine as someone else")
		accessClient := engine.New(e.URL, e.AccessToken)
		_, err := accessClient.ApplyConfig(ctx, engine.ApplyRequest{Document: rendered(c).YAML, Summary: "written in the dashboard", CreateOnly: true})
		Expect(err).NotTo(HaveOccurred())
		applied(c, crewletaiv1alpha1.ReasonInSync)
		Eventually(reasonOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))

		By("editing a Seat's part of it")
		editThroughEngine(e, "goal: Edited in the dashboard.")
		applied(c, crewletaiv1alpha1.ReasonDrifted)
		Eventually(func(g Gomega) string {
			return conditionOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionApplied)(g).Message
		}).WithTimeout(settle).Should(ContainSubstring("roles[0].goal"))
		Expect(log.of(name)).To(ContainElement(HavePrefix("Warning Drifted")))

		Consistently(func(g Gomega) {
			for _, r := range revisions(e) {
				g.Expect(r.CreatedBy).To(Equal(engine.TokenLogin(enginetest.AccessTokenID)))
			}
			g.Expect(storeListing(e)).To(BeEmpty())
		}).WithTimeout(3 * quietPeriod).Should(Succeed())
	})

	It("seeds an empty engine once and then leaves the document to it", func() {
		const name = "seed"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		c := company(name, crewletaiv1alpha1.ManagementPolicySeed, "ana")
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonSeeded)
		runsRendered(e, c)
		Expect(revisions(e)).To(HaveLen(1))
		Expect(reasonOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonNotManaged))

		By("adding a Seat, which the engine's document no longer follows")
		newSeat(namespace, name, "bo")
		Eventually(reasonOf(seat(name+"-bo"), crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonNotManaged))
		Consistently(func() []revision { return revisions(e) }).WithTimeout(3 * quietPeriod).Should(HaveLen(1))
	})

	It("removes a deleted Seat from the document and its values from the engine's secret store", func() {
		const name = "remove"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		boSlack := name + "-bo-slack"
		secret(boSlack, map[string]string{crewletaiv1alpha1.SlackBotTokenKey: "xoxb", crewletaiv1alpha1.SlackSigningSecretKey: "sig"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		bo := newSeat(namespace, name, "bo", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Integrations.Slack.CredentialsSecretRef.Name = boSlack
		})
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)
		boNames := []string{
			secretsync.StoreName(secretref.Key{Namespace: namespace, Name: boSlack, Key: crewletaiv1alpha1.SlackBotTokenKey}),
			secretsync.StoreName(secretref.Key{Namespace: namespace, Name: boSlack, Key: crewletaiv1alpha1.SlackSigningSecretKey}),
		}
		Expect(storeListing(e)).To(HaveKey(boNames[0]))

		By("leaving an entry of the operator's shape that nothing references, and one of a person's")
		stale := secretsync.StoreName(secretref.Key{Namespace: namespace, Name: "gone", Key: "value"})
		Expect(e.Client().PutSecret(ctx, stale, []byte("old"), secretsync.Source)).To(Succeed())
		Expect(e.Client().PutSecret(ctx, "MANUAL_TOKEN", []byte("mine"), "")).To(Succeed())

		By("deleting the Seat")
		Expect(k8sClient.Delete(ctx, bo)).To(Succeed())
		Eventually(func(g Gomega) {
			active, err := e.Client().ActiveConfig(ctx)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(active.Document)).NotTo(ContainSubstring("handle: " + bo.Name))
		}).WithTimeout(settle).Should(Succeed())
		runsRendered(e, c)
		Eventually(func() map[string]engine.SecretInfo { return storeListing(e) }).WithTimeout(settle).Should(SatisfyAll(
			Not(HaveKey(boNames[0])), Not(HaveKey(boNames[1])), Not(HaveKey(stale)), HaveKey("MANUAL_TOKEN"),
		))
	})

	It("reports a reference that does not resolve on the part that holds it", func() {
		const name = "dangling"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce)
		lost := newSeat(namespace, name, "lost", func(s *crewletaiv1alpha1.Seat) { s.Spec.UnitRef.Name = nowhere })
		standIn(c)
		Eventually(reasonOf(lost, crewletaiv1alpha1.ConditionResolvedRefs)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonRefNotFound))
		Expect(conditionOf(lost, crewletaiv1alpha1.ConditionResolvedRefs)(Default).Message).To(ContainSubstring("Unit nowhere"))
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonRefNotFound))
		_, err := e.Client().ActiveConfig(ctx)
		Expect(err).To(MatchError(engine.ErrNoActiveRevision))
	})

	It("reports a seat that cannot run in its pool on the Seat, as its class or its company says why", func() {
		const name = "pooled"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})

		By("running the company with a class that allows Shared pools and not PerSeat ones")
		pools := class.DeepCopy()
		pools.ResourceVersion = ""
		pools.Name = namespace + "-pools"
		pools.Spec.SeatPools = crewletaiv1alpha1.SeatPoolSettings{Policy: crewletaiv1alpha1.SeatPoolsAllowed, MaxPods: 20}
		Expect(k8sClient.Create(ctx, pools)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, pools)).To(Succeed()) })
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce)
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(c), c)).To(Succeed())
			c.Spec.EngineClassName = pools.Name
			c.Spec.Topology.SeatPools = []crewletaiv1alpha1.SeatPool{
				{Name: poolGeneral, Mode: crewletaiv1alpha1.SeatPoolShared},
				{Name: poolIsolated, Mode: crewletaiv1alpha1.SeatPoolPerSeat},
			}
			// The seats with no chain of their own run on the provider
			// that reaches no model, never on the agent-mode CLI below.
			c.Spec.LLM.Order = []crewletaiv1alpha1.LLMProviderReference{{Name: name + "-llm"}}
			g.Expect(k8sClient.Update(ctx, c)).To(Succeed())
		}).WithTimeout(settle).Should(Succeed())
		placed := func(pool string) func(*crewletaiv1alpha1.Seat) {
			return func(s *crewletaiv1alpha1.Seat) { s.Spec.Placement = crewletaiv1alpha1.SeatPlacement{Pool: pool} }
		}
		shared := newSeat(namespace, name, "shared", placed(poolGeneral))
		alone := newSeat(namespace, name, "alone", placed(poolIsolated))
		lost := newSeat(namespace, name, "lost", placed(nowhere))
		Expect(k8sClient.Create(ctx, &crewletaiv1alpha1.LLMProvider{
			Namespace: namespace, Name: name + "-agent",
			Spec: crewletaiv1alpha1.LLMProviderSpec{
				CompanyRef:          crewletaiv1alpha1.CompanyReference{Name: name},
				LLMProviderSettings: crewletaiv1alpha1.LLMProviderSettings{Type: crewletaiv1alpha1.LLMProviderCLIAgent, Model: cliModel},
				CLI: crewletaiv1alpha1.CLIAgentSettings{
					Agent: crewletaiv1alpha1.CLIAgentClaudeCode, Mode: crewletaiv1alpha1.CLIAgentModeAgent,
					Auth: crewletaiv1alpha1.CLIAgentAuth{TokenSecretRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-llm", Key: llmKey}},
				},
			},
		})).To(Succeed())
		bridged := newSeat(namespace, name, "bridged", placed(poolGeneral), func(s *crewletaiv1alpha1.Seat) {
			s.Spec.LLM.Default = []crewletaiv1alpha1.LLMProviderReference{{Name: name + "-agent"}}
		})
		standIn(c)

		By("reporting the seat in a pool the company does not declare as a reference that does not resolve, which holds nothing")
		Eventually(reasonOf(lost, crewletaiv1alpha1.ConditionResolvedRefs)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonPoolNotFound))
		Expect(conditionOf(lost, crewletaiv1alpha1.ConditionResolvedRefs)(Default).Message).To(ContainSubstring("pool nowhere"))
		Eventually(reasonOf(lost, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonBlocked))

		By("refusing the seat in a PerSeat pool on Applied, its references resolved")
		Eventually(reasonOf(alone, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonPerSeatNotAllowed))
		Expect(conditionOf(alone, crewletaiv1alpha1.ConditionApplied)(Default).Message).To(ContainSubstring("spec.seatPools.allowPerSeat"))
		Expect(conditionOf(alone, crewletaiv1alpha1.ConditionResolvedRefs)(Default).Status).To(Equal(metav1.ConditionTrue))

		By("reporting the seat whose executor runs a CLI in agent mode on a seat pod, which serves no tool bridge, on its Ready alone")
		Eventually(reasonOf(bridged, crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonAgentModeBridgeUnsupported))
		Expect(conditionOf(bridged, crewletaiv1alpha1.ConditionReady)(Default).Message).To(ContainSubstring("LLMProvider " + name + "-agent, a CLI in mode Agent"))
		Expect(conditionOf(bridged, crewletaiv1alpha1.ConditionResolvedRefs)(Default).Status).To(Equal(metav1.ConditionTrue))
		Eventually(reasonOf(bridged, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonBlocked),
			"the seat holds nothing: the other seats' refusals hold the document")

		By("blocking the seat that can run, and recording where it is placed")
		Eventually(reasonOf(shared, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonBlocked))
		Expect(shared.Status.Placement.Pool).To(Equal(poolGeneral))
		Expect(shared.Status.Placement.Workload).To(Equal(sharedPoolStatefulSetName(c, poolGeneral)))
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(alone), alone)).To(Succeed())
		Expect(alone.Status.Placement.Pool).To(Equal(poolIsolated))
		Expect(alone.Status.Placement.Workload).To(BeEmpty(), "the class runs no pod for a seat it refuses its pool")
		// The Company's status is written after its parts'.
		Eventually(func(g Gomega) metav1.ConditionStatus {
			return conditionOf(c, crewletaiv1alpha1.ConditionConfigApplied)(g).Status
		}).WithTimeout(settle).Should(Equal(metav1.ConditionFalse))
		_, err := e.Client().ActiveConfig(ctx)
		Expect(err).To(MatchError(engine.ErrNoActiveRevision))
	})

	It("applies a seat in a pool the company does not declare, and reports it on the Seat", func() {
		const name = "undeclared"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)

		By("placing a new seat in a pool the company does not declare")
		lost := newSeat(namespace, name, "lost", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Placement = crewletaiv1alpha1.SeatPlacement{Pool: nowhere}
		})
		Eventually(reasonOf(lost, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		Expect(reasonOf(lost, crewletaiv1alpha1.ConditionResolvedRefs)(Default)).To(Equal(crewletaiv1alpha1.ReasonPoolNotFound))
		Expect(conditionOf(lost, crewletaiv1alpha1.ConditionResolvedRefs)(Default).Status).To(Equal(metav1.ConditionFalse))
		Eventually(reasonOf(lost, crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonPoolNotFound))
		Expect(lost.Status.Placement.Workload).To(BeEmpty(), "no StatefulSet runs a pool the company does not declare")
		applied(c, crewletaiv1alpha1.ReasonApplied)
		active := runsRendered(e, c)
		Expect(string(active.Document)).To(ContainSubstring(render.UndeclaredPoolLabel+": nowhere"),
			"the seat is pinned to a label the pods of a pool declared later do not carry")
		Expect(string(active.Document)).NotTo(ContainSubstring(render.PoolLabel + ": nowhere"))
		Expect(c.Status.Config.Placement.Pools).NotTo(ContainElement(nowhere), "no StatefulSet is kept for a pool the company does not declare")
		Expect(log.of(lost.Name)).To(ConsistOf(HavePrefix("Warning " + crewletaiv1alpha1.ReasonPoolNotFound)))
	})

	It("applies a seat whose agent-mode executor cannot run, and reports it on the Seat and the LLMProvider", func() {
		const name = "bridge"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		secret(name+"-e2b", map[string]string{llmKey: "k"})

		By("running the company with a class that allows Shared pools")
		pools := class.DeepCopy()
		pools.ResourceVersion = ""
		pools.Name = namespace + "-bridge"
		pools.Spec.SeatPools = crewletaiv1alpha1.SeatPoolSettings{Policy: crewletaiv1alpha1.SeatPoolsAllowed, MaxPods: 20}
		Expect(k8sClient.Create(ctx, pools)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, pools)).To(Succeed()) })
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		updateLive(c, func(c *crewletaiv1alpha1.Company) {
			c.Spec.EngineClassName = pools.Name
			c.Spec.Topology.SeatPools = []crewletaiv1alpha1.SeatPool{{Name: poolGeneral, Mode: crewletaiv1alpha1.SeatPoolShared}}
			c.Spec.LLM.Order = []crewletaiv1alpha1.LLMProviderReference{{Name: name + "-llm"}}
			// An agent-mode run happens in the company's sandbox.
			c.Spec.Sandbox = crewletaiv1alpha1.SandboxSettings{E2B: crewletaiv1alpha1.E2BSandbox{
				APIKeySecretRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-e2b", Key: llmKey},
			}}
		})
		agent := &crewletaiv1alpha1.LLMProvider{
			Namespace: namespace, Name: name + "-agent",
			Spec: crewletaiv1alpha1.LLMProviderSpec{
				CompanyRef:          crewletaiv1alpha1.CompanyReference{Name: name},
				LLMProviderSettings: crewletaiv1alpha1.LLMProviderSettings{Type: crewletaiv1alpha1.LLMProviderCLIAgent, Model: cliModel},
				CLI: crewletaiv1alpha1.CLIAgentSettings{
					Agent: crewletaiv1alpha1.CLIAgentClaudeCode, Mode: crewletaiv1alpha1.CLIAgentModeAgent,
					Auth: crewletaiv1alpha1.CLIAgentAuth{TokenSecretRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-llm", Key: llmKey}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		onAgent := func(s *crewletaiv1alpha1.Seat) {
			s.Spec.LLM.Default = []crewletaiv1alpha1.LLMProviderReference{{Name: agent.Name}}
		}
		coder := newSeat(namespace, name, "coder", onAgent)

		By("applying the company with no public address, which every agent-mode run calls its seat's tools back through")
		standInAt(c, "")
		applied(c, crewletaiv1alpha1.ReasonApplied)
		active := runsRendered(e, c)
		Expect(string(active.Document)).To(ContainSubstring("handle: " + coder.Name))
		Expect(reasonOf(coder, crewletaiv1alpha1.ConditionApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonInRevision))
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonNotExposed))
		Expect(conditionOf(coder, crewletaiv1alpha1.ConditionReady)(Default).Message).To(ContainSubstring("has no public address"))
		Eventually(reasonOf(agent, crewletaiv1alpha1.ConditionAgentModeRunnable)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonNotExposed))
		Expect(conditionOf(agent, crewletaiv1alpha1.ConditionAgentModeRunnable)(Default).Status).To(Equal(metav1.ConditionFalse))
		Expect(reasonOf(agent, crewletaiv1alpha1.ConditionResolvedRefs)(Default)).To(Equal(crewletaiv1alpha1.ReasonResolved))
		Expect(reasonOf(agent, crewletaiv1alpha1.ConditionApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonInRevision))
		Expect(reasonOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionReady)(Default)).
			To(BeElementOf(crewletaiv1alpha1.ReasonLeaseHeld, crewletaiv1alpha1.ReasonUnplaced), "a seat on the text provider is not affected")
		Expect(log.of(coder.Name)).To(ContainElement(HavePrefix("Warning " + crewletaiv1alpha1.ReasonNotExposed)))
		Expect(log.of(agent.Name)).To(ContainElement(HavePrefix("Warning " + crewletaiv1alpha1.ReasonNotExposed)))

		By("adding a seat on the agent-mode provider in a seat pool, whose pods serve no tool bridge")
		bridged := newSeat(namespace, name, "bridged", onAgent, func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Placement = crewletaiv1alpha1.SeatPlacement{Pool: poolGeneral}
		})
		Eventually(reasonOf(bridged, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		Eventually(reasonOf(bridged, crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonAgentModeBridgeUnsupported))
		Expect(conditionOf(bridged, crewletaiv1alpha1.ConditionReady)(Default).Message).To(ContainSubstring("seat pool " + poolGeneral))
		applied(c, crewletaiv1alpha1.ReasonApplied)
		runsRendered(e, c)
		Expect(log.of(bridged.Name)).To(ConsistOf(HavePrefix("Warning " + crewletaiv1alpha1.ReasonAgentModeBridgeUnsupported)))

		By("giving the company a public address")
		standInAt(c, baseURL)
		Eventually(reasonOf(agent, crewletaiv1alpha1.ConditionAgentModeRunnable)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonToolBridgeServed))
		Expect(conditionOf(agent, crewletaiv1alpha1.ConditionAgentModeRunnable)(Default).Message).To(ContainSubstring(baseURL + "/mcp/"))
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).
			Should(BeElementOf(crewletaiv1alpha1.ReasonLeaseHeld, crewletaiv1alpha1.ReasonUnplaced))
		Expect(reasonOf(bridged, crewletaiv1alpha1.ConditionReady)(Default)).To(Equal(crewletaiv1alpha1.ReasonAgentModeBridgeUnsupported),
			"a public address reaches the data pods alone")
		applied(c, crewletaiv1alpha1.ReasonApplied)
		runsRendered(e, c)
		Expect(log.of(bridged.Name)).To(HaveLen(1), "the seat that stayed unrunnable is announced once")
		Expect(log.of(coder.Name)).To(ConsistOf(HavePrefix("Warning "+crewletaiv1alpha1.ReasonNotExposed)),
			"the seat is announced once, however many reconciles found it unrunnable")
		Expect(log.of(agent.Name)).To(ConsistOf(HavePrefix("Warning "+crewletaiv1alpha1.ReasonNotExposed)),
			"the provider is announced once, however many reconciles found it unrunnable")
	})

	It("records where the revision the engine runs pins the seats, for the runtime to keep their pods", func() {
		const name = "pinned"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})

		pools := class.DeepCopy()
		pools.ResourceVersion = ""
		pools.Name = namespace + "-pinned"
		pools.Spec.SeatPools = crewletaiv1alpha1.SeatPoolSettings{Policy: crewletaiv1alpha1.SeatPoolsAllowed, AllowPerSeat: new(true), MaxPods: 20}
		Expect(k8sClient.Create(ctx, pools)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, pools)).To(Succeed()) })
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce)
		updateLive(c, func(c *crewletaiv1alpha1.Company) {
			c.Spec.EngineClassName = pools.Name
			c.Spec.Topology.SeatPools = []crewletaiv1alpha1.SeatPool{
				{Name: poolGeneral, Mode: crewletaiv1alpha1.SeatPoolShared},
				{Name: poolIsolated, Mode: crewletaiv1alpha1.SeatPoolPerSeat},
			}
		})
		placed := func(pool string) func(*crewletaiv1alpha1.Seat) {
			return func(s *crewletaiv1alpha1.Seat) { s.Spec.Placement = crewletaiv1alpha1.SeatPlacement{Pool: pool} }
		}
		newSeat(namespace, name, "ana")
		newSeat(namespace, name, "bo", placed(poolGeneral))
		cy := newSeat(namespace, name, "cy", placed(poolIsolated))
		// Each document the engine is sent must find its pins in the
		// record already: written after the apply, the record would leave a
		// moment when the runtime sees a pool the engine is about to need
		// pinned by nothing, and deletes its pods.
		var (
			mu         sync.Mutex
			writes     int
			unrecorded []string
		)
		// The key, not c: the hook runs on the reconciler's goroutine while
		// the spec reads c over again.
		key := client.ObjectKeyFromObject(c)
		engines.watchApplies(key, func(document []byte) {
			mu.Lock()
			defer mu.Unlock()
			writes++
			pins, err := render.PlacementOf(document)
			if err != nil {
				unrecorded = append(unrecorded, err.Error())
				return
			}
			held := &crewletaiv1alpha1.Company{}
			if err := k8sClient.Get(ctx, key, held); err != nil {
				unrecorded = append(unrecorded, err.Error())
				return
			}
			recorded := held.Status.Config.Placement
			for _, pool := range pins.Pools {
				if !slices.Contains(recorded.Pools, pool) {
					unrecorded = append(unrecorded, "pool "+pool)
				}
			}
			for _, seat := range pins.Seats {
				if !slices.Contains(recorded.Seats, seat) {
					unrecorded = append(unrecorded, "seat "+seat)
				}
			}
		})
		DeferCleanup(func() {
			mu.Lock()
			defer mu.Unlock()
			Expect(writes).To(BeNumerically(">=", 2), "the spec writes two revisions")
			Expect(unrecorded).To(BeEmpty(), "pins the record did not hold when the engine was sent the document that pins them")
		})
		standIn(c)
		placement := func(g Gomega) crewletaiv1alpha1.RevisionPlacement {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(c), c)).To(Succeed())
			return c.Status.Config.Placement
		}

		By("recording each pool a seat runs in, and each seat of a PerSeat pool")
		applied(c, crewletaiv1alpha1.ReasonApplied)
		Eventually(placement).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.RevisionPlacement{
			Pools: []string{crewletaiv1alpha1.DataPool, poolGeneral}, Seats: []string{name + "-cy"},
		}))

		By("keeping a moving seat pinned to its own pods while its new pool has no ready pod")
		updateLive(cy, func(s *crewletaiv1alpha1.Seat) { s.Spec.Placement.Pool = poolGeneral })
		Eventually(reasonOf(cy, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonPlacementPending))
		Expect(conditionOf(cy, crewletaiv1alpha1.ConditionApplied)(Default).Message).To(ContainSubstring(sharedPoolStatefulSetName(c, poolGeneral)))
		Consistently(placement).WithTimeout(3 * time.Second).Should(Equal(crewletaiv1alpha1.RevisionPlacement{
			Pools: []string{crewletaiv1alpha1.DataPool, poolGeneral}, Seats: []string{name + "-cy"},
		}))

		By("dropping a seat's own pods from the record once a revision that places it elsewhere is applied")
		readyStatefulSet(c, sharedPoolStatefulSetName(c, poolGeneral))
		Eventually(placement).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.RevisionPlacement{
			Pools: []string{crewletaiv1alpha1.DataPool, poolGeneral},
		}))
		Eventually(reasonOf(cy, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		active, err := e.Client().ActiveConfig(ctx)
		Expect(err).NotTo(HaveOccurred())
		pins, err := render.PlacementOf(active.Document)
		Expect(err).NotTo(HaveOccurred())
		Expect(pins).To(Equal(render.Placement{Pools: []string{crewletaiv1alpha1.DataPool, poolGeneral}}), "the record is not what the engine runs")

		By("keeping a seat moved to a pool the company does not declare where it runs")
		updateLive(cy, func(s *crewletaiv1alpha1.Seat) { s.Spec.Placement.Pool = nowhere })
		Eventually(reasonOf(cy, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonPlacementPending))
		Expect(conditionOf(cy, crewletaiv1alpha1.ConditionApplied)(Default).Message).
			To(ContainSubstring("pool nowhere, which Company " + name + " does not declare (see ResolvedRefs)"))
		Expect(reasonOf(cy, crewletaiv1alpha1.ConditionResolvedRefs)(Default)).To(Equal(crewletaiv1alpha1.ReasonPoolNotFound))
		Consistently(placement).WithTimeout(3 * time.Second).Should(Equal(crewletaiv1alpha1.RevisionPlacement{
			Pools: []string{crewletaiv1alpha1.DataPool, poolGeneral},
		}))
	})

	It("reports the engine node that holds each agent seat, and a seat no node may run as unplaced", func() {
		const name = "placed"
		node := name + "-data-0"
		// The engine as the company's data pod runs it: named after the pod,
		// labelled for the seats placed on the data pods.
		startEngine(name, enginetest.WithNode(node, map[string]string{render.PoolLabel: crewletaiv1alpha1.DataPool}))
		secret(name+"-llm", map[string]string{llmKey: "k"})

		pools := class.DeepCopy()
		pools.ResourceVersion = ""
		pools.Name = namespace + "-placed"
		pools.Spec.SeatPools = crewletaiv1alpha1.SeatPoolSettings{Policy: crewletaiv1alpha1.SeatPoolsAllowed, MaxPods: 20}
		Expect(k8sClient.Create(ctx, pools)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, pools)).To(Succeed()) })
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce)
		updateLive(c, func(c *crewletaiv1alpha1.Company) {
			c.Spec.EngineClassName = pools.Name
			c.Spec.Topology.SeatPools = []crewletaiv1alpha1.SeatPool{{Name: poolGeneral, Mode: crewletaiv1alpha1.SeatPoolShared}}
		})
		ana := newSeat(namespace, name, "ana")
		bo := newSeat(namespace, name, "bo", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Placement = crewletaiv1alpha1.SeatPlacement{Pool: poolGeneral}
		})
		jane := &crewletaiv1alpha1.Seat{
			Namespace: namespace, Name: name + "-jane",
			Spec: crewletaiv1alpha1.SeatSpec{
				CompanyRef: crewletaiv1alpha1.CompanyReference{Name: name},
				Kind:       crewletaiv1alpha1.SeatKindHuman,
				Contact:    crewletaiv1alpha1.SeatContact{GitHubLogin: name + "-jane"},
			},
		}
		Expect(k8sClient.Create(ctx, jane)).To(Succeed())
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)

		By("naming the data pod that holds the seat placed on the data pods")
		Eventually(reasonOf(ana, crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonLeaseHeld))
		Expect(ana.Status.Placement.Node).To(Equal(node))
		Expect(conditionOf(ana, crewletaiv1alpha1.ConditionReady)(Default).Message).To(ContainSubstring("Engine node " + node + " holds the seat"))

		By("reporting the seat of a pool no engine node runs as unplaced, with no node")
		Eventually(reasonOf(bo, crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonUnplaced))
		Expect(conditionOf(bo, crewletaiv1alpha1.ConditionReady)(Default).Message).To(ContainSubstring(sharedPoolStatefulSetName(c, poolGeneral)))
		Expect(bo.Status.Placement.Node).To(BeEmpty())

		By("giving a human seat, which the engine does not lease, no Ready")
		Eventually(reasonOf(jane, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		Consistently(func(g Gomega) *metav1.Condition {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(jane), jane)).To(Succeed())
			return meta.FindStatusCondition(jane.Status.Conditions, crewletaiv1alpha1.ConditionReady)
		}).WithTimeout(3 * time.Second).Should(BeNil())

		By("reporting every agent seat of a suspended company as not running")
		updateLive(c, func(c *crewletaiv1alpha1.Company) { c.Spec.Suspend = new(true) })
		Eventually(reasonOf(ana, crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonSuspended))
		Expect(ana.Status.Placement.Node).To(BeEmpty())
	})

	It("refuses two Seats of one handle on both, and gives neither the handle's identity", func() {
		const name = "twins"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce)
		first := newSeat(namespace, name, "first", func(s *crewletaiv1alpha1.Seat) { s.Spec.Handle = "twin" })
		second := newSeat(namespace, name, "second", func(s *crewletaiv1alpha1.Seat) { s.Spec.Handle = "twin" })
		standIn(c)
		for _, s := range []*crewletaiv1alpha1.Seat{first, second} {
			Eventually(reasonOf(s, crewletaiv1alpha1.ConditionAccepted)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonConflict))
			Expect(conditionOf(s, crewletaiv1alpha1.ConditionAccepted)(Default).Message).To(ContainSubstring(`shares the handle "twin"`))
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(s), s)).To(Succeed())
			Expect(s.Status.SeatID).To(BeEmpty(), "Seat %s was given the identity of a handle it shares", s.Name)
		}
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonConflict))
		_, err := e.Client().ActiveConfig(ctx)
		Expect(err).To(MatchError(engine.ErrNoActiveRevision))
	})

	It("applies the document through an engine that names the operator its only writer, which refuses every other token", func() {
		const name = "managed"
		e := startEngine(name, enginetest.WithCompanyWriters(enginetest.OperatorTokenID))
		secret(name+"-llm", map[string]string{llmKey: "k1"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)
		Expect(reasonOf(c, crewletaiv1alpha1.ConditionSecretsSynced)(Default)).To(Equal(crewletaiv1alpha1.ReasonSynced))
		first := runsRendered(e, c)

		By("refusing a person's edit, which therefore never has to be overwritten")
		access := engine.New(e.URL, e.AccessToken)
		edited := regexp.MustCompile(`goal: .*`).ReplaceAll(first.Document, []byte("goal: A person's goal"))
		_, err := access.ApplyConfig(ctx, engine.ApplyRequest{Document: edited, Summary: "a person's edit", IfMatch: first.RevisionID})
		Expect(err).To(MatchError(engine.ErrConfigManaged))

		By("applying the operator's own next revision")
		newSeat(namespace, name, "bo")
		Eventually(reasonOf(seat(name+"-bo"), crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		Expect(revisions(e)).To(HaveLen(2))
		for _, r := range revisions(e) {
			Expect(r.CreatedBy).To(Equal(engine.TokenLogin(enginetest.OperatorTokenID)))
		}
		runsRendered(e, c)
	})

	It("reports an engine that refuses the operator's token a write as WriteRefused, and writes nothing", func() {
		const name = "foreign-writer"
		e := startEngine(name, enginetest.WithCompanyWriters(enginetest.AccessTokenID))
		secret(name+"-llm", map[string]string{llmKey: "k1"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonWriteRefused)
		got := conditionOf(c, crewletaiv1alpha1.ConditionConfigApplied)(Default)
		Expect(got.Status).To(Equal(metav1.ConditionFalse))
		Expect(got.Message).To(ContainSubstring("api.auth.company_writers names " + enginetest.AccessTokenID))
		Expect(got.Message).To(ContainSubstring(name + "-bootstrap"))
		Expect(reasonOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonBlocked))
		Expect(log.of(name)).To(ContainElement(HavePrefix("Warning WriteRefused")))
		_, err := e.Client().ActiveConfig(ctx)
		Expect(err).To(MatchError(engine.ErrNoActiveRevision))
	})

	It("reports a document that takes a human seat a person holds as SeatHeld, and keeps the revision the engine runs", func() {
		const name = "held"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k1"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		founder := newSeat(namespace, name, "founder", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Kind, s.Spec.Goal = crewletaiv1alpha1.SeatKindHuman, ""
		})
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)
		good := runsRendered(e, c)
		// A person on the seat, as an admin of the company puts one there;
		// the address is theirs, and no status may carry it.
		const address = "founder@held.example"
		e.AddPerson(GinkgoT(), address, founder.Name)
		heldBy := founder.Name + " (held by a person, active)"

		By("making the founder's seat an agent's")
		updateLive(founder, func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Kind, s.Spec.Goal = crewletaiv1alpha1.SeatKindAgent, "Found the company."
		})
		Eventually(reasonOf(founder, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonSeatHeld))
		Expect(conditionOf(founder, crewletaiv1alpha1.ConditionApplied)(Default).Message).To(ContainSubstring("a person, active"))
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonSeatHeld))
		got := conditionOf(c, crewletaiv1alpha1.ConditionConfigApplied)(Default)
		Expect(got.Message).To(ContainSubstring(heldBy))
		Expect(got.Message).To(ContainSubstring("a person is moved to another human seat or removed"))
		Expect(got.Message).NotTo(ContainSubstring(address))
		Expect(reasonOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonInRevision),
			"ana's generation still runs; only the change to the founder's seat is held back")
		Expect(log.of(name)).To(ContainElement(HavePrefix("Warning SeatHeld")))
		active, err := e.Client().ActiveConfig(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(active.RevisionID).To(Equal(good.RevisionID))

		By("making it Human again")
		updateLive(founder, func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Kind, s.Spec.Goal = crewletaiv1alpha1.SeatKindHuman, ""
		})
		Eventually(func(g Gomega) metav1.ConditionStatus {
			return conditionOf(c, crewletaiv1alpha1.ConditionConfigApplied)(g).Status
		}).WithTimeout(settle).Should(Equal(metav1.ConditionTrue))

		By("deleting the founder's Seat")
		Expect(k8sClient.Delete(ctx, founder)).To(Succeed())
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonSeatHeld))
		Expect(conditionOf(c, crewletaiv1alpha1.ConditionConfigApplied)(Default).Message).To(ContainSubstring(heldBy))
		active, err = e.Client().ActiveConfig(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(active.RevisionID).To(Equal(good.RevisionID))
		Expect(revisions(e)).To(HaveLen(1))

		By("restoring it")
		newSeat(namespace, name, "founder", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Kind, s.Spec.Goal = crewletaiv1alpha1.SeatKindHuman, ""
		})
		Eventually(func(g Gomega) metav1.ConditionStatus {
			return conditionOf(c, crewletaiv1alpha1.ConditionConfigApplied)(g).Status
		}).WithTimeout(settle).Should(Equal(metav1.ConditionTrue))
		runsRendered(e, c)
	})

	It("reports the parts of a Company that is not accepted as not accepted either", func() {
		c := company("refused", crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		Eventually(func(g Gomega) {
			got := &crewletaiv1alpha1.Company{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(c), got)).To(Succeed())
			base := got.DeepCopy()
			meta.SetStatusCondition(&got.Status.Conditions, metav1.Condition{
				Type: crewletaiv1alpha1.ConditionAccepted, Status: metav1.ConditionFalse, Reason: crewletaiv1alpha1.ReasonLimitExceeded,
				Message: "The Company has 3 Seats; EngineClass team allows 2.", ObservedGeneration: got.Generation,
			})
			g.Expect(k8sClient.Status().Patch(ctx, got, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))).To(Succeed())
		}).WithTimeout(settle).Should(Succeed())
		ana := seat("refused-ana")
		Eventually(func(g Gomega) string { return conditionOf(ana, crewletaiv1alpha1.ConditionAccepted)(g).Message }).
			WithTimeout(settle).Should(ContainSubstring(crewletaiv1alpha1.ReasonLimitExceeded))
		accepted := conditionOf(ana, crewletaiv1alpha1.ConditionAccepted)(Default)
		Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
		Expect(accepted.Reason).To(Equal(crewletaiv1alpha1.ReasonCompanyNotAccepted))
	})

	It("reports the parts of a Company that does not exist", func() {
		orphan := newSeat(namespace, "no-such-company", "orphan")
		Eventually(reasonOf(orphan, crewletaiv1alpha1.ConditionAccepted)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonCompanyNotFound))
		Expect(conditionOf(orphan, crewletaiv1alpha1.ConditionAccepted)(Default).Status).To(Equal(metav1.ConditionFalse))
	})
})

// newSeat creates an agent Seat of a company, named <company>-<name> so that
// the specs' companies, which share a namespace, never share a Seat.
func newSeat(namespace, company, name string, mutate ...func(*crewletaiv1alpha1.Seat)) *crewletaiv1alpha1.Seat {
	GinkgoHelper()
	s := &crewletaiv1alpha1.Seat{
		Namespace: namespace, Name: company + "-" + name,
		Spec: crewletaiv1alpha1.SeatSpec{
			CompanyRef: crewletaiv1alpha1.CompanyReference{Name: company},
			Goal:       "Answer every question " + name + " is asked.",
		},
	}
	for _, m := range mutate {
		m(s)
	}
	Expect(k8sClient.Create(ctx, s)).To(Succeed())
	return s
}

// editThroughEngine changes the first seat's goal in the engine's document
// as the access token, the way a person in the dashboard would.
func editThroughEngine(e *enginetest.Engine, goal string) {
	GinkgoHelper()
	accessClient := engine.New(e.URL, e.AccessToken)
	active, err := accessClient.ActiveConfig(ctx)
	Expect(err).NotTo(HaveOccurred())
	edited := regexp.MustCompile(`goal: .*`).ReplaceAll(active.Document, []byte(goal))
	Expect(edited).NotTo(Equal(active.Document))
	_, err = accessClient.ApplyConfig(ctx, engine.ApplyRequest{Document: edited, Summary: "a person's edit", IfMatch: active.RevisionID})
	Expect(err).NotTo(HaveOccurred())
}

// conditionsOfObject returns any crewlet.ai object's status conditions.
func conditionsOfObject(obj client.Object) []metav1.Condition {
	switch o := obj.(type) {
	case *crewletaiv1alpha1.Company:
		return o.Status.Conditions
	default:
		return *newPart(obj).conditions()
	}
}

// listOf lists the parts of a company of one kind.
func listOf[T any](list client.ObjectList, namespace, company string) []T {
	GinkgoHelper()
	Expect(k8sClient.List(ctx, list, client.InNamespace(namespace))).To(Succeed())
	var out []T
	for _, obj := range itemsOf(list) {
		if companyRefOf(obj) == company {
			out = append(out, *any(obj).(*T))
		}
	}
	return out
}

// sampleParts decodes the Nimbus Company and its parts from config/samples,
// in the namespace given, and its EngineClass.
func sampleParts(namespace string) ([]client.Object, *crewletaiv1alpha1.EngineClass) {
	GinkgoHelper()
	objects, err := decodeSamples(namespace)
	Expect(err).NotTo(HaveOccurred())
	var parts []client.Object
	var class *crewletaiv1alpha1.EngineClass
	for _, obj := range objects {
		if c, ok := obj.(*crewletaiv1alpha1.EngineClass); ok {
			class = c
			continue
		}
		parts = append(parts, obj)
	}
	Expect(class).NotTo(BeNil())
	return parts, class
}

// readyStatefulSet stands in for the runtime reconciler and the StatefulSet
// controller: a seat StatefulSet of the Company's, of that name, labelled as
// the operator labels it (its cache holds no other), with a ready pod.
func readyStatefulSet(c *crewletaiv1alpha1.Company, name string) {
	GinkgoHelper()
	labels := selectorFor(c, componentSeats)
	sts := &appsv1.StatefulSet{
		Namespace: c.Namespace, Name: name, Labels: labelsFor(c, componentSeats),
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: crewletaiv1alpha1.GroupVersion.String(), Kind: render.KindCompany, Name: c.Name, UID: c.UID,
			Controller: new(true), BlockOwnerDeletion: new(true),
		}},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				Labels: labels,
				Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: engineContainer, Image: "ghcr.io/crewlet/crewlet:v0.1.0"}}},
			},
		},
	}
	Expect(k8sClient.Create(ctx, sts)).To(Succeed())
	sts.Status = appsv1.StatefulSetStatus{Replicas: 1, ReadyReplicas: 1}
	Expect(k8sClient.Status().Update(ctx, sts)).To(Succeed())
}
