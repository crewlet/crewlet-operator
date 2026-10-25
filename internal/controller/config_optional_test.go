package controller

import (
	"context"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// The objects and Secrets of the optional-key reconciles.
const (
	optionalServer   = "tavily"
	optionalRegistry = "NPM_CONFIG_REGISTRY"
	optionalSecret   = "npm"
	optionalKey      = "registry"
	requiredSecret   = "tavily"
	requiredKey      = "tavilyKey"
	tavilyValue      = "tvly"
	npmRegistry      = "https://npm.example.com"
	// seatTokenKey is the key of a Secret a Seat gives a tool server its own
	// value from.
	seatTokenKey = "token"
	// serverShell is the command the tool servers the engine starts in these
	// tests run: a shell, which writes down what it was started with.
	serverShell = "/bin/sh"
)

// optionalRun is a rendered reconcile of a Company whose MCPServer tavily is
// started with its API key, required, and a registry, optional, each read from
// a Secret, and whose Seat ana gives it nothing; and of the Secrets given, as
// the secret sync reads them.
func optionalRun(t *testing.T, secrets ...*corev1.Secret) (*configRun, *part, *events.FakeRecorder) {
	t.Helper()
	objects, err := decodeSamples("ns")
	if err != nil {
		t.Fatal(err)
	}
	var class *crewletaiv1alpha1.EngineClass
	for _, obj := range objects {
		if c, ok := obj.(*crewletaiv1alpha1.EngineClass); ok {
			class = c
		}
	}
	company := &crewletaiv1alpha1.Company{Namespace: "ns", Name: fleetCompany, Spec: crewletaiv1alpha1.CompanySpec{CompanyName: "Acme"}}
	meta.SetStatusCondition(&company.Status.Conditions, metav1.Condition{
		Type: crewletaiv1alpha1.ConditionAccepted, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonValid,
	})
	server := &crewletaiv1alpha1.MCPServer{Namespace: "ns", Name: optionalServer, Spec: crewletaiv1alpha1.MCPServerSpec{
		CompanyRef: crewletaiv1alpha1.CompanyReference{Name: fleetCompany},
		Transport:  crewletaiv1alpha1.MCPTransportStdio,
		Command:    "npx",
		Env: []crewletaiv1alpha1.MCPEnvVar{
			{Name: "TAVILY_API_KEY", ValueFrom: fromSecret(requiredSecret, requiredKey)},
			{Name: optionalRegistry, ValueFrom: fromSecret(optionalSecret, optionalKey)},
		},
		OptionalKeys: []string{optionalRegistry},
	}}
	seat := &crewletaiv1alpha1.Seat{Namespace: "ns", Name: seatAna, Spec: crewletaiv1alpha1.SeatSpec{
		CompanyRef: crewletaiv1alpha1.CompanyReference{Name: fleetCompany}, Goal: "Answer.",
	}}
	p := newPart(server)
	recorder := events.NewFakeRecorder(10)
	run := &configRun{
		CompanyConfigReconciler: &CompanyConfigReconciler{
			Recorder:  recorder,
			APIReader: fakeClient().WithObjects(objectsOf(secrets)...).Build(),
		},
		company: company, class: class, parts: []*part{p, newPart(seat)},
	}
	doc, err := render.Company(run.input())
	if err != nil {
		t.Fatal(err)
	}
	run.doc = doc
	run.objectsChecked = true
	return run, p, recorder
}

func objectsOf(secrets []*corev1.Secret) []client.Object {
	out := make([]client.Object, len(secrets))
	for i, s := range secrets {
		out[i] = s
	}
	return out
}

func fromSecret(name, key string) crewletaiv1alpha1.SecretValueSource {
	return crewletaiv1alpha1.SecretValueSource{SecretKeyRef: crewletaiv1alpha1.SecretKeyReference{Name: name, Key: key}}
}

func valueSecret(name string, data map[string]string) *corev1.Secret {
	s := &corev1.Secret{Data: map[string][]byte{}}
	s.Namespace, s.Name = "ns", name
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s
}

// resolvedRefs is the part's ResolvedRefs once the reconcile decided it.
func resolvedRefs(run *configRun, p *part) *metav1.Condition {
	reconcileStatus(run, p)
	return meta.FindStatusCondition(*p.conditions(), crewletaiv1alpha1.ConditionResolvedRefs)
}

// TestAnOptionalSecretThatIsMissingHoldsNothing holds a reconcile of a
// tool server whose optional key's Secret does not exist to what Kubernetes
// does with a missing optional secretKeyRef: nothing is missing, the
// document goes ahead, and the part says which value the engine leaves out.
// A missing Secret under the required key is the failure it always was.
func TestAnOptionalSecretThatIsMissingHoldsNothing(t *testing.T) {
	required := valueSecret(requiredSecret, map[string]string{requiredKey: tavilyValue})
	run, p, _ := optionalRun(t, required)
	blocked, err := run.resolveSecrets(context.Background())
	if err != nil || blocked {
		t.Fatalf("the reconcile is held for an optional key's missing Secret: blocked %t, %v", blocked, err)
	}
	c := resolvedRefs(run, p)
	if c == nil || c.Status != metav1.ConditionTrue || c.Reason != crewletaiv1alpha1.ReasonResolved ||
		!strings.Contains(c.Message, "spec.env[1].valueFrom (NPM_CONFIG_REGISTRY): Secret ns/npm does not exist") {
		t.Errorf("ResolvedRefs is %+v, want True, naming the value the engine leaves out", c)
	}

	run, p, _ = optionalRun(t, valueSecret(optionalSecret, map[string]string{optionalKey: npmRegistry}))
	blocked, err = run.resolveSecrets(context.Background())
	if err != nil || !blocked {
		t.Fatalf("the required key's missing Secret holds nothing: blocked %t, %v", blocked, err)
	}
	if c := resolvedRefs(run, p); c == nil || c.Reason != crewletaiv1alpha1.ReasonSecretNotFound {
		t.Errorf("ResolvedRefs is %+v, want %s", c, crewletaiv1alpha1.ReasonSecretNotFound)
	}
}

// TestAMissingSecretIsReportedWhereItIsRequired holds the report of a
// missing Secret key to the parts that read it under a key they do not let go
// without it: a part that reads the same key only as an optional value does
// not need it, and is not told it is missing, although the document is held
// for the part that does.
func TestAMissingSecretIsReportedWhereItIsRequired(t *testing.T) {
	run, server, _ := optionalRun(t, valueSecret(requiredSecret, map[string]string{requiredKey: tavilyValue}))
	seat := run.parts[1]
	ana := seat.obj.(*crewletaiv1alpha1.Seat)
	ana.Spec.Sandbox = crewletaiv1alpha1.SeatSandbox{Env: []crewletaiv1alpha1.SeatEnvVar{{
		Name:      optionalRegistry,
		ValueFrom: crewletaiv1alpha1.SeatCredentialSource{SecretKeyRef: fromSecret(optionalSecret, optionalKey).SecretKeyRef},
	}}}
	seat.refs = secretRefsOf(ana)
	doc, err := render.Company(run.input())
	if err != nil {
		t.Fatal(err)
	}
	run.doc = doc
	blocked, err := run.resolveSecrets(context.Background())
	if err != nil || !blocked {
		t.Fatalf("a Secret one part requires is missing and nothing is held: blocked %t, %v", blocked, err)
	}
	if c := resolvedRefs(run, seat); c == nil || c.Reason != crewletaiv1alpha1.ReasonSecretNotFound {
		t.Errorf("the Seat that requires it has ResolvedRefs %+v, want %s", c, crewletaiv1alpha1.ReasonSecretNotFound)
	}
	// Held, the document leaves the MCPServer going without the value as
	// much as applied: it is told so, and not that the key exists.
	if c := resolvedRefs(run, server); c == nil || c.Status != metav1.ConditionTrue ||
		!strings.Contains(c.Message, "spec.env[1].valueFrom (NPM_CONFIG_REGISTRY): Secret ns/npm does not exist") {
		t.Errorf("the MCPServer that lets it go has ResolvedRefs %+v, want True, naming the value it goes without", c)
	}
	if missing := (secretref.Key{Namespace: "ns", Name: optionalSecret, Key: optionalKey}); !slices.Contains(run.input().Unset, missing) {
		t.Errorf("the render is not told %v holds nothing while the document is held: %v", missing, run.input().Unset)
	}
}

// TestAnEmptyRequiredValueIsReported holds a tool server's required value
// whose Secret key holds nothing to a report on the MCPServer, which holds
// nothing back: Crewlet applies the document and starts the server only
// once the value is there, and says so nowhere the operator reads, so the
// part does, once, with a Warning event. Optional, the same empty value is
// left out and no report.
func TestAnEmptyRequiredValueIsReported(t *testing.T) {
	run, p, recorder := optionalRun(t,
		valueSecret(requiredSecret, map[string]string{requiredKey: ""}),
		valueSecret(optionalSecret, map[string]string{optionalKey: ""}))
	blocked, err := run.resolveSecrets(context.Background())
	if err != nil || blocked {
		t.Fatalf("an empty value holds the document: blocked %t, %v", blocked, err)
	}
	if p.hasProblem() {
		t.Error("an empty value holds the document")
	}
	c := resolvedRefs(run, p)
	if c == nil || c.Status != metav1.ConditionFalse || c.Reason != crewletaiv1alpha1.ReasonSecretValueEmpty ||
		!strings.Contains(c.Message, `Spec.env[0].valueFrom: Secret ns/tavily holds an empty value under "`+requiredKey+`"`) ||
		strings.Contains(c.Message, optionalRegistry+" in the optionalKeys") {
		t.Errorf("ResolvedRefs is %+v, want False %s for the required key alone", c, crewletaiv1alpha1.ReasonSecretValueEmpty)
	}
	got := make([]string, 0, len(recorder.Events))
	for range len(recorder.Events) {
		got = append(got, <-recorder.Events)
	}
	if len(got) != 1 || !strings.HasPrefix(got[0], "Warning "+crewletaiv1alpha1.ReasonSecretValueEmpty) {
		t.Errorf("the events are %q, want one Warning %s", got, crewletaiv1alpha1.ReasonSecretValueEmpty)
	}
	empty := secretref.Key{Namespace: "ns", Name: requiredSecret, Key: requiredKey}
	if !slices.Contains(run.input().Empty, empty) {
		t.Errorf("the render is not told %v holds an empty value: %v", empty, run.input().Empty)
	}

	run, p, _ = optionalRun(t,
		valueSecret(requiredSecret, map[string]string{requiredKey: tavilyValue}),
		valueSecret(optionalSecret, map[string]string{optionalKey: ""}))
	if blocked, err := run.resolveSecrets(context.Background()); err != nil || blocked {
		t.Fatalf("an empty optional value holds the document: blocked %t, %v", blocked, err)
	}
	c = resolvedRefs(run, p)
	if c == nil || c.Status != metav1.ConditionTrue ||
		!strings.Contains(c.Message, `spec.env[1].valueFrom (NPM_CONFIG_REGISTRY): Secret ns/npm holds an empty value under "registry"`) {
		t.Errorf("ResolvedRefs is %+v, want True, naming the empty value the engine leaves out", c)
	}
}

// TestAnEmptyValueOfTheCompanysOwnStepIsSaid holds an empty value a
// Company's own setup step gives every executor run to a sentence of the
// SecretsSynced message, the Company having no ResolvedRefs of its own to
// report it on, and the same value under a key the step declares optional to
// a sentence naming it among the values the steps go without; and either to
// none while no seat launches an executor, as the company has no model
// provider, when the engine starts nothing with it.
func TestAnEmptyValueOfTheCompanysOwnStepIsSaid(t *testing.T) {
	const stepSecret, stepKey, stepVar = "setup", "npmToken", "NPM_TOKEN"
	for _, c := range []struct {
		optional, runs bool
	}{{false, true}, {true, true}, {false, false}} {
		run, _, _ := optionalRun(t,
			valueSecret(requiredSecret, map[string]string{requiredKey: tavilyValue}),
			valueSecret(optionalSecret, map[string]string{optionalKey: npmRegistry}),
			valueSecret("e2b", map[string]string{"e2bKey": "e2b"}),
			valueSecret("claude-key", map[string]string{llmKey: "k"}),
			valueSecret(stepSecret, map[string]string{stepKey: ""}))
		step := crewletaiv1alpha1.SandboxSetupStep{
			Name: "registry",
			Env:  []crewletaiv1alpha1.SecretEnvVar{{Name: stepVar, ValueFrom: fromSecret(stepSecret, stepKey)}},
		}
		if c.optional {
			step.OptionalKeys = []string{stepVar}
		}
		run.company.Spec.Sandbox = crewletaiv1alpha1.SandboxSettings{
			DefaultRunIn: crewletaiv1alpha1.SandboxPlacementE2B,
			E2B:          crewletaiv1alpha1.E2BSandbox{APIKeySecretRef: crewletaiv1alpha1.SecretKeyReference{Name: "e2b", Key: "e2bKey"}},
			Setup:        []crewletaiv1alpha1.SandboxSetupStep{step},
		}
		if c.runs {
			// A provider ana's executor runs on: every agent seat's
			// executor runs the Company's steps once one exists.
			run.parts = append(run.parts, newPart(&crewletaiv1alpha1.LLMProvider{
				Namespace: "ns", Name: "claude", Spec: crewletaiv1alpha1.LLMProviderSpec{
					CompanyRef: crewletaiv1alpha1.CompanyReference{Name: fleetCompany},
					LLMProviderSettings: crewletaiv1alpha1.LLMProviderSettings{
						Agent: crewletaiv1alpha1.CLIAgentClaudeCode, Model: cliModel,
					},
					Auth: crewletaiv1alpha1.CLIAgentAuth{
						Mode:            crewletaiv1alpha1.CLIAgentAuthAPIKey,
						APIKeySecretRef: crewletaiv1alpha1.SecretKeyReference{Name: "claude-key", Key: llmKey},
					},
				},
			}))
		}
		doc, err := render.Company(run.input())
		if err != nil {
			t.Fatal(err)
		}
		run.doc = doc
		if blocked, err := run.resolveSecrets(context.Background()); err != nil || blocked {
			t.Fatalf("an empty value holds the document: blocked %t, %v", blocked, err)
		}
		const said = "spec.sandbox.setup[0].env[0].valueFrom: Secret ns/" + stepSecret + " holds an empty value under \"" +
			stepKey + "\", given as " + stepVar + ": each executor run whose environment it reaches starts without it"
		const leftOut = "spec.sandbox.setup[0].env[0].valueFrom (" + stepVar + "): Secret ns/" + stepSecret +
			" holds an empty value under \"" + stepKey + "\", and its spec.sandbox.setup[0].optionalKeys lists " + stepVar
		switch {
		case !c.runs && (len(run.companyEmpty) > 0 || len(run.companyLeftOut) > 0):
			t.Errorf("a value no executor run is given is reported: %q, %q", run.companyEmpty, run.leftOutLines(run.companyLeftOut))
		case c.runs && c.optional && (len(run.companyEmpty) > 0 || !slices.Equal(run.leftOutLines(run.companyLeftOut), []string{leftOut})):
			t.Errorf("the Company's values are %q empty and %q left out, want %q left out alone",
				run.companyEmpty, run.leftOutLines(run.companyLeftOut), leftOut)
		case c.runs && !c.optional && (len(run.companyEmpty) != 1 || !strings.HasPrefix(run.companyEmpty[0], said)):
			t.Errorf("the Company's empty values are %q, want one saying %q", run.companyEmpty, said)
		}
	}
}

// TestAValueIsReportedForWhatItKeepsFromStarting holds a report of a value
// that holds nothing to what the engine starts with it, and to the remedy it
// offers: a per-seat server's own value every seat granted it replaces with
// its own starts nothing, so it is not reported; a seat's own value is its
// identity, so leaving its key out is not offered; a server's own value of a
// shared server keeps the server from starting, and leaving it out is offered
// in the server's optionalKeys; and a seat's value the server's optionalKeys
// lets go is named on the Seat with the list that lets it.
func TestAValueIsReportedForWhatItKeepsFromStarting(t *testing.T) {
	const anaTavily, anaKey = "ana-tavily", "key"
	perSeat := func(run *configRun, env ...crewletaiv1alpha1.MCPCredentialValue) (*part, *part) {
		t.Helper()
		server := run.parts[0]
		server.obj.(*crewletaiv1alpha1.MCPServer).Spec.Shared = new(false)
		seat := run.parts[1]
		ana := seat.obj.(*crewletaiv1alpha1.Seat)
		ana.Spec.MCP = []crewletaiv1alpha1.MCPCredentials{{Name: optionalServer, Env: env}}
		seat.refs = secretRefsOf(ana)
		doc, err := render.Company(run.input())
		if err != nil {
			t.Fatal(err)
		}
		run.doc = doc
		if blocked, err := run.resolveSecrets(context.Background()); err != nil || blocked {
			t.Fatalf("blocked %t, %v", blocked, err)
		}
		return server, seat
	}
	fromAna := func(name, key string) crewletaiv1alpha1.MCPCredentialValue {
		return crewletaiv1alpha1.MCPCredentialValue{
			Name: name, ValueFrom: fromSecret(anaTavily, key),
		}
	}

	run, _, _ := optionalRun(t,
		valueSecret(requiredSecret, map[string]string{requiredKey: ""}),
		valueSecret(optionalSecret, map[string]string{optionalKey: npmRegistry}),
		valueSecret(anaTavily, map[string]string{anaKey: "tvly-ana"}))
	server, _ := perSeat(run, fromAna("TAVILY_API_KEY", anaKey))
	if c := resolvedRefs(run, server); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("a per-seat server's own empty value every seat replaces is reported: %+v", c)
	}

	run, _, _ = optionalRun(t,
		valueSecret(requiredSecret, map[string]string{requiredKey: tavilyValue}),
		valueSecret(optionalSecret, map[string]string{optionalKey: npmRegistry}),
		valueSecret(anaTavily, map[string]string{anaKey: ""}))
	_, seat := perSeat(run, fromAna("TAVILY_API_KEY", anaKey))
	c := resolvedRefs(run, seat)
	if c == nil || c.Reason != crewletaiv1alpha1.ReasonSecretValueEmpty ||
		!strings.Contains(c.Message, "does not start it. Give the key a value.") || strings.Contains(c.Message, "optionalKeys") {
		t.Errorf("a seat's empty value has ResolvedRefs %+v, want %s naming the seat's instance and no optionalKeys",
			c, crewletaiv1alpha1.ReasonSecretValueEmpty)
	}

	run, server, _ = optionalRun(t,
		valueSecret(requiredSecret, map[string]string{requiredKey: ""}),
		valueSecret(optionalSecret, map[string]string{optionalKey: npmRegistry}))
	if blocked, err := run.resolveSecrets(context.Background()); err != nil || blocked {
		t.Fatalf("blocked %t, %v", blocked, err)
	}
	c = resolvedRefs(run, server)
	if c == nil || c.Reason != crewletaiv1alpha1.ReasonSecretValueEmpty ||
		!strings.Contains(c.Message, "so it does not start MCPServer tavily. Give the key a value, or list "+
			"TAVILY_API_KEY in its spec.optionalKeys to have it left out") {
		t.Errorf("a shared server's empty value has ResolvedRefs %+v, want %s offering its optionalKeys",
			c, crewletaiv1alpha1.ReasonSecretValueEmpty)
	}

	run, _, _ = optionalRun(t,
		valueSecret(requiredSecret, map[string]string{requiredKey: tavilyValue}),
		valueSecret(optionalSecret, map[string]string{optionalKey: npmRegistry}),
		valueSecret(anaTavily, map[string]string{anaKey: "tvly-ana"}))
	_, seat = perSeat(run, fromAna("TAVILY_API_KEY", anaKey), fromAna(optionalRegistry, "registry"))
	c = resolvedRefs(run, seat)
	if c == nil || c.Status != metav1.ConditionTrue ||
		!strings.Contains(c.Message, `spec.mcp[0].env[1].valueFrom (NPM_CONFIG_REGISTRY): Secret ns/ana-tavily has no key "registry", `+
			"and MCPServer tavily's spec.optionalKeys lists NPM_CONFIG_REGISTRY") {
		t.Errorf("a seat's value the server lets go has ResolvedRefs %+v, want True naming the server's list", c)
	}
}
