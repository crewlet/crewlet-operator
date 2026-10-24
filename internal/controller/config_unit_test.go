package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/yaml"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// decodeSamples decodes every object in config/samples, the Nimbus company,
// its parts and its class, the namespaced ones into the namespace given.
func decodeSamples(namespace string) ([]client.Object, error) {
	files, err := filepath.Glob(filepath.Join("..", "..", "config", "samples", "v1alpha1_*.yaml"))
	if err != nil {
		return nil, err
	}
	var objects []client.Object
	for _, file := range files {
		data, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			return nil, err
		}
		for _, manifest := range regexp.MustCompile(`(?m)^---\s*$`).Split(string(data), -1) {
			var typeMeta metav1.TypeMeta
			if err := yaml.Unmarshal([]byte(manifest), &typeMeta); err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
			}
			var obj client.Object
			switch typeMeta.Kind {
			case render.KindEngineClass:
				obj = &crewletaiv1alpha1.EngineClass{}
			case render.KindCompany:
				obj = &crewletaiv1alpha1.Company{}
			case render.KindUnit:
				obj = &crewletaiv1alpha1.Unit{}
			case render.KindSeat:
				obj = &crewletaiv1alpha1.Seat{}
			case render.KindLLMProvider:
				obj = &crewletaiv1alpha1.LLMProvider{}
			case render.KindMCPServer:
				obj = &crewletaiv1alpha1.MCPServer{}
			case render.KindIntegration:
				obj = &crewletaiv1alpha1.Integration{}
			case render.KindSchedule:
				obj = &crewletaiv1alpha1.Schedule{}
			case "":
				// A manifest of comments only.
				continue
			default:
				// Skipping it would leave the kind out of every spec that
				// applies the samples, without any of them failing.
				return nil, fmt.Errorf("%s: a sample of kind %s, which the tests do not decode", file, typeMeta.Kind)
			}
			// Strict, so a field the API does not have fails the test
			// instead of vanishing.
			if err := yaml.UnmarshalStrict([]byte(manifest), obj); err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
			}
			if typeMeta.Kind != render.KindEngineClass {
				obj.SetNamespace(namespace)
			}
			objects = append(objects, obj)
		}
	}
	return objects, nil
}

// inputOf sorts objects into the input of a render.
func inputOf(objects []client.Object) *render.Input {
	in := &render.Input{StoreName: secretsync.StoreName, ProvisionedName: secretsync.ProvisionedName}
	for _, obj := range objects {
		switch o := obj.(type) {
		case *crewletaiv1alpha1.EngineClass:
			in.Class = o
		case *crewletaiv1alpha1.Company:
			in.Company = o
		case *crewletaiv1alpha1.Unit:
			in.Units = append(in.Units, *o)
		case *crewletaiv1alpha1.Seat:
			in.Seats = append(in.Seats, *o)
		case *crewletaiv1alpha1.LLMProvider:
			in.LLMProviders = append(in.LLMProviders, *o)
		case *crewletaiv1alpha1.MCPServer:
			in.MCPServers = append(in.MCPServers, *o)
		case *crewletaiv1alpha1.Integration:
			in.Integrations = append(in.Integrations, *o)
		case *crewletaiv1alpha1.Schedule:
			in.Schedules = append(in.Schedules, *o)
		}
	}
	return in
}

// TestSecretRefsCoverEveryRenderedReference holds the Secret index to the
// renderer: every Secret key the example company's document references must
// be found in the spec of one of the objects it was rendered from, or a
// rotation of that Secret would never reach the engine.
func TestSecretRefsCoverEveryRenderedReference(t *testing.T) {
	objects, err := decodeSamples("tenant-nimbus")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := render.Company(inputOf(objects))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Refs) < 10 {
		t.Fatalf("the samples render %d Secret references; the test needs them to reference Secrets in many places", len(doc.Refs))
	}
	indexed := make([]secretref.Key, 0, len(objects))
	for _, obj := range objects {
		indexed = append(indexed, secretRefsOf(obj)...)
	}
	for _, k := range doc.Refs {
		if !slices.ContainsFunc(indexed, func(ref secretref.Key) bool { return references(ref, k) }) {
			t.Errorf("the document references %s, which no object's spec is indexed by", k)
		}
	}
}

// TestSecretRefsOfReadsTheSpecOnly: a Company's status names the API access
// Secret, which the operator writes; indexing it would make the operator's
// own write look like a change to a referenced Secret.
func TestSecretRefsOfReadsTheSpecOnly(t *testing.T) {
	c := &crewletaiv1alpha1.Company{}
	c.Namespace = "ns"
	c.Status.APIAccessSecretRef.Name = "nimbus-api-access"
	c.Spec.Embeddings.APIKeySecretRef = crewletaiv1alpha1.SecretKeyReference{Name: "openai", Key: llmKey}
	got := secretRefsOf(c)
	want := []secretref.Key{{Namespace: "ns", Name: "openai", Key: llmKey}}
	if !slices.Equal(got, want) {
		t.Errorf("secretRefsOf() = %v, want %v", got, want)
	}
}

// TestReferencesMatchesAKeyOrTheWholeSecret pins how a missing key is traced
// back to the references that could have made it.
func TestReferencesMatchesAKeyOrTheWholeSecret(t *testing.T) {
	const slack = "ana-slack"
	missing := secretref.Key{Namespace: "ns", Name: slack, Key: "botToken"}
	for _, c := range []struct {
		ref  secretref.Key
		want bool
	}{
		{secretref.Key{Namespace: "ns", Name: slack, Key: "botToken"}, true},
		{secretref.Key{Namespace: "ns", Name: slack}, true},
		{secretref.Key{Namespace: "ns", Name: slack, Key: "signingSecret"}, false},
		{secretref.Key{Namespace: "other", Name: slack}, false},
	} {
		if got := references(c.ref, missing); got != c.want {
			t.Errorf("references(%v, %v) = %v, want %v", c.ref, missing, got, c.want)
		}
	}
}

// TestChangeClockWaitsOutTheQuietPeriod: a change holds the company back for
// the quiet period after the latest change, and no longer.
func TestChangeClockWaitsOutTheQuietPeriod(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	clock := changeClock{now: func() time.Time { return now }}
	company := types.NamespacedName{Namespace: "ns", Name: companyName}

	if wait := clock.unsettled(company); wait != 0 {
		t.Fatalf("a company nothing changed for waits %s", wait)
	}
	clock.touch(company)
	now = now.Add(500 * time.Millisecond)
	clock.touch(company)
	now = now.Add(500 * time.Millisecond)
	if wait := clock.unsettled(company); wait != quietPeriod-500*time.Millisecond {
		t.Fatalf("unsettled() = %s, want the rest of the quiet period after the latest change", wait)
	}
	if other := (types.NamespacedName{Namespace: "ns", Name: "acme"}); clock.unsettled(other) != 0 {
		t.Fatal("a change to one company held another back")
	}
	now = now.Add(quietPeriod)
	if wait := clock.unsettled(company); wait != 0 {
		t.Fatalf("unsettled() = %s after the quiet period", wait)
	}
}

// TestChangeClockStopsWaitingForAnEndlessBurst: changes that never pause
// (a Secret rewritten every second) hold a company back for maxSettle at
// most, so its document is applied after all.
func TestChangeClockStopsWaitingForAnEndlessBurst(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	clock := changeClock{now: func() time.Time { return now }}
	company := types.NamespacedName{Namespace: "ns", Name: companyName}
	start := now
	for now.Sub(start) < maxSettle {
		clock.touch(company)
		if wait := clock.unsettled(company); wait <= 0 || now.Add(wait).After(start.Add(maxSettle)) {
			t.Fatalf("unsettled() = %s at %s into the burst, want a wait that ends by %s", wait, now.Sub(start), maxSettle)
		}
		now = now.Add(time.Second)
	}
	clock.touch(company)
	if wait := clock.unsettled(company); wait != 0 {
		t.Fatalf("unsettled() = %s after %s of changes, want 0", wait, now.Sub(start))
	}
}

// TestChangeClockForgetsACompany: the clock holds nothing about a company
// once it is forgotten, as it is when the company is deleted.
func TestChangeClockForgetsACompany(t *testing.T) {
	clock := changeClock{}
	company := types.NamespacedName{Namespace: "ns", Name: companyName}
	clock.touch(company)
	clock.forget(company)
	if len(clock.bursts) != 0 {
		t.Fatalf("the clock still holds %v", clock.bursts)
	}
}

// TestStoreNamesInFindsTheOperatorsReferences: only names of the operator's
// shape are kept for the running document, since only those are ever
// deleted.
func TestStoreNamesInFindsTheOperatorsReferences(t *testing.T) {
	owned := secretsync.StoreName(secretref.Key{Namespace: "ns", Name: "anthropic", Key: llmKey})
	document := []byte(`providers:
  llm:
    main:
      auth: {api_key: "${` + owned + `}", token: "${` + owned + `}"}
      env: {MANUAL: "${MANUAL_TOKEN}", REDACTED: "__redacted__"}
`)
	if got := storeNamesIn(document); !slices.Equal(got, []string{owned}) {
		t.Errorf("storeNamesIn() = %v, want [%s]", got, owned)
	}
}

// TestUnitTreeCountsDepthAndSeats pins the Units' status: a Unit outside the
// chart (its parents loop, or one is missing) has no depth.
func TestUnitTreeCountsDepthAndSeats(t *testing.T) {
	unit := func(name, parent string) *part {
		u := &crewletaiv1alpha1.Unit{}
		u.Name = name
		u.Spec.ParentRef.Name = parent
		return newPart(u)
	}
	seatIn := func(name, unit string) *part {
		s := &crewletaiv1alpha1.Seat{}
		s.Name = name
		s.Spec.UnitRef.Name = unit
		return newPart(s)
	}
	facts := unitTree([]*part{
		unit("eng", ""), unit("plat", "eng"), unit("storage", "plat"),
		unit("a", "b"), unit("b", "a"), unit("stray", "missing"),
		seatIn("ana", "plat"), seatIn("bo", "plat"), seatIn("cy", "eng"), seatIn("di", ""),
	})
	depth := func(name string) any {
		if d := facts[name].depth; d != nil {
			return *d
		}
		return nil
	}
	for name, want := range map[string]any{
		"eng": int32(0), "plat": int32(1), "storage": int32(2), "a": nil, "b": nil, "stray": nil,
	} {
		if got := depth(name); got != want {
			t.Errorf("depth of %s = %v, want %v", name, got, want)
		}
	}
	for name, want := range map[string]int32{"eng": 1, "plat": 2, "storage": 0} {
		if got := facts[name].seats; got != want {
			t.Errorf("seats of %s = %d, want %d", name, got, want)
		}
	}
}

// TestBoundedKeepsAMessageReadable: a message is cut at a rune boundary and
// says it was cut.
func TestBoundedKeepsAMessageReadable(t *testing.T) {
	long := ""
	for len(long) <= maxConditionMessage {
		long += "Ünïcödé "
	}
	got := bounded(long)
	if len(got) > maxConditionMessage {
		t.Fatalf("bounded() is %d bytes, more than %d", len(got), maxConditionMessage)
	}
	if !regexp.MustCompile(`(?s)^[\p{L} ]+ …$`).MatchString(got) {
		t.Errorf("bounded() cut a rune or lost the ellipsis: %q", got[len(got)-16:])
	}
	if bounded("short") != "short" {
		t.Error("bounded() changed a short message")
	}
}

// TestDataPodURL pins the address the first document is applied through: the
// first data pod's own name in the headless data Service.
func TestDataPodURL(t *testing.T) {
	c := &crewletaiv1alpha1.Company{}
	c.Namespace, c.Name = "tenant-3f2a", "nimbus"
	if got, want := dataPodURL(c), "http://nimbus-data-0.nimbus-data.tenant-3f2a.svc:8080"; got != want {
		t.Errorf("dataPodURL() = %s, want %s", got, want)
	}
}

func TestMovingTheDefaultClassReachesTheDocument(t *testing.T) {
	class := func(isDefault string) *crewletaiv1alpha1.EngineClass {
		c := &crewletaiv1alpha1.EngineClass{}
		if isDefault != "" {
			c.Annotations = map[string]string{crewletaiv1alpha1.DefaultEngineClassAnnotation: isDefault}
		}
		return c
	}
	p := defaultClassChanged()
	if !p.Update(event.UpdateEvent{ObjectOld: class(""), ObjectNew: class("true")}) {
		t.Error("a class made the default does not re-run the Companies that name none")
	}
	if !p.Update(event.UpdateEvent{ObjectOld: class("true"), ObjectNew: class("")}) {
		t.Error("a class that stops being the default does not re-run the Companies that name none")
	}
	if p.Update(event.UpdateEvent{ObjectOld: class("true"), ObjectNew: class("true")}) {
		t.Error("an unchanged annotation re-runs Companies")
	}
}

// TestEngineStateChangedSeesWhyACompanyIsRefused: the parts repeat the
// Company's Accepted reason, so a new reason re-runs the Company, and the
// config reconciler's own writes do not.
func TestEngineStateChangedSeesWhyACompanyIsRefused(t *testing.T) {
	with := func(t, reason string) *crewletaiv1alpha1.Company {
		c := &crewletaiv1alpha1.Company{}
		c.Status.Conditions = []metav1.Condition{{Type: t, Status: metav1.ConditionFalse, Reason: reason}}
		return c
	}
	p := engineStateChanged()
	if !p.Update(event.UpdateEvent{
		ObjectOld: with(crewletaiv1alpha1.ConditionAccepted, crewletaiv1alpha1.ReasonEngineClassNotFound),
		ObjectNew: with(crewletaiv1alpha1.ConditionAccepted, crewletaiv1alpha1.ReasonLimitExceeded),
	}) {
		t.Error("a Company refused for another reason does not re-run its parts' status")
	}
	if p.Update(event.UpdateEvent{
		ObjectOld: with(crewletaiv1alpha1.ConditionConfigApplied, crewletaiv1alpha1.ReasonInvalid),
		ObjectNew: with(crewletaiv1alpha1.ConditionConfigApplied, crewletaiv1alpha1.ReasonApplied),
	}) {
		t.Error("the config reconciler's own condition re-runs it")
	}
}

// TestEngineStateChangedSeesTheAddressTheRenderReads: the runtime reconciler
// writes the vendors' address into the Company's status, and the render's
// verdicts and the Seats' webhook addresses are built on it, so a change of it
// renders again. The dashboard's address is the engine's crewlet.yaml, which
// the runtime reconciler writes, and not the document's: a change of it alone
// is nothing for the config reconciler to do.
func TestEngineStateChangedSeesTheAddressTheRenderReads(t *testing.T) {
	p := engineStateChanged()
	updated := &crewletaiv1alpha1.Company{}
	updated.Status.Endpoints.PublicBaseURL = "https://hooks.example.com"
	if !p.Update(event.UpdateEvent{ObjectOld: &crewletaiv1alpha1.Company{}, ObjectNew: updated}) {
		t.Error("a new vendors' address does not render the document again")
	}
	for _, set := range []func(*crewletaiv1alpha1.CompanyEndpoints){
		func(e *crewletaiv1alpha1.CompanyEndpoints) { e.ExposedAPI = "https://api.example.com" },
		func(e *crewletaiv1alpha1.CompanyEndpoints) { e.DashboardBaseURL = "https://crewlet.example.com" },
	} {
		unchanged := &crewletaiv1alpha1.Company{}
		set(&unchanged.Status.Endpoints)
		if p.Update(event.UpdateEvent{ObjectOld: &crewletaiv1alpha1.Company{}, ObjectNew: unchanged}) {
			t.Errorf("an endpoint the render does not read renders the document again: %+v", unchanged.Status.Endpoints)
		}
	}
}

// TestEngineHealthyReportsTheEnginesAlarms pins the EngineHealthy condition:
// unknown until the engine has looked, and pointing at backups when that is
// what it raises.
func TestEngineHealthyReportsTheEnginesAlarms(t *testing.T) {
	for _, c := range []struct {
		name    string
		alarms  *engine.Alarms
		err     error
		status  metav1.ConditionStatus
		reason  string
		message string
	}{
		{
			name: "unreachable", err: fmt.Errorf("dial tcp: connection refused"),
			status: metav1.ConditionUnknown, reason: crewletaiv1alpha1.ReasonAlarmsNotReported, message: "connection refused",
		},
		{
			name:   "not evaluated yet",
			status: metav1.ConditionUnknown, reason: crewletaiv1alpha1.ReasonAlarmsNotReported, message: "not evaluated",
		},
		{
			name: "none", alarms: &engine.Alarms{},
			status: metav1.ConditionTrue, reason: crewletaiv1alpha1.ReasonNoAlarms, message: "no alarm",
		},
		{
			name: "no backup", alarms: &engine.Alarms{Count: 5, Worst: "backup_age"},
			status: metav1.ConditionFalse, reason: crewletaiv1alpha1.ReasonAlarms, message: "`crewlet backup`",
		},
		{
			name: "another", alarms: &engine.Alarms{Count: 1, Worst: "objects_missing"},
			status: metav1.ConditionFalse, reason: crewletaiv1alpha1.ReasonAlarms, message: "objects_missing has stood longest",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := engineHealthy(c.alarms, c.err)
			if got.Type != crewletaiv1alpha1.ConditionEngineHealthy || got.Status != c.status || got.Reason != c.reason ||
				!strings.Contains(got.Message, c.message) {
				t.Errorf("got %s=%s %s %q, want %s %s containing %q",
					got.Type, got.Status, got.Reason, got.Message, c.status, c.reason, c.message)
			}
		})
	}
}

// TestSyncPauseChangesNoPauseItDoesNotOwn runs the decision of one seat's
// pause with no engine to call: under Observe and Seed, and for a Seat that
// leaves spec.paused unset under any policy, the operator only reports what
// the engine holds, and a call would reach the nil client and fail the test.
func TestSyncPauseChangesNoPauseItDoesNotOwn(t *testing.T) {
	paused := engine.SeatPause{Paused: true, By: humanJane, At: time.Date(2026, 6, 8, 7, 30, 2, 0, time.UTC), Reason: "Looking"}
	for _, c := range []struct {
		name   string
		policy crewletaiv1alpha1.ManagementPolicy
		want   *bool
		state  engine.SeatPause
		status metav1.ConditionStatus
		reason string
	}{
		{"unset, paused by a person, under Enforce", crewletaiv1alpha1.ManagementPolicyEnforce, nil, paused,
			metav1.ConditionTrue, crewletaiv1alpha1.ReasonSeatPaused},
		{"unset, running, under Enforce", crewletaiv1alpha1.ManagementPolicyEnforce, nil, engine.SeatPause{},
			metav1.ConditionFalse, crewletaiv1alpha1.ReasonSeatRunning},
		{"asked to run, paused, under Observe", crewletaiv1alpha1.ManagementPolicyObserve, new(false), paused,
			metav1.ConditionTrue, crewletaiv1alpha1.ReasonPauseNotManaged},
		{"asked to pause, running, under Seed", crewletaiv1alpha1.ManagementPolicySeed, new(true), engine.SeatPause{},
			metav1.ConditionFalse, crewletaiv1alpha1.ReasonPauseNotManaged},
	} {
		t.Run(c.name, func(t *testing.T) {
			run := &configRun{company: &crewletaiv1alpha1.Company{Spec: crewletaiv1alpha1.CompanySpec{
				Config: crewletaiv1alpha1.ConfigSettings{ManagementPolicy: c.policy},
			}}}
			seat := &crewletaiv1alpha1.Seat{Name: "jo", Spec: crewletaiv1alpha1.SeatSpec{Paused: c.want}}
			got, again := run.syncPause(t.Context(), nil, seat, c.state)
			if got.status != c.status || got.reason != c.reason || again != 0 {
				t.Fatalf("syncPause() = %+v, %s; want %s %s", got, again, c.status, c.reason)
			}
			if got.status == metav1.ConditionTrue && got.pause != c.state {
				t.Errorf("the verdict reports the pause %+v, want the engine's %+v", got.pause, c.state)
			}
		})
	}
}
