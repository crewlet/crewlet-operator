package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
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

// belowExecutorFloor is a Seat's token ceiling below the room Crewlet's
// executor launch needs (turn_engine.executor_min_budget_tokens, 25000 by
// default), a refusal only the engine's dry run makes: the API admits any
// ceiling of a token or more, and the operator does not read the floor.
var belowExecutorFloor = crewletaiv1alpha1.TokenBudget{Day: 1000}

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

// envDumpArgs are the arguments of a stdio tool server, run by /bin/sh, that
// writes the environment it is started with to path, by a rename, and never
// answers, so the engine gives it up after its one-second startup timeout and
// starts it again later: each start is handed what the engine resolved when
// the revision it runs activated, which envDump reads back.
func envDumpArgs(path string) []string {
	return []string{"-c", `env > "$0.part" && mv "$0.part" "$0"; exec cat >/dev/null`, path}
}

// envDump is the environment the server of envDumpArgs was last started with,
// nil before its first start.
func envDump(path string) map[string]string {
	GinkgoHelper()
	data, err := os.ReadFile(path) //nolint:gosec // G304: the file the test's own server writes in its temporary directory.
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	Expect(err).NotTo(HaveOccurred())
	vars := map[string]string{}
	for line := range strings.Lines(string(data)) {
		k, v, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "=")
		vars[k] = v
	}
	return vars
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

	// company creates a small company: one Claude Code provider whose key is
	// in the Secret <name>-llm, the Seats named, in the root of the chart,
	// and their executors placed Direct.
	company := func(name string, policy crewletaiv1alpha1.ManagementPolicy, seats ...string) *crewletaiv1alpha1.Company {
		GinkgoHelper()
		c := &crewletaiv1alpha1.Company{
			Namespace: namespace, Name: name,
			Spec: crewletaiv1alpha1.CompanySpec{
				CompanyName: "Acme " + name, EngineClassName: class.Name,
				Config: crewletaiv1alpha1.ConfigSettings{ManagementPolicy: policy},
				// Every agent seat's executor runs in a box: Direct, on
				// the data pod, where the test's engine runs no CLI.
				Sandbox: crewletaiv1alpha1.SandboxSettings{DefaultRunIn: crewletaiv1alpha1.SandboxPlacementDirect},
			},
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		Expect(k8sClient.Create(ctx, &crewletaiv1alpha1.LLMProvider{
			Namespace: namespace, Name: name + "-llm",
			Spec: crewletaiv1alpha1.LLMProviderSpec{
				CompanyRef: crewletaiv1alpha1.CompanyReference{Name: name},
				// Claude Code on a key that signs nothing in, which the
				// test's engine is not given a binary of: no seat ever
				// reaches a model.
				LLMProviderSettings: crewletaiv1alpha1.LLMProviderSettings{
					Agent: crewletaiv1alpha1.CLIAgentClaudeCode, Model: cliModel,
				},
				Auth: crewletaiv1alpha1.CLIAgentAuth{
					Mode:            crewletaiv1alpha1.CLIAgentAuthAPIKey,
					APIKeySecretRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-llm", Key: llmKey},
				},
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
			Expect(listed[storeName].OperatorID).To(Equal(engine.TokenLogin(render.TokenOperator)))
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

	It("writes back a value somebody else replaced in the engine's store, says who, and has the engine read it again", func() {
		const name = "overwrite"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "ours"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)
		storeName := secretsync.StoreName(secretref.Key{Namespace: namespace, Name: name + "-llm", Key: llmKey})
		Expect(revisions(e)).To(HaveLen(1))

		By("replacing the value with the access token, which holds the operator's grant")
		Expect(engine.New(e.URL, e.AccessToken).PutSecret(ctx, storeName, []byte("theirs"))).To(Succeed())
		access := engine.TokenLogin(enginetest.AccessTokenID)

		Eventually(func() string { return storedValue(e, storeName) }).WithTimeout(settle).Should(Equal("ours"))
		Expect(storeListing(e)[storeName].OperatorID).To(Equal(engine.TokenLogin(enginetest.OperatorTokenID)))
		Eventually(func() []revision { return revisions(e) }).WithTimeout(settle).Should(HaveLen(2),
			"the engine reads the value written back only when a revision activates")
		Expect(revisions(e)[0].Summary).To(Equal("crewlet-operator: Secret " + namespace + "/" + name + "-llm"))
		Expect(log.of(name)).To(ContainElement(SatisfyAll(
			HavePrefix("Warning SecretOverwritten Wrote back 1 values somebody else had replaced"),
			ContainSubstring(storeName+" (written by "+access+", with "+access+")"),
		)))

		By("leaving the store alone once it holds the operator's value again")
		Consistently(func() []revision { return revisions(e) }).WithTimeout(3 * quietPeriod).Should(HaveLen(2))
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

	It("starts without an optional tool-server key while its Secret is missing, never serves a deleted one, and pushes it again", func() {
		const name, tavilyVar, registryVar = "optional", "TAVILY_API_KEY", "NPM_CONFIG_REGISTRY"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		secret(name+"-tavily", map[string]string{requiredKey: tavilyValue})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		env := filepath.Join(GinkgoT().TempDir(), "env")
		startedWith := func() map[string]string { return envDump(env) }
		server := &crewletaiv1alpha1.MCPServer{
			Namespace: namespace, Name: name + "-tavily",
			Spec: crewletaiv1alpha1.MCPServerSpec{
				CompanyRef:            crewletaiv1alpha1.CompanyReference{Name: name},
				Command:               serverShell,
				Args:                  envDumpArgs(env),
				StartupTimeoutSeconds: 1,
				Env: []crewletaiv1alpha1.MCPEnvVar{
					{Name: tavilyVar, ValueFrom: crewletaiv1alpha1.SecretValueSource{
						SecretKeyRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-tavily", Key: requiredKey},
					}},
					{Name: registryVar, ValueFrom: crewletaiv1alpha1.SecretValueSource{
						SecretKeyRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-npm", Key: optionalKey},
					}},
				},
				OptionalKeys: []string{registryVar},
			},
		}
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)
		registry := secretsync.StoreName(secretref.Key{Namespace: namespace, Name: name + "-npm", Key: optionalKey})
		gone := func() map[string]engine.SecretInfo { return storeListing(e) }
		Expect(gone()).NotTo(HaveKey(registry), "nothing is pushed for a Secret that does not exist")
		resolved := conditionOf(server, crewletaiv1alpha1.ConditionResolvedRefs)(Default)
		Expect(resolved.Status).To(Equal(metav1.ConditionTrue))
		Expect(resolved.Message).To(ContainSubstring("spec.env[1].valueFrom (NPM_CONFIG_REGISTRY): Secret " + namespace + "/" + name + "-npm does not exist"))
		Expect(conditionOf(c, crewletaiv1alpha1.ConditionSecretsSynced)(Default).Message).To(ContainSubstring("It holds none for the 1 it lets go without"))
		Expect(reasonOf(server, crewletaiv1alpha1.ConditionApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonInRevision))
		runsRendered(e, c)
		Expect(revisions(e)).To(HaveLen(1))
		Eventually(startedWith).WithTimeout(settle).Should(HaveKeyWithValue(tavilyVar, tavilyValue))
		Expect(startedWith()).NotTo(HaveKey(registryVar), "the engine started the server with a value nothing holds")

		By("creating the Secret")
		npm := secret(name+"-npm", map[string]string{optionalKey: npmRegistry})
		Eventually(func() string { return storedValue(e, registry) }).WithTimeout(settle).Should(Equal(npmRegistry))
		Eventually(func() []revision { return revisions(e) }).WithTimeout(settle).Should(HaveLen(2),
			"the engine reads the value only when a revision activates")
		Eventually(func(g Gomega) string {
			return conditionOf(server, crewletaiv1alpha1.ConditionResolvedRefs)(g).Message
		}).WithTimeout(settle).Should(Equal("Every object and Secret key it references exists."))
		runsRendered(e, c)
		Eventually(startedWith).WithTimeout(settle).Should(HaveKeyWithValue(registryVar, npmRegistry))

		By("deleting it")
		Expect(k8sClient.Delete(ctx, npm)).To(Succeed())
		Eventually(gone).WithTimeout(settle).ShouldNot(HaveKey(registry),
			"the engine would start the server with the value of a Secret that is gone")
		Eventually(func() []revision { return revisions(e) }).WithTimeout(settle).Should(HaveLen(3),
			"the engine resolves the reference to nothing only when a revision activates")
		applied(c, crewletaiv1alpha1.ReasonApplied)
		Eventually(func(g Gomega) string {
			return conditionOf(server, crewletaiv1alpha1.ConditionResolvedRefs)(g).Message
		}).WithTimeout(settle).Should(ContainSubstring("Secret " + namespace + "/" + name + "-npm does not exist"))
		Eventually(startedWith).WithTimeout(settle).ShouldNot(HaveKey(registryVar),
			"the engine starts the server with the value of a Secret that is gone")

		By("creating it again, with another value")
		secret(name+"-npm", map[string]string{optionalKey: "https://mirror.example.com"})
		Eventually(func() string { return storedValue(e, registry) }).WithTimeout(settle).Should(Equal("https://mirror.example.com"))
		Eventually(func() []revision { return revisions(e) }).WithTimeout(settle).Should(HaveLen(4))
		runsRendered(e, c)
		Eventually(startedWith).WithTimeout(settle).Should(HaveKeyWithValue(registryVar, "https://mirror.example.com"))

		By("emptying the required key")
		updateLive(&corev1.Secret{Namespace: namespace, Name: name + "-tavily"}, func(s *corev1.Secret) {
			s.Data = map[string][]byte{requiredKey: {}}
		})
		Eventually(reasonOf(server, crewletaiv1alpha1.ConditionResolvedRefs)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonSecretValueEmpty))
		Expect(conditionOf(server, crewletaiv1alpha1.ConditionResolvedRefs)(Default).Message).
			To(ContainSubstring(`holds an empty value under "` + requiredKey + `", given as ` + tavilyVar))
		Eventually(reasonOf(server, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision),
			"an empty value holds nothing back: the engine applies the document and leaves the server unstarted")
		applied(c, crewletaiv1alpha1.ReasonApplied)
		Eventually(func() []string { return log.of(server.Name) }).WithTimeout(settle).
			Should(ContainElement(HavePrefix("Warning " + crewletaiv1alpha1.ReasonSecretValueEmpty)))
	})

	It("starts nothing with a deleted optional Secret's value while the document is held", func() {
		const name, tavilyVar, registryVar = "optheld", "TAVILY_API_KEY", "NPM_CONFIG_REGISTRY"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		// One Secret for the vendor, holding the server's key, which it is
		// not started without, and the registry, which it may go without.
		both := name + "-npm"
		secret(both, map[string]string{requiredKey: tavilyValue, optionalKey: npmRegistry})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		env := filepath.Join(GinkgoT().TempDir(), "env")
		startedWith := func() map[string]string { return envDump(env) }
		from := func(key string) crewletaiv1alpha1.SecretValueSource {
			return crewletaiv1alpha1.SecretValueSource{SecretKeyRef: crewletaiv1alpha1.SecretKeyReference{Name: both, Key: key}}
		}
		server := &crewletaiv1alpha1.MCPServer{
			Namespace: namespace, Name: name + "-tavily",
			Spec: crewletaiv1alpha1.MCPServerSpec{
				CompanyRef: crewletaiv1alpha1.CompanyReference{Name: name},
				Command:    serverShell, Args: envDumpArgs(env), StartupTimeoutSeconds: 1,
				Env: []crewletaiv1alpha1.MCPEnvVar{
					{Name: tavilyVar, ValueFrom: from(requiredKey)},
					{Name: registryVar, ValueFrom: from(optionalKey)},
				},
				OptionalKeys: []string{registryVar},
			},
		}
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		// A second server reading the registry alone, and letting it go.
		mirror := &crewletaiv1alpha1.MCPServer{
			Namespace: namespace, Name: name + "-mirror",
			Spec: crewletaiv1alpha1.MCPServerSpec{
				CompanyRef: crewletaiv1alpha1.CompanyReference{Name: name},
				Command:    serverShell, Args: []string{"-c", "exec cat >/dev/null"}, StartupTimeoutSeconds: 1,
				Env:          []crewletaiv1alpha1.MCPEnvVar{{Name: registryVar, ValueFrom: from(optionalKey)}},
				OptionalKeys: []string{registryVar},
			},
		}
		Expect(k8sClient.Create(ctx, mirror)).To(Succeed())
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)
		registry := secretsync.StoreName(secretref.Key{Namespace: namespace, Name: both, Key: optionalKey})
		Eventually(startedWith).WithTimeout(settle).Should(HaveKeyWithValue(registryVar, npmRegistry))
		Expect(startedWith()).To(HaveKeyWithValue(tavilyVar, tavilyValue))
		count := func() int { return len(revisions(e)) }
		reactivated := func(before int) {
			GinkgoHelper()
			Eventually(func() map[string]engine.SecretInfo { return storeListing(e) }).WithTimeout(settle).ShouldNot(HaveKey(registry),
				"the engine's store keeps the value of a Secret that is gone")
			Eventually(count).WithTimeout(settle).Should(Equal(before+1),
				"the engine resolves the reference to nothing only when a revision activates")
			Expect(revisions(e)[0].Summary).To(ContainSubstring(registry))
			Eventually(startedWith).WithTimeout(settle).ShouldNot(HaveKey(registryVar),
				"the engine starts the server with the value of a Secret that is gone")
			Expect(startedWith()).To(HaveKeyWithValue(tavilyVar, tavilyValue),
				"the running revision keeps the value it requires until the next one is applied")
			Consistently(count).WithTimeout(6*time.Second).Should(Equal(before+1),
				"a revision that went without the value is activated again")
			Expect(log.of(name)).To(ContainElement(HavePrefix("Normal Reactivated ")))
			Expect(log.of(name)).NotTo(ContainElement(HavePrefix("Normal SecretKept ")),
				"the running revision, which the operator activated, lets the value go")
		}

		By("deleting the Secret, which the server's required key holds the document for")
		before := count()
		Expect(k8sClient.Delete(ctx, &corev1.Secret{Namespace: namespace, Name: both})).To(Succeed())
		applied(c, crewletaiv1alpha1.ReasonSecretNotFound)
		reactivated(before)
		Expect(reasonOf(server, crewletaiv1alpha1.ConditionResolvedRefs)(Default)).To(Equal(crewletaiv1alpha1.ReasonSecretNotFound))
		resolved := conditionOf(mirror, crewletaiv1alpha1.ConditionResolvedRefs)(Default)
		Expect(resolved.Status).To(Equal(metav1.ConditionTrue))
		Expect(resolved.Message).To(ContainSubstring("spec.env[0].valueFrom (NPM_CONFIG_REGISTRY): Secret "+namespace+"/"+both+
			" does not exist"), "a part that goes without the value is told it is there")

		By("creating it again")
		secret(both, map[string]string{requiredKey: tavilyValue, optionalKey: "https://mirror.example.com"})
		applied(c, crewletaiv1alpha1.ReasonApplied)
		Eventually(startedWith).WithTimeout(settle).Should(HaveKeyWithValue(registryVar, "https://mirror.example.com"))

		By("removing the optional key while another object holds the document")
		// A server whose optionalKeys names a key no value is written under,
		// which Crewlet refuses: the render holds the document for it.
		const seatToken = "SEAT_TOKEN"
		bad := &crewletaiv1alpha1.MCPServer{
			Namespace: namespace, Name: name + "-bad",
			Spec: crewletaiv1alpha1.MCPServerSpec{
				CompanyRef: crewletaiv1alpha1.CompanyReference{Name: name},
				Command:    serverShell, Args: []string{"-c", "exec cat >/dev/null"},
				OptionalKeys: []string{seatToken},
			},
		}
		Expect(k8sClient.Create(ctx, bad)).To(Succeed())
		applied(c, crewletaiv1alpha1.ReasonOptionalKeyUnused)
		Expect(reasonOf(bad, crewletaiv1alpha1.ConditionResolvedRefs)(Default)).To(Equal(crewletaiv1alpha1.ReasonOptionalKeyUnused))
		Expect(conditionOf(bad, crewletaiv1alpha1.ConditionResolvedRefs)(Default).Message).
			To(ContainSubstring("optionalKeys[0]: names " + seatToken + ", which no value is written under"))
		Eventually(reasonOf(bad, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonOptionalKeyUnused))
		before = count()
		updateLive(&corev1.Secret{Namespace: namespace, Name: both}, func(s *corev1.Secret) {
			s.Data = map[string][]byte{requiredKey: []byte(tavilyValue)}
		})
		reactivated(before)

		By("giving the server a Seat's value under its optional key")
		// The entry leaves something out once a value read from a Secret is
		// written under it, whether or not the Secret exists: one that does
		// not is left out. Applied, not DriftCorrected: the revision that
		// activated the running document again is the operator's own.
		updateLive(seat(name+"-ana"), func(s *crewletaiv1alpha1.Seat) {
			s.Spec.MCP = []crewletaiv1alpha1.MCPCredentials{{Name: bad.Name, Env: []crewletaiv1alpha1.MCPCredentialValue{{
				Name:      seatToken,
				ValueFrom: crewletaiv1alpha1.SeatCredentialSource{SecretKeyRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-ana", Key: seatTokenKey}},
			}}}}
		})
		applied(c, crewletaiv1alpha1.ReasonApplied)
		Eventually(reasonOf(bad, crewletaiv1alpha1.ConditionResolvedRefs)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonResolved))
		runsRendered(e, c)
		Expect(startedWith()).NotTo(HaveKey(registryVar))
	})

	// requiringServer starts the engine of a company whose one tool server
	// reads the registry from the Secret <name>-npm and requires it, applies
	// its document, and returns the server, the store name of the registry,
	// what the server was last started with, and the count of the engine's
	// revisions.
	requiringServer := func(name string) (*enginetest.Engine, *crewletaiv1alpha1.MCPServer, string, func() map[string]string, func() int) {
		GinkgoHelper()
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		secret(name+"-npm", map[string]string{optionalKey: npmRegistry})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		env := filepath.Join(GinkgoT().TempDir(), "env")
		server := &crewletaiv1alpha1.MCPServer{
			Namespace: namespace, Name: name + "-registry",
			Spec: crewletaiv1alpha1.MCPServerSpec{
				CompanyRef: crewletaiv1alpha1.CompanyReference{Name: name},
				Command:    serverShell, Args: envDumpArgs(env), StartupTimeoutSeconds: 1,
				Env: []crewletaiv1alpha1.MCPEnvVar{{Name: optionalRegistry, ValueFrom: crewletaiv1alpha1.SecretValueSource{
					SecretKeyRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-npm", Key: optionalKey},
				}}},
			},
		}
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)
		startedWith := func() map[string]string { return envDump(env) }
		Eventually(startedWith).WithTimeout(settle).Should(HaveKeyWithValue(optionalRegistry, npmRegistry))
		registry := secretsync.StoreName(secretref.Key{Namespace: namespace, Name: name + "-npm", Key: optionalKey})
		return e, server, registry, startedWith, func() int { return len(revisions(e)) }
	}
	// lettingGo lists the registry in the server's optionalKeys, and waits
	// until the reconcile has read it.
	lettingGo := func(server *crewletaiv1alpha1.MCPServer) {
		GinkgoHelper()
		updateLive(server, func(s *crewletaiv1alpha1.MCPServer) { s.Spec.OptionalKeys = []string{optionalRegistry} })
		Eventually(func(g Gomega) int64 {
			return conditionOf(server, crewletaiv1alpha1.ConditionResolvedRefs)(g).ObservedGeneration
		}).WithTimeout(settle).Should(Equal(server.Generation))
	}
	// keptFor asserts that the engine's store keeps the registry, whose
	// Secret is gone, for the running revision, which requires it, and
	// starts the server with it, activating nothing for it, and that the
	// Company and the server say so.
	keptFor := func(e *enginetest.Engine, name string, server *crewletaiv1alpha1.MCPServer, registry string,
		startedWith func() map[string]string, count func() int, before int,
	) {
		GinkgoHelper()
		c := &crewletaiv1alpha1.Company{Namespace: namespace, Name: name}
		Eventually(func() []string { return log.of(name) }).WithTimeout(settle).
			Should(ContainElement(HavePrefix("Normal SecretKept ")))
		Expect(log.of(name)).To(ContainElement(ContainSubstring(registry)))
		Eventually(func(g Gomega) string {
			return conditionOf(c, crewletaiv1alpha1.ConditionSecretsSynced)(g).Message
		}).WithTimeout(settle).Should(ContainSubstring("The engine's secret store keeps the value it held for the 1 the document " +
			"lets go without whose Secret or key no longer exists (" + namespace + "/" + name + "-npm[" + optionalKey + "]): " +
			"the revision the engine runs reads each as required"))
		Expect(conditionOf(c, crewletaiv1alpha1.ConditionSecretsSynced)(Default).Message).NotTo(ContainSubstring("holds none for"),
			"the store holds none of a value it keeps")
		Eventually(func(g Gomega) string {
			return conditionOf(server, crewletaiv1alpha1.ConditionResolvedRefs)(g).Message
		}).WithTimeout(settle).Should(ContainSubstring("Secret " + namespace + "/" + name + "-npm does not exist, " +
			"and its spec.optionalKeys lists " + optionalRegistry + "; the engine keeps the value it held for the revision it runs"))
		Consistently(func() string { return storedValue(e, registry) }).WithTimeout(6*time.Second).Should(Equal(npmRegistry),
			"the store took a value from the revision the engine runs, which requires it")
		Expect(count()).To(Equal(before), "a revision was activated without a value the running revision requires")
		Expect(startedWith()).To(HaveKeyWithValue(optionalRegistry, npmRegistry))
		Expect(log.of(name)).NotTo(ContainElement(HavePrefix("Normal Reactivated ")))
	}
	// letGoOnceItRuns asserts that once the next document runs, the registry
	// is gone from the store and the revision activated again without it,
	// once, and the server starts without it.
	letGoOnceItRuns := func(e *enginetest.Engine, name, registry string, startedWith func() map[string]string,
		count func() int, before int,
	) {
		GinkgoHelper()
		c := &crewletaiv1alpha1.Company{Namespace: namespace, Name: name}
		applied(c, crewletaiv1alpha1.ReasonApplied)
		Eventually(func() map[string]engine.SecretInfo { return storeListing(e) }).WithTimeout(settle).ShouldNot(HaveKey(registry),
			"the engine's store keeps the value of a Secret that is gone, which the revision it runs lets go")
		Eventually(count).WithTimeout(settle).Should(Equal(before+2),
			"the next document, and the same activated again without the value it started with")
		Expect(revisions(e)[0].Summary).To(ContainSubstring(registry))
		// The value goes, and the revision activates again without it, at
		// the operator's look after the one that applied the next document,
		// once the engine says it runs that one: the Reactivated event comes
		// just after the reload the count saw, and no condition waits for it.
		Eventually(func() []string { return log.of(name) }).WithTimeout(settle).
			Should(ContainElement(HavePrefix("Normal Reactivated ")))
		Eventually(startedWith).WithTimeout(settle).ShouldNot(HaveKey(optionalRegistry),
			"the engine starts the server with the value of a Secret that is gone")
		Consistently(count).WithTimeout(6 * time.Second).Should(Equal(before + 2))
		applied(c, crewletaiv1alpha1.ReasonApplied)
		runsRendered(e, c)
		Expect(conditionOf(c, crewletaiv1alpha1.ConditionSecretsSynced)(Default).Message).To(ContainSubstring("It holds none for the 1"))
	}

	It("keeps a value the running revision requires while a document that lets it go is held, and lets it go once that one runs", func() {
		const name = "optkept"
		e, server, registry, startedWith, count := requiringServer(name)

		By("holding the next document for another object's Secret, and letting the key go in it")
		Expect(k8sClient.Delete(ctx, &corev1.Secret{Namespace: namespace, Name: name + "-llm"})).To(Succeed())
		applied(&crewletaiv1alpha1.Company{Namespace: namespace, Name: name}, crewletaiv1alpha1.ReasonSecretNotFound)
		lettingGo(server)
		before := count()

		By("deleting the key's Secret")
		Expect(k8sClient.Delete(ctx, &corev1.Secret{Namespace: namespace, Name: name + "-npm"})).To(Succeed())
		keptFor(e, name, server, registry, startedWith, count, before)

		By("clearing the hold")
		secret(name+"-llm", map[string]string{llmKey: "k"})
		letGoOnceItRuns(e, name, registry, startedWith, count, before)
	})

	It("keeps a value the running revision requires while the engine refuses a document that lets it go, and lets it go once one runs", func() {
		const name = "optrefused"
		e, server, registry, startedWith, count := requiringServer(name)

		By("adding a Seat the engine refuses, and letting the key go")
		coder := newSeat(namespace, name, "coder", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.TokenBudget = belowExecutorFloor
		})
		c := &crewletaiv1alpha1.Company{Namespace: namespace, Name: name}
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInvalid))
		lettingGo(server)
		before := count()

		By("deleting the key's Secret")
		Expect(k8sClient.Delete(ctx, &corev1.Secret{Namespace: namespace, Name: name + "-npm"})).To(Succeed())
		keptFor(e, name, server, registry, startedWith, count, before)
		Expect(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonInvalid))

		By("taking the ceiling away again")
		updateLive(coder, func(s *crewletaiv1alpha1.Seat) { s.Spec.TokenBudget = crewletaiv1alpha1.TokenBudget{} })
		letGoOnceItRuns(e, name, registry, startedWith, count, before)
	})

	It("takes a deleted value out for a running revision that lets it go, while a document that requires it is held", func() {
		const name = "optreq"
		e, server, registry, startedWith, count := requiringServer(name)
		c := &crewletaiv1alpha1.Company{Namespace: namespace, Name: name}
		lettingGo(server)
		Eventually(count).WithTimeout(settle).Should(Equal(2))
		applied(c, crewletaiv1alpha1.ReasonApplied)

		By("holding the next document for another object's Secret, and requiring the key in it again")
		Expect(k8sClient.Delete(ctx, &corev1.Secret{Namespace: namespace, Name: name + "-llm"})).To(Succeed())
		applied(c, crewletaiv1alpha1.ReasonSecretNotFound)
		updateLive(server, func(s *crewletaiv1alpha1.MCPServer) { s.Spec.OptionalKeys = nil })
		Eventually(func(g Gomega) int64 {
			return conditionOf(server, crewletaiv1alpha1.ConditionResolvedRefs)(g).ObservedGeneration
		}).WithTimeout(settle).Should(Equal(server.Generation))
		Expect(count()).To(Equal(2))
		Eventually(startedWith).WithTimeout(settle).Should(HaveKeyWithValue(optionalRegistry, npmRegistry))

		By("deleting the key's Secret")
		Expect(k8sClient.Delete(ctx, &corev1.Secret{Namespace: namespace, Name: name + "-npm"})).To(Succeed())
		Eventually(reasonOf(server, crewletaiv1alpha1.ConditionResolvedRefs)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonSecretNotFound), "the next document requires the key, and is held for it")
		Eventually(func() map[string]engine.SecretInfo { return storeListing(e) }).WithTimeout(settle).ShouldNot(HaveKey(registry),
			"the store keeps the deleted Secret's value for the running revision, which lets it go")
		Eventually(count).WithTimeout(settle).Should(Equal(3), "the running revision was not activated again without the value")
		Expect(revisions(e)[0].Summary).To(ContainSubstring(registry))
		Eventually(startedWith).WithTimeout(settle).ShouldNot(HaveKey(optionalRegistry),
			"the engine starts the server with the value of a Secret that is gone")
		Consistently(count).WithTimeout(6 * time.Second).Should(Equal(3))
		Expect(log.of(name)).To(ContainElement(HavePrefix("Normal Reactivated ")))
		Expect(log.of(name)).NotTo(ContainElement(HavePrefix("Normal SecretKept ")))
		Expect(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonSecretNotFound))
	})

	It("lets a deleted value go in one activation when the engine runs the document it renders, whoever activated it", func() {
		const name = "optreload"
		e, server, registry, startedWith, count := requiringServer(name)
		c := &crewletaiv1alpha1.Company{Namespace: namespace, Name: name}
		lettingGo(server)
		Eventually(count).WithTimeout(settle).Should(Equal(2))
		applied(c, crewletaiv1alpha1.ReasonApplied)

		By("a person activating the running document again")
		_, err := engine.New(e.URL, e.AccessToken).ReloadConfig(ctx, "reloaded by a person")
		Expect(err).NotTo(HaveOccurred())
		Eventually(count).WithTimeout(settle).Should(Equal(3))
		Consistently(count).WithTimeout(3 * quietPeriod).Should(Equal(3))

		By("deleting the key's Secret")
		Expect(k8sClient.Delete(ctx, &corev1.Secret{Namespace: namespace, Name: name + "-npm"})).To(Succeed())
		Eventually(func() map[string]engine.SecretInfo { return storeListing(e) }).WithTimeout(settle).ShouldNot(HaveKey(registry))
		Eventually(count).WithTimeout(settle).Should(Equal(4))
		Consistently(count).WithTimeout(6*time.Second).Should(Equal(4),
			"a revision was activated with the deleted Secret's value in the store, and activated again")
		Eventually(startedWith).WithTimeout(settle).ShouldNot(HaveKey(optionalRegistry))
		Expect(log.of(name)).NotTo(ContainElement(HavePrefix("Normal Reactivated ")))
		Expect(log.of(name)).NotTo(ContainElement(HavePrefix("Normal SecretKept ")))
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

		By("adding a Seat whose token ceiling is below the room an executor launch needs: only the engine's dry run sees the two together")
		coder := newSeat(namespace, name, "coder", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.TokenBudget = belowExecutorFloor
		})
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInvalid))
		Expect(conditionOf(coder, crewletaiv1alpha1.ConditionApplied)(Default).Message).To(ContainSubstring("turn_engine.executor_min_budget_tokens"))
		// The parts' status is written before the Company's.
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInvalid))
		Expect(reasonOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonInRevision),
			"ana's generation still runs; only the new Seat is held back")
		active, err := e.Client().ActiveConfig(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(active.RevisionID).To(Equal(good.RevisionID))
		Expect(revisions(e)).To(HaveLen(1))
		Expect(log.of(name)).To(ContainElement(HavePrefix("Warning Invalid")))

		By("taking the ceiling away again")
		updateLive(coder, func(s *crewletaiv1alpha1.Seat) { s.Spec.TokenBudget = crewletaiv1alpha1.TokenBudget{} })
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		applied(c, crewletaiv1alpha1.ReasonApplied)
		runsRendered(e, c)
	})

	It("reports a seat whose executor cannot run on its Ready, and holds a document Crewlet refuses for an executor on the object at fault", func() {
		const name = "executor"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k"})
		secret(name+"-e2b", map[string]string{llmKey: "k"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		e2b := crewletaiv1alpha1.E2BSandbox{APIKeySecretRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-e2b", Key: llmKey}}
		updateLive(c, func(c *crewletaiv1alpha1.Company) {
			c.Spec.Sandbox.E2B = e2b
			// The seats with no chain of their own run on the provider
			// whose runs go Direct, never on the ones below.
			c.Spec.LLM.Order = []crewletaiv1alpha1.LLMProviderReference{{Name: name + "-llm"}}
		})
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)

		By("adding a Seat whose executor runs ClaudeCode in E2B on a login a box there cannot carry")
		secret(name+"-bundle", map[string]string{llmKey: "bundle"})
		bundle := &crewletaiv1alpha1.LLMProvider{
			Namespace: namespace, Name: name + "-bundle",
			Spec: crewletaiv1alpha1.LLMProviderSpec{
				CompanyRef: crewletaiv1alpha1.CompanyReference{Name: name},
				LLMProviderSettings: crewletaiv1alpha1.LLMProviderSettings{
					Agent: crewletaiv1alpha1.CLIAgentClaudeCode, Model: cliModel,
				},
				RunIn: crewletaiv1alpha1.SandboxPlacementE2B,
				Auth: crewletaiv1alpha1.CLIAgentAuth{
					CredentialBundleSecretRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-bundle", Key: llmKey},
				},
			},
		}
		Expect(k8sClient.Create(ctx, bundle)).To(Succeed())
		coder := newSeat(namespace, name, "coder", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.LLM.Default = []crewletaiv1alpha1.LLMProviderReference{{Name: bundle.Name}}
		})
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonSandboxLoginNotPortable))
		ready := conditionOf(coder, crewletaiv1alpha1.ConditionReady)(Default)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.ObservedGeneration).To(Equal(coder.Generation))
		Expect(ready.Message).To(ContainSubstring("a credential bundle stays on the engine's data volume"))
		Expect(ready.Message).To(ContainSubstring("auth.tokenSecretRef, with auth mode APIKey, or with auth mode Env"))
		Expect(ready.Message).To(ContainSubstring("set LLMProvider " + bundle.Name + "'s spec.runIn to Direct"))

		By("applying the company with the seat all the same, as Crewlet applies it")
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		applied(c, crewletaiv1alpha1.ReasonApplied)
		Eventually(reasonOf(bundle, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		active := runsRendered(e, c)
		Expect(string(active.Document)).To(ContainSubstring("handle: " + coder.Name))
		Expect(reasonOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionReady)(Default)).
			To(BeElementOf(crewletaiv1alpha1.ReasonLeaseHeld, crewletaiv1alpha1.ReasonUnplaced), "a seat whose executor runs Direct is not affected")
		Expect(log.of(coder.Name)).To(ConsistOf(HavePrefix("Warning " + crewletaiv1alpha1.ReasonSandboxLoginNotPortable)))

		By("running its executor Direct, where the provider's login seeds the box")
		updateLive(bundle, func(p *crewletaiv1alpha1.LLMProvider) { p.Spec.RunIn = crewletaiv1alpha1.SandboxPlacementDirect })
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).
			Should(BeElementOf(crewletaiv1alpha1.ReasonLeaseHeld, crewletaiv1alpha1.ReasonUnplaced))
		applied(c, crewletaiv1alpha1.ReasonApplied)
		good := runsRendered(e, c)
		Expect(log.of(coder.Name)).To(HaveLen(1), "a seat that became runnable is not announced again")

		By("running the seat's executor on Codex alone, which Crewlet refuses on every write")
		Expect(k8sClient.Create(ctx, &crewletaiv1alpha1.LLMProvider{
			Namespace: namespace, Name: name + "-codex",
			Spec: crewletaiv1alpha1.LLMProviderSpec{
				CompanyRef: crewletaiv1alpha1.CompanyReference{Name: name},
				LLMProviderSettings: crewletaiv1alpha1.LLMProviderSettings{
					Agent: crewletaiv1alpha1.CLIAgentCodex, Model: "gpt-5",
				},
				Auth: crewletaiv1alpha1.CLIAgentAuth{
					Mode:            crewletaiv1alpha1.CLIAgentAuthAPIKey,
					APIKeySecretRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-llm", Key: llmKey},
				},
			},
		})).To(Succeed())
		updateLive(coder, func(s *crewletaiv1alpha1.Seat) {
			s.Spec.LLM.Default = []crewletaiv1alpha1.LLMProviderReference{{Name: name + "-codex"}}
		})
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonNoExecutorCLI))
		Expect(conditionOf(coder, crewletaiv1alpha1.ConditionApplied)(Default).Message).
			To(ContainSubstring("Spec.llm.default: is LLMProvider " + name + "-codex (Codex), and none of them drives ClaudeCode or OpenCode"))
		Expect(conditionOf(coder, crewletaiv1alpha1.ConditionResolvedRefs)(Default).Status).To(Equal(metav1.ConditionTrue),
			"the seat's references resolve: what Crewlet refuses is the seat as it is")
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonNoExecutorCLI))
		held, err := e.Client().ActiveConfig(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(held.RevisionID).To(Equal(good.RevisionID), "the document is refused before the engine is asked")

		By("placing the provider it runs on in E2B while the Company configures none, which Crewlet refuses at the provider")
		// In this order every document on the way is refused, so none
		// replaces the revision the engine runs.
		updateLive(c, func(c *crewletaiv1alpha1.Company) { c.Spec.Sandbox.E2B = crewletaiv1alpha1.E2BSandbox{} })
		updateLive(bundle, func(p *crewletaiv1alpha1.LLMProvider) { p.Spec.RunIn = crewletaiv1alpha1.SandboxPlacementE2B })
		Consistently(reasonOf(bundle, crewletaiv1alpha1.ConditionApplied)).WithTimeout(3*time.Second).
			ShouldNot(Equal(crewletaiv1alpha1.ReasonSandboxPlacementNotOffered), "no executor launches on the provider")
		updateLive(coder, func(s *crewletaiv1alpha1.Seat) {
			s.Spec.LLM.Default = []crewletaiv1alpha1.LLMProviderReference{{Name: bundle.Name}}
		})
		Eventually(reasonOf(bundle, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonSandboxPlacementNotOffered))
		Expect(conditionOf(bundle, crewletaiv1alpha1.ConditionApplied)(Default).Message).
			To(ContainSubstring("Spec.runIn: is E2B, and Company " + name + "'s spec.sandbox configures no e2b, while the executor of seat " +
				coder.Name + " can launch on it"))
		Expect(conditionOf(bundle, crewletaiv1alpha1.ConditionResolvedRefs)(Default).Status).To(Equal(metav1.ConditionTrue),
			"the provider's references resolve: what Crewlet refuses is the cell it places runs in")
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonSandboxPlacementNotOffered))
		held, err = e.Client().ActiveConfig(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(held.RevisionID).To(Equal(good.RevisionID))

		By("taking the Company's spec.sandbox away, which Crewlet refuses on the Company")
		updateLive(c, func(c *crewletaiv1alpha1.Company) { c.Spec.Sandbox = crewletaiv1alpha1.SandboxSettings{} })
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)).WithTimeout(settle).
			Should(Equal(crewletaiv1alpha1.ReasonSandboxRequired))
		Expect(conditionOf(c, crewletaiv1alpha1.ConditionConfigApplied)(Default).Message).
			To(ContainSubstring("Company/" + name + " spec.sandbox: is unset, and every agent seat's executor runs as its coding CLI's own run"))
		Eventually(reasonOf(bundle, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).
			ShouldNot(Equal(crewletaiv1alpha1.ReasonSandboxPlacementNotOffered),
				"the refusal of the Company stands for the provider's, as Crewlet asks nothing of a cell without providers.sandbox")
		held, err = e.Client().ActiveConfig(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(held.RevisionID).To(Equal(good.RevisionID))

		By("running the executors Direct again")
		updateLive(bundle, func(p *crewletaiv1alpha1.LLMProvider) { p.Spec.RunIn = "" })
		updateLive(c, func(c *crewletaiv1alpha1.Company) {
			c.Spec.Sandbox = crewletaiv1alpha1.SandboxSettings{DefaultRunIn: crewletaiv1alpha1.SandboxPlacementDirect}
		})
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
		Expect(e.Client().PutSecret(ctx, stale, []byte("old"))).To(Succeed())
		Expect(e.Client().PutSecret(ctx, "MANUAL_TOKEN", []byte("mine"))).To(Succeed())

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
			g.Expect(k8sClient.Update(ctx, c)).To(Succeed())
		}).WithTimeout(settle).Should(Succeed())
		placed := func(pool string) func(*crewletaiv1alpha1.Seat) {
			return func(s *crewletaiv1alpha1.Seat) { s.Spec.Placement = crewletaiv1alpha1.SeatPlacement{Pool: pool} }
		}
		shared := newSeat(namespace, name, "shared", placed(poolGeneral))
		alone := newSeat(namespace, name, "alone", placed(poolIsolated))
		lost := newSeat(namespace, name, "lost", placed(nowhere))
		standIn(c)

		By("reporting the seat in a pool the company does not declare as a reference that does not resolve, which holds nothing")
		Eventually(reasonOf(lost, crewletaiv1alpha1.ConditionResolvedRefs)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonPoolNotFound))
		Expect(conditionOf(lost, crewletaiv1alpha1.ConditionResolvedRefs)(Default).Message).To(ContainSubstring("pool nowhere"))
		Eventually(reasonOf(lost, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonBlocked))

		By("refusing the seat in a PerSeat pool on Applied, its references resolved")
		Eventually(reasonOf(alone, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonPerSeatNotAllowed))
		Expect(conditionOf(alone, crewletaiv1alpha1.ConditionApplied)(Default).Message).To(ContainSubstring("spec.seatPools.allowPerSeat"))
		Expect(conditionOf(alone, crewletaiv1alpha1.ConditionResolvedRefs)(Default).Status).To(Equal(metav1.ConditionTrue))

		By("blocking the seat that can run, and recording where it is placed")
		Eventually(reasonOf(shared, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonBlocked),
			"the seat holds nothing: the other seats' refusals hold the document")
		Expect(conditionOf(shared, crewletaiv1alpha1.ConditionResolvedRefs)(Default).Status).To(Equal(metav1.ConditionTrue))
		Expect(reasonOf(shared, crewletaiv1alpha1.ConditionReady)(Default)).
			NotTo(BeElementOf(crewletaiv1alpha1.ReasonNotExposed, crewletaiv1alpha1.ReasonSandboxLoginNotPortable),
				"a seat pod's Direct boxes call the seat's tools back on the pod's own listener")
		Expect(log.of(shared.Name)).To(BeEmpty(), "a seat whose executor can run is not announced")
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

	It("applies a seat whose executor cannot reach its tools where it runs, and reports it on the Seat", func() {
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
			c.Spec.Sandbox.E2B = crewletaiv1alpha1.E2BSandbox{
				APIKeySecretRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-e2b", Key: llmKey},
			}
		})
		remote := &crewletaiv1alpha1.LLMProvider{
			Namespace: namespace, Name: name + "-remote",
			Spec: crewletaiv1alpha1.LLMProviderSpec{
				CompanyRef: crewletaiv1alpha1.CompanyReference{Name: name},
				LLMProviderSettings: crewletaiv1alpha1.LLMProviderSettings{
					Agent: crewletaiv1alpha1.CLIAgentClaudeCode, Model: cliModel,
				},
				RunIn: crewletaiv1alpha1.SandboxPlacementE2B,
				Auth:  crewletaiv1alpha1.CLIAgentAuth{TokenSecretRef: crewletaiv1alpha1.SecretKeyReference{Name: name + "-llm", Key: llmKey}},
			},
		}
		Expect(k8sClient.Create(ctx, remote)).To(Succeed())
		coder := newSeat(namespace, name, "coder", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.LLM.Default = []crewletaiv1alpha1.LLMProviderReference{{Name: remote.Name}}
		})

		By("applying the company with no public address, which a box in E2B calls its seat's tools back at")
		standInAt(c, "")
		applied(c, crewletaiv1alpha1.ReasonApplied)
		active := runsRendered(e, c)
		Expect(string(active.Document)).To(ContainSubstring("handle: " + coder.Name))
		Expect(reasonOf(coder, crewletaiv1alpha1.ConditionApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonInRevision))
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonNotExposed))
		Expect(conditionOf(coder, crewletaiv1alpha1.ConditionReady)(Default).Message).To(ContainSubstring("has no public address"))
		Eventually(reasonOf(remote, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		Expect(reasonOf(seat(name+"-ana"), crewletaiv1alpha1.ConditionReady)(Default)).
			To(BeElementOf(crewletaiv1alpha1.ReasonLeaseHeld, crewletaiv1alpha1.ReasonUnplaced),
				"a seat whose executor runs Direct on a data pod reaches its tools on the pod's own listener")
		Expect(log.of(coder.Name)).To(ConsistOf(HavePrefix("Warning " + crewletaiv1alpha1.ReasonNotExposed)))

		By("adding a seat in a seat pool whose executor runs Direct, which reaches its tools on its seat pod's own listener")
		pooled := newSeat(namespace, name, "pooled", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Placement = crewletaiv1alpha1.SeatPlacement{Pool: poolGeneral}
		})
		Eventually(reasonOf(pooled, crewletaiv1alpha1.ConditionApplied)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonInRevision))
		Eventually(reasonOf(pooled, crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).
			Should(BeElementOf(crewletaiv1alpha1.ReasonLeaseHeld, crewletaiv1alpha1.ReasonUnplaced),
				"a seat pod serves its own Direct boxes the tool bridge on loopback, with no public address")
		applied(c, crewletaiv1alpha1.ReasonApplied)
		runsRendered(e, c)
		Expect(log.of(pooled.Name)).To(BeEmpty(), "a seat whose executor can run is not announced")

		By("giving the company a public address")
		standInAt(c, baseURL)
		Eventually(reasonOf(coder, crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).
			Should(BeElementOf(crewletaiv1alpha1.ReasonLeaseHeld, crewletaiv1alpha1.ReasonUnplaced))
		applied(c, crewletaiv1alpha1.ReasonApplied)
		runsRendered(e, c)
		Expect(log.of(pooled.Name)).To(BeEmpty(), "a seat that could always run is never announced")
		Expect(log.of(coder.Name)).To(ConsistOf(HavePrefix("Warning "+crewletaiv1alpha1.ReasonNotExposed)),
			"the seat is announced once, however many reconciles found it unrunnable")
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
		Expect(conditionOf(ana, crewletaiv1alpha1.ConditionReady)(Default).Message).To(And(
			ContainSubstring("It runs the seat's executor as ClaudeCode on LLMProvider "+name+"-llm"),
			ContainSubstring("Direct, on the data pod that holds the seat")), "the condition names where the seat's executor runs")

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

	It("reports whether anybody has joined the company, and that nobody can join one with no Human seat", func() {
		const name = "joined"
		e := startEngine(name)
		secret(name+"-llm", map[string]string{llmKey: "k1"})
		c := company(name, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		founder := newSeat(namespace, name, "founder", func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Kind, s.Spec.Goal = crewletaiv1alpha1.SeatKindHuman, ""
		})
		standIn(c)
		applied(c, crewletaiv1alpha1.ReasonApplied)
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionIdentityClaimed)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonUnclaimed))
		unclaimed := conditionOf(c, crewletaiv1alpha1.ConditionIdentityClaimed)(Default)
		Expect(unclaimed.Status).To(Equal(metav1.ConditionFalse))
		Expect(unclaimed.Message).To(ContainSubstring("Its Human seats are " + founder.Name + "."))
		Expect(unclaimed.Message).To(ContainSubstring("Secret " + name + "-api-access"))
		Expect(reasonOf(c, crewletaiv1alpha1.ConditionConfigApplied)(Default)).To(Equal(crewletaiv1alpha1.ReasonApplied),
			"a company nobody has joined still runs its document")

		By("adding its first person")
		e.AddPerson(GinkgoT(), "founder@joined.example", founder.Name)
		Eventually(reasonOf(c, crewletaiv1alpha1.ConditionIdentityClaimed)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonPersonEnrolled))
		Expect(conditionOf(c, crewletaiv1alpha1.ConditionIdentityClaimed)(Default).Status).To(Equal(metav1.ConditionTrue))

		By("running a company of agents alone")
		const agents = "agents-only"
		startEngine(agents)
		secret(agents+"-llm", map[string]string{llmKey: "k1"})
		only := company(agents, crewletaiv1alpha1.ManagementPolicyEnforce, "ana")
		standIn(only)
		applied(only, crewletaiv1alpha1.ReasonApplied)
		Eventually(reasonOf(only, crewletaiv1alpha1.ConditionIdentityClaimed)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonNoHumanSeat))
		Expect(conditionOf(only, crewletaiv1alpha1.ConditionIdentityClaimed)(Default).Status).To(Equal(metav1.ConditionFalse))
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
