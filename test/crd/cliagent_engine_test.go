package crd_test

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
	"github.com/crewlet/crewlet-operator/test/docs"
)

// TestCLIAgentSignInFollowsTheEngine holds the LLMProvider API's rules over a
// provider's sign-in, and the table docs/guides/cli-agents.md states
// them in, to the engine every suite runs (crewlet's main) rather than to a
// copy of its profile table. The token and key variable of each CLI are read
// from crewlet's own refusals, and held to the copy the API's own-variable
// rule and its cases name. Every CLI the API offers is then tried in every
// sign-in shape, those variables among them, each provider driving one seat's
// executor: the API server's verdict is taken from the installed CRD, and the
// engine's from the crewlet binary, by `crewlet validate` (does it admit the
// provider and its seat, and does it doubt either) and `crewlet llm list`
// (what it reports the CLI signed in with). A crewlet change that moves a
// profile's token or key variable, or marks another CLI as signing in through
// its environment, fails here, on the next run, until the API and the guide
// follow it.
//
// The engine admits one shape the API refuses on purpose, which this does not
// hold against it: a Subscription signed in to nothing (the engine expects
// `crewlet llm login` at a terminal a pod does not have). The rest must
// agree. Where a seat's executor run is signed in with what is the engine's
// to judge at each launch, on the seat the run is for, which no validation
// shows: the operator reports it on the seat (render's executor verdict).
func TestCLIAgentSignInFollowsTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	agents := llmProviderEnum(t, "spec", "agent")

	variables := engineVariables(t, namespace, agents)
	for _, agent := range agents {
		name := v1.CLIAgentName(agent)
		if got, want := variables[name], cliVariables[name]; got != want {
			t.Errorf("%s: the engine's profile names token variable %q and key variable %q, the API's rules %q and %q",
				agent, got.headless, got.key, want.headless, want.key)
		}
	}

	verdicts := map[string]map[string]verdict{}
	for _, s := range signInShapes(variables) {
		providers := make([]v1.LLMProvider, 0, len(agents))
		admitted := map[string]string{}
		for _, agent := range agents {
			p := shapedProvider(namespace, v1.CLIAgentName(agent), &s)
			admitted[p.Name] = apiVerdict(t, p)
			providers = append(providers, *p)
		}
		report := askEngine(t, providers, true)
		verdicts[s.name] = map[string]verdict{}
		for i := range providers {
			p := &providers[i]
			verdicts[s.name][string(p.Spec.Agent)] = verdict{
				refusal: admitted[p.Name],
				problem: first(report.problems[p.Name]),
				warning: first(report.warnings[p.Name]),
				signIn:  report.signIns[p.Name],
			}
		}
	}

	holdVerdicts(t, verdicts)

	// The CLIs a model provider's key in env signs in are the engine's, and
	// the ones the API's rules and their cases name.
	var fromEnv []v1.CLIAgentName
	for agent, v := range verdicts[shapeEnv+"OPENROUTER_API_KEY"] {
		if v.signIn == signInEnvironment {
			fromEnv = append(fromEnv, v1.CLIAgentName(agent))
		}
	}
	slices.Sort(fromEnv)
	if want := slices.Sorted(slices.Values(envSignIn)); !slices.Equal(fromEnv, want) {
		t.Errorf("the engine signs %v in with a key in env, the API's rules %v", fromEnv, want)
	}

	signInTableFollows(t, agents, variables, fromEnv, verdicts)
}

// TestCLIAgentModelFollowsTheEngine holds the LLMProvider API's rules over a
// provider's model to the engine, which refuses a model of
// white space alone, and on every write a model with no provider segment for
// a CLI whose profile reads its model as <provider>/<model>
// (model_names_provider). Every CLI the API offers is
// tried, signed in a way the API admits for it, with models
// with and without a provider, with empty segments and with segments of white
// space, Go's and Unicode's beyond ASCII's among it: the API refuses with its
// model rules exactly the providers whose model the engine refuses. A crewlet
// change that marks another profile as naming its provider, or stops marking
// OpenCode's, fails here until the API follows it. The engine's refusal of a
// model naming no provider quotes the error OpenCode answers it with and the
// command that lists OpenCode's providers, and so do the rule's message and
// docs/guides/cli-agents.md, where a person reads why their model was
// refused: all three are held to the same two phrases.
//
// The API's rule is the engine's test, transcribed: a slash, and something
// other than white space on each side of the first one, where white space is
// what Go's strings.TrimSpace removes, as CEL's trim() is. The engine judges
// the model as the profile's model_args render it, which the API offers no
// override of, so the value the rule judges is spec.model itself.
func TestCLIAgentModelFollowsTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	agents := llmProviderEnum(t, "spec", "agent")
	variables := engineVariables(t, namespace, agents)
	models := []string{
		"claude-sonnet-5", modelViaAnthropic, modelViaOpenRouter,
		"anthropic/", "/claude-sonnet-5", "/", " anthropic/claude-sonnet-5", "anthropic /x",
		" /x", "x/ ", "x/\t", "\u00a0/x", "x/\u3000", "x/\u2028", "x/\u0085",
		// Not white space to Go: a format character and a zero-width
		// space are a provider and a model like any other.
		"\u180e/x", "x/\u200b",
		// No model at all, which the engine refuses whatever the CLI.
		"   ", "\u3000",
	}
	refusedModels := map[v1.CLIAgentName]bool{}
	quoted := false
	for _, model := range models {
		providers := make([]v1.LLMProvider, 0, len(agents))
		refused := map[string]bool{}
		refusals := map[string]string{}
		for _, agent := range agents {
			shape := admittedSignIn(v1.CLIAgentName(agent), variables)
			p := shapedProvider(namespace, v1.CLIAgentName(agent), &shape)
			p.Spec.Model = model
			refusal := apiVerdict(t, p)
			refusals[p.Name] = refusal
			refused[p.Name] = strings.Contains(refusal, msgOpenCodeModel) || strings.Contains(refusal, msgBlankModel)
			// The shape is admitted but for its model, so the model rule
			// is the question.
			if !refused[p.Name] && refusal != "" {
				t.Fatalf("%s, %s, model %q: the API refuses the sign-in shape, not the model: %s",
					agent, shape.name, model, refusal)
			}
			providers = append(providers, *p)
		}
		// The sign-in is not the question, and `crewlet llm list`
		// prints a model as it is, a tab in it splitting its row's
		// cells.
		report := askEngine(t, providers, false)
		for i := range providers {
			p := &providers[i]
			engineRefuses := len(report.models[p.Name]) > 0
			switch {
			case refused[p.Name] && !engineRefuses:
				t.Errorf("%s, model %q: the API refuses the model and the engine admits it",
					p.Spec.Agent, model)
			case !refused[p.Name] && engineRefuses:
				t.Errorf("%s, model %q: the engine refuses the model and the API admits it: %s",
					p.Spec.Agent, model, report.models[p.Name][0])
			}
			if engineRefuses {
				refusedModels[p.Spec.Agent] = true
			}
			// The rule's message quotes the engine's refusal of a
			// model naming no provider: the error OpenCode refuses it
			// with, and the command that lists OpenCode's providers.
			// Held on both sides, so an engine that rewords either
			// fails here until the message follows it.
			// A model of white space alone is refused by the blank
			// rules on both sides, whose words are their own.
			if !engineRefuses || !strings.Contains(refusals[p.Name], msgOpenCodeModel) ||
				strings.Contains(refusals[p.Name], msgBlankModel) {
				continue
			}
			quoted = true
			engineRefusal := strings.Join(report.models[p.Name], "\n")
			for _, phrase := range []string{openCodeModelRefusal, openCodeModelListing} {
				if !strings.Contains(engineRefusal, phrase) {
					t.Errorf("%s, model %q: the engine's refusal no longer says %q, which the API's "+
						"rule quotes: %s", p.Spec.Agent, model, phrase, engineRefusal)
				}
				if !strings.Contains(refusals[p.Name], phrase) {
					t.Errorf("%s, model %q: the API's refusal does not say %q, as the engine's does: %s",
						p.Spec.Agent, model, phrase, refusals[p.Name])
				}
			}
		}
	}
	// The probe can fail: the engine refuses some model of OpenCode's, and
	// the API refuses it with the rule whose message quotes the engine.
	if !refusedModels[v1.CLIAgentOpenCode] {
		t.Errorf("the engine refused no OpenCode model; the probe's models no longer reach its rule")
	}
	if !quoted {
		t.Errorf("neither side refused an OpenCode model with its model rule; the wording was held on nothing")
	}
	// The guide wraps its lines, so it is read as words.
	guide, err := os.ReadFile(filepath.Join("..", "..", "docs", "guides", "cli-agents.md"))
	if err != nil {
		t.Fatal(err)
	}
	prose := strings.Join(strings.Fields(string(guide)), " ")
	for _, phrase := range []string{openCodeModelRefusal, openCodeModelListing} {
		if !strings.Contains(prose, "`"+phrase+"`") {
			t.Errorf("docs/guides/cli-agents.md does not quote %q, as the engine's refusal of an OpenCode model "+
				"naming no provider does", phrase)
		}
	}
}

// TestCLIAgentRunInFollowsTheEngine holds the LLMProvider API's rule over a
// provider's runIn, and the CLIs the operator reads as running a seat's
// executor (render.RunnerCLIs), to the engine every suite runs. Every CLI the
// API offers is tried, signed in a way the API admits for it, with runIn
// Direct and with runIn E2B in a Company that configures both: the API
// refuses with its runIn rule exactly the providers whose run_in the engine
// refuses, which it refuses on a CLI it has no in-box runner for, and the
// CLIs whose runIn both admit are render.RunnerCLIs, on which the operator's
// executor verdicts, its NoExecutorCLI refusal and the sign-in table's column
// rest. A crewlet change that gives another CLI an in-box runner, or takes
// one away, fails here until the API, the render and the guide follow it.
func TestCLIAgentRunInFollowsTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	agents := llmProviderEnum(t, "spec", "agent")
	variables := engineVariables(t, namespace, agents)
	want := slices.Sorted(slices.Values(render.RunnerCLIs))
	for _, runIn := range []v1.SandboxPlacement{v1.SandboxPlacementDirect, v1.SandboxPlacementE2B} {
		providers := make([]v1.LLMProvider, 0, len(agents))
		refusals := map[string]string{}
		for _, agent := range agents {
			shape := admittedSignIn(v1.CLIAgentName(agent), variables)
			p := shapedProvider(namespace, v1.CLIAgentName(agent), &shape)
			p.Spec.RunIn = runIn
			refusal := apiVerdict(t, p)
			if refusal != "" && !strings.Contains(refusal, msgCLIRunIn) {
				t.Fatalf("%s, %s, runIn %s: the API refuses the provider for more than its runIn: %s",
					agent, shape.name, runIn, refusal)
			}
			refusals[p.Name] = refusal
			providers = append(providers, *p)
		}
		report := askEngine(t, providers, false)
		var runners []v1.CLIAgentName
		for i := range providers {
			p := &providers[i]
			engineRefuses := len(report.runIns[p.Name]) > 0
			switch apiRefuses := refusals[p.Name] != ""; {
			case apiRefuses && !engineRefuses:
				t.Errorf("%s, runIn %s: the API refuses the runIn and the engine admits it", p.Spec.Agent, runIn)
			case !apiRefuses && engineRefuses:
				t.Errorf("%s, runIn %s: the engine refuses the runIn and the API admits it: %s",
					p.Spec.Agent, runIn, report.runIns[p.Name][0])
			case !apiRefuses && len(report.problems[p.Name])+len(report.warnings[p.Name]) > 0:
				t.Errorf("%s, runIn %s: the API admits it and the engine finds fault with it or its seat: %q",
					p.Spec.Agent, runIn, append(report.problems[p.Name], report.warnings[p.Name]...))
			}
			if !engineRefuses {
				runners = append(runners, p.Spec.Agent)
			}
		}
		slices.Sort(runners)
		if !slices.Equal(runners, want) {
			t.Errorf("runIn %s: the engine places the runs of %v, the operator reads the CLIs that run a seat's "+
				"executor as %v (render.RunnerCLIs)", runIn, runners, want)
		}
	}
}

// TestCLIAgentIsolationFollowsTheEngine holds the LLMProvider API's rule over
// the variables env may not set, because Crewlet sets them on every call of
// the CLI to keep it inside its seat's own home and its call's working
// directory, to the engine every suite runs. The engine refuses one in a
// providers.llm entry's env naming every variable of the list for that CLI
// (crewlet's validateCredentials, over cliprofile's IsolationVariables), so
// one provider of each CLI the API offers sets HOME, which every CLI's list
// holds, and the list is read from the refusal and held to the copy the
// rule's cases are built from (homeIsolation and cliIsolation). Then every
// name any CLI's list holds is tried on every CLI: the API refuses it with
// its isolation rule exactly where the engine's list for that CLI holds it.
// The rule quotes no name beyond those lists, and the lists are also stated
// where a person reads them, in the rule's message, which the API server
// answers a refused provider with, and in the table of
// docs/guides/cli-agents.md: both are held to the engine's lists too. A
// crewlet change that gives a profile another config_env, work_config_env
// or isolation_env variable, or drops one, fails here until the rule, its
// message and the guide follow it.
func TestCLIAgentIsolationFollowsTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	agents := llmProviderEnum(t, "spec", "agent")
	beside := map[v1.CLIAgentName]v1.CLIAgentAuth{}
	providers := make([]v1.LLMProvider, 0, len(agents))
	for _, agent := range agents {
		name := v1.CLIAgentName(agent)
		beside[name] = loginBeside(t, namespace, name)
		shape := signInShape{name: "env HOME", auth: beside[name], env: []string{"HOME"}}
		providers = append(providers, *shapedProvider(namespace, name, &shape))
	}
	report := askEngine(t, providers, false)

	engineLists := map[v1.CLIAgentName][]string{}
	var everyName []string
	for i := range providers {
		p := &providers[i]
		var lists [][]string
		for _, problem := range report.problems[p.Name] {
			if m := isolationRefusal.FindStringSubmatch(problem); m != nil {
				lists = append(lists, strings.Split(m[1], ", "))
			}
		}
		if len(lists) != 1 {
			t.Fatalf("%s: want one problem matching %q, got %q", p.Spec.Agent, isolationRefusal, report.problems[p.Name])
		}
		got := slices.Sorted(slices.Values(lists[0]))
		engineLists[p.Spec.Agent] = got
		want := slices.Sorted(slices.Values(append(slices.Clone(homeIsolation), cliIsolation[p.Spec.Agent]...)))
		if !slices.Equal(got, want) {
			t.Errorf("%s: the engine sets %v on every call to isolate the CLI, the API's rule refuses %v in env",
				p.Spec.Agent, got, want)
		}
		for _, name := range got {
			if !slices.Contains(everyName, name) {
				everyName = append(everyName, name)
			}
		}
	}
	slices.Sort(everyName)

	for _, agent := range agents {
		name := v1.CLIAgentName(agent)
		for _, variable := range everyName {
			shape := signInShape{name: "env " + variable, auth: beside[name], env: []string{variable}}
			refusal := apiVerdict(t, shapedProvider(namespace, name, &shape))
			switch engineRefuses := slices.Contains(engineLists[name], variable); {
			case engineRefuses && !strings.Contains(refusal, msgCLIIsolation):
				t.Errorf("%s with %s in env: the engine refuses it and the API's isolation rule does not: %q",
					agent, variable, refusal)
			case !engineRefuses && refusal != "":
				t.Errorf("%s with %s in env: the engine sets no such variable for it and the API refuses it: %s",
					agent, variable, refusal)
			}
		}
	}

	// The probe above tries the engine's names alone, so a name the rule
	// refuses that no list holds, a provider the API refuses and the engine
	// runs, is read off the rule instead.
	rule := validationOf(t, "llmproviders.crewlet.ai", msgCLIIsolation, "spec")
	quoted := quotedVariable.FindAllStringSubmatch(rule.Rule, -1)
	named := make([]string, 0, len(quoted))
	for _, m := range quoted {
		named = append(named, m[1])
	}
	if got := slices.Compact(slices.Sorted(slices.Values(named))); !slices.Equal(got, everyName) {
		t.Errorf("the API's isolation rule refuses %v in env, the variables of the engine's lists are %v", got, everyName)
	}
	isolationMessageFollows(t, rule.Message, agents, engineLists)
	isolationTableFollows(t, agents, engineLists)
}

// TestCLIAgentZoneFollowsTheEngine holds the LLMProvider API's refusal of TZ
// in env to the engine every suite runs, which pins the zone of every CLI it
// starts and refuses TZ in a providers.llm entry's env for every CLI: each CLI
// the API offers is tried with TZ, and with tz, which names no zone, in env
// beside the login it is admitted with (loginBeside), so the variable is the
// only question. The API refuses TZ with its zone rule exactly where the
// engine refuses it, and admits tz where the engine does.
func TestCLIAgentZoneFollowsTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	agents := llmProviderEnum(t, "spec", "agent")
	providers := make([]v1.LLMProvider, 0, 2*len(agents))
	for _, variable := range []string{"TZ", "tz"} {
		for _, agent := range agents {
			name := v1.CLIAgentName(agent)
			shape := signInShape{name: "env " + variable, auth: loginBeside(t, namespace, name), env: []string{variable}}
			p := shapedProvider(namespace, name, &shape)
			p.Name = fmt.Sprintf("%s-%d", p.Name, len(providers))
			providers = append(providers, *p)
		}
	}
	report := askEngine(t, providers, false)
	for i := range providers {
		p := &providers[i]
		variable := p.Spec.Env[0].Name
		engineRefuses := slices.ContainsFunc(report.problems[p.Name], func(problem string) bool {
			return strings.Contains(problem, variable+zoneRefusal)
		})
		if want := variable == "TZ"; engineRefuses != want {
			t.Errorf("%s with %s in env: the engine refuses it as the zone = %v, want %v: %q",
				p.Spec.Agent, variable, engineRefuses, want, report.problems[p.Name])
		}
		refusal := apiVerdict(t, p)
		switch {
		case engineRefuses && !strings.Contains(refusal, msgCLIZone):
			t.Errorf("%s with %s in env: the engine refuses it and the API's zone rule does not: %q",
				p.Spec.Agent, variable, refusal)
		case !engineRefuses && refusal != "":
			t.Errorf("%s with %s in env: the engine admits it and the API refuses it: %s",
				p.Spec.Agent, variable, refusal)
		}
	}
}

// zoneRefusal follows TZ in the engine's refusal of it in a providers.llm
// entry's env (crewlet's internal/config, validateCredentials).
const zoneRefusal = " is the zone the engine hands every CLI it starts"

// TestCLIAgentSubscriptionEnvFollowsTheEngine holds the LLMProvider API's
// refusal of a subscription variable in env to the engine: the variables each
// CLI's profile sets under auth.mode subscription alone (subscription_env),
// read from the profiles of the engine every suite runs, are the copy the
// rule's cases are built from and the names the rule quotes; and for every CLI
// and every such variable, the engine refuses it in a Subscription provider's
// env exactly where the API does. A profile that gains one fails here until
// the API follows it.
func TestCLIAgentSubscriptionEnvFollowsTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	agents := llmProviderEnum(t, "spec", "agent")

	source, err := os.OpenRoot(enginetest.Source(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	raw, err := source.ReadFile(engineProfiles)
	if err != nil {
		t.Fatal(err)
	}
	var profiles map[string]struct {
		SubscriptionEnv map[string]string `json:"subscription_env"`
	}
	if err := yaml.Unmarshal(raw, &profiles); err != nil {
		t.Fatalf("%s: %v", engineProfiles, err)
	}
	engineLists := map[v1.CLIAgentName][]string{}
	var everyName []string
	for profile, p := range profiles {
		if len(p.SubscriptionEnv) == 0 {
			continue
		}
		agent, ok := agentOfProfile(agents, profile)
		if !ok {
			t.Errorf("the engine's %s profile sets %v under subscription, and no agent the API offers is it",
				profile, slices.Sorted(maps.Keys(p.SubscriptionEnv)))
			continue
		}
		engineLists[agent] = slices.Sorted(maps.Keys(p.SubscriptionEnv))
		everyName = append(everyName, engineLists[agent]...)
	}
	everyName = slices.Compact(slices.Sorted(slices.Values(everyName)))
	if !maps.EqualFunc(engineLists, subscriptionEnv, slices.Equal) {
		t.Errorf("the engine's profiles set %v under subscription, the API's rule refuses %v", engineLists, subscriptionEnv)
	}
	rule := validationOf(t, "llmproviders.crewlet.ai", msgCLISubscriptionEnv, "spec")
	quoted := quotedVariable.FindAllStringSubmatch(rule.Rule, -1)
	named := make([]string, 0, len(quoted))
	for _, m := range quoted {
		named = append(named, m[1])
	}
	if got := slices.Compact(slices.Sorted(slices.Values(named))); !slices.Equal(got, everyName) {
		t.Errorf("the API's subscription rule refuses %v in env, the engine's profiles set %v", got, everyName)
	}

	// Every CLI with every such variable, beside the login it is admitted
	// with (loginBeside), so the variable is the only question.
	providers := make([]v1.LLMProvider, 0, len(agents)*len(everyName))
	for _, variable := range everyName {
		for _, agent := range agents {
			name := v1.CLIAgentName(agent)
			shape := signInShape{name: "env " + variable, auth: loginBeside(t, namespace, name), env: []string{variable}}
			p := shapedProvider(namespace, name, &shape)
			p.Name = fmt.Sprintf("%s-%d", p.Name, len(providers))
			providers = append(providers, *p)
		}
	}
	report := askEngine(t, providers, false)
	for i := range providers {
		p := &providers[i]
		variable := p.Spec.Env[0].Name
		engineRefuses := slices.ContainsFunc(report.problems[p.Name], func(problem string) bool {
			return strings.Contains(problem, variable+subscriptionRefusal)
		})
		if want := slices.Contains(engineLists[p.Spec.Agent], variable); engineRefuses != want {
			t.Errorf("%s with %s in env under subscription: the engine refuses it = %v, its profile sets it = %v: %q",
				p.Spec.Agent, variable, engineRefuses, want, report.problems[p.Name])
		}
		refusal := apiVerdict(t, p)
		switch {
		case engineRefuses && !strings.Contains(refusal, msgCLISubscriptionEnv):
			t.Errorf("%s with %s in env: the engine refuses it under subscription and the API's rule does not: %q",
				p.Spec.Agent, variable, refusal)
		case !engineRefuses && refusal != "":
			t.Errorf("%s with %s in env: the engine sets no such variable for it and the API refuses it: %s",
				p.Spec.Agent, variable, refusal)
		}
	}
}

// TestCLIAgentSignInEnvFollowsTheEngine holds the LLMProvider API's rules over
// auth mode Env to the engine: the variables each CLI's profile lists as
// signing it in from a provider's env (sign_in_env), read from the profiles of
// the engine every suite runs, are the copy the rules' cases are built from
// and the names both rules quote, the one that requires one of them in env
// under Env and the one that refuses each in env under every other mode. Then
// every CLI is tried with every such name in env under each mode it may take:
// beside the login it is admitted with (loginBeside), beside a key where it
// has a key variable, and under Env. The engine refuses the name under a mode
// other than Env exactly where the profile lists it and the API's rule
// refuses it, signs the CLI in with env under Env where the profile lists it,
// and admits it under Env exactly where the API does. A profile that gains such a name, or loses one, fails here until
// the API follows it.
func TestCLIAgentSignInEnvFollowsTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	agents := llmProviderEnum(t, "spec", "agent")

	source, err := os.OpenRoot(enginetest.Source(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	raw, err := source.ReadFile(engineProfiles)
	if err != nil {
		t.Fatal(err)
	}
	var profiles map[string]struct {
		SignInEnv []string `json:"sign_in_env"`
	}
	if err := yaml.Unmarshal(raw, &profiles); err != nil {
		t.Fatalf("%s: %v", engineProfiles, err)
	}
	engineLists := map[v1.CLIAgentName][]string{}
	var everyName []string
	for profile, p := range profiles {
		if len(p.SignInEnv) == 0 {
			continue
		}
		agent, ok := agentOfProfile(agents, profile)
		if !ok {
			t.Errorf("the engine's %s profile signs its CLI in from env by %v, and no agent the API offers is it",
				profile, p.SignInEnv)
			continue
		}
		engineLists[agent] = p.SignInEnv
		everyName = append(everyName, p.SignInEnv...)
	}
	everyName = slices.Compact(slices.Sorted(slices.Values(everyName)))
	if !maps.EqualFunc(engineLists, signInEnv, slices.Equal) {
		t.Errorf("the engine's profiles sign their CLIs in from env by %v, the API's rules by %v", engineLists, signInEnv)
	}
	for _, message := range []string{msgCLIEnvUnset, msgCLIEnvOnly} {
		rule := validationOf(t, "llmproviders.crewlet.ai", message, "spec")
		named := make([]string, 0, len(everyName))
		for _, m := range quotedVariable.FindAllStringSubmatch(rule.Rule, -1) {
			named = append(named, m[1])
		}
		if got := slices.Compact(slices.Sorted(slices.Values(named))); !slices.Equal(got, everyName) {
			t.Errorf("the API's rule %q quotes %v, the engine's profiles sign their CLIs in from env by %v",
				message, got, everyName)
		}
	}

	metered := v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey, APIKeySecretRef: secretKey("cli", apiKeyKey)}
	providers := make([]v1.LLMProvider, 0, 3*len(agents)*len(everyName))
	for _, variable := range everyName {
		for _, agent := range agents {
			name := v1.CLIAgentName(agent)
			auths := []v1.CLIAgentAuth{loginBeside(t, namespace, name), {Mode: v1.CLIAgentAuthEnv}}
			if cliVariables[name].key != "" {
				auths = append(auths, metered)
			}
			for _, auth := range auths {
				shape := signInShape{name: "env " + variable, auth: auth, env: []string{variable}}
				p := shapedProvider(namespace, name, &shape)
				p.Name = fmt.Sprintf("%s-%d", p.Name, len(providers))
				providers = append(providers, *p)
			}
		}
	}
	report := askEngine(t, providers, true)
	for i := range providers {
		p := &providers[i]
		variable, mode := p.Spec.Env[0].Name, p.Spec.Auth.Mode
		listed := slices.Contains(engineLists[p.Spec.Agent], variable)
		refusal := apiVerdict(t, p)
		if mode == v1.CLIAgentAuthEnv {
			// Under auth.mode env the engine reports every CLI it admits
			// signed in with env, an env sign-in CLI that reads a
			// credential-named name as its model provider's key included.
			engineAdmits := len(report.problems[p.Name]) == 0 && len(report.warnings[p.Name]) == 0 &&
				report.signIns[p.Name] == signInEnvMode
			if listed && !engineAdmits {
				t.Errorf("%s with %s in env under Env: its profile lists it, and the engine refuses it or reports it "+
					"signed in with %q: %q", p.Spec.Agent, variable, report.signIns[p.Name], report.problems[p.Name])
			}
			if apiAdmits := refusal == ""; apiAdmits != engineAdmits {
				t.Errorf("%s with %s in env under Env: the engine admits it = %v, the API = %v: %s",
					p.Spec.Agent, variable, engineAdmits, apiAdmits, refusal)
			}
			continue
		}
		engineRefuses := slices.ContainsFunc(report.problems[p.Name], func(problem string) bool {
			return strings.Contains(problem, variable+signInEnvRefusal)
		})
		if engineRefuses != listed {
			t.Errorf("%s with %s in env under %s: the engine refuses it = %v, its profile lists it = %v: %q",
				p.Spec.Agent, variable, cmp.Or(mode, v1.CLIAgentAuthSubscription), engineRefuses, listed, report.problems[p.Name])
		}
		switch {
		case engineRefuses && !strings.Contains(refusal, msgCLIEnvOnly):
			t.Errorf("%s with %s in env under %s: the engine refuses it and the API's rule does not: %q",
				p.Spec.Agent, variable, cmp.Or(mode, v1.CLIAgentAuthSubscription), refusal)
		case !engineRefuses && refusal != "":
			t.Errorf("%s with %s in env under %s: the engine admits it and the API refuses it: %s",
				p.Spec.Agent, variable, cmp.Or(mode, v1.CLIAgentAuthSubscription), refusal)
		}
	}
}

// signInEnvRefusal follows the variable in the engine's refusal of one its
// profile signs the CLI in by from env, under a mode other than env
// (crewlet's internal/config, validateEnvSignIn).
const signInEnvRefusal = ` signs the "`

// loginBeside is the sign-in a probe of a variable in env runs beside: a
// bundle, a Subscription login that leaves the variable the only question,
// for every CLI the API admits one for, and for a CLI that keeps no login
// (QwenCode), which the API refuses a bundle, the sign-in it is admitted with.
func loginBeside(t *testing.T, namespace string, agent v1.CLIAgentName) v1.CLIAgentAuth {
	t.Helper()
	bundle := signInShape{name: shapeBundle, auth: v1.CLIAgentAuth{CredentialBundleSecretRef: secretKey("cli", "bundle")}}
	if apiVerdict(t, shapedProvider(namespace, agent, &bundle)) == "" {
		return bundle.auth
	}
	return admittedSignIn(agent, cliVariables).auth
}

// engineProfiles is the engine's table of CLI profiles.
const engineProfiles = "internal/providers/llm/cliagent/cliprofile/profiles.yaml"

// subscriptionRefusal follows the variable in the engine's refusal of one its
// profile sets under subscription (crewlet's internal/config,
// validateCredentials).
const subscriptionRefusal = " is set by the "

// agentOfProfile is the agent of agents the engine's profile named profile
// drives: the same words, the profile's in kebab case and the agent's in
// PascalCase (claude-code is ClaudeCode).
func agentOfProfile(agents []string, profile string) (v1.CLIAgentName, bool) {
	for _, agent := range agents {
		if strings.EqualFold(strings.ReplaceAll(profile, "-", ""), agent) {
			return v1.CLIAgentName(agent), true
		}
	}
	return "", false
}

// quotedVariable is a variable's name quoted in a CEL rule: upper case, so
// never a CLI's name, which is written in mixed case.
var quotedVariable = regexp.MustCompile(`'([A-Z][A-Z0-9_]*)'`)

// validationOf is the validation the installed CRD crd declares on the field
// at path whose message starts with message.
func validationOf(t *testing.T, crd, message string, path ...string) apiextensionsv1.ValidationRule {
	t.Helper()
	for _, c := range installedCRDs {
		if c.Name != crd {
			continue
		}
		node := c.Spec.Versions[0].Schema.OpenAPIV3Schema
		for _, field := range path {
			child, ok := node.Properties[field]
			if !ok {
				t.Fatalf("%s has no field %s", crd, strings.Join(path, "."))
			}
			node = &child
		}
		for _, rule := range node.XValidations {
			if strings.HasPrefix(rule.Message, message) {
				return rule
			}
		}
		t.Fatalf("%s %s declares no rule whose message starts %q", crd, strings.Join(path, "."), message)
	}
	t.Fatalf("no CRD %s was installed", crd)
	return apiextensionsv1.ValidationRule{}
}

// isolationMessageFollows holds the isolation rule's message to the engine's
// lists. Its first sentence names, after its colon, the variables of every
// CLI ahead of "for any CLI", and then each CLI's own after "<CLI>'s", so a
// CLI's list as the message states it is the first group and its own.
func isolationMessageFollows(t *testing.T, message string, agents []string, lists map[v1.CLIAgentName][]string) {
	t.Helper()
	_, named, colon := strings.Cut(message, ": ")
	named, _, _ = strings.Cut(named, ". ")
	every, own, group := strings.Cut(named, " for any CLI")
	if !colon || !group || len(variableName.FindAllString(every, -1)) == 0 {
		t.Fatalf("the API's isolation rule's message names no variables for every CLI ahead of %q: %s",
			" for any CLI", message)
	}
	stated := map[v1.CLIAgentName][]string{}
	for _, agent := range agents {
		stated[v1.CLIAgentName(agent)] = variableName.FindAllString(every, -1)
	}
	marks := possessive.FindAllStringSubmatchIndex(own, -1)
	if len(marks) == 0 || len(variableName.FindAllString(own[:marks[0][0]], -1)) > 0 {
		t.Errorf("the API's isolation rule's message names variables of no CLI after %q: %s", " for any CLI", message)
	}
	for i, m := range marks {
		agent, end := own[m[2]:m[3]], len(own)
		if i+1 < len(marks) {
			end = marks[i+1][0]
		}
		if !slices.Contains(agents, agent) {
			t.Errorf("the API's isolation rule's message names variables of %s, which the API does not offer: %s",
				agent, message)
			continue
		}
		stated[v1.CLIAgentName(agent)] = append(stated[v1.CLIAgentName(agent)],
			variableName.FindAllString(own[m[1]:end], -1)...)
	}
	for _, agent := range agents {
		name := v1.CLIAgentName(agent)
		got, want := slices.Sorted(slices.Values(stated[name])), slices.Sorted(slices.Values(lists[name]))
		if !slices.Equal(got, want) {
			t.Errorf("the API's isolation rule's message says env may not set %v for %s, the engine refuses %v",
				got, agent, want)
		}
	}
}

// possessive is a CLI's name ahead of the variables a message names for it,
// as in "Codex's CODEX_HOME".
var possessive = regexp.MustCompile(`\b([A-Z][A-Za-z]+)'s `)

// variableName is an environment variable's name in prose: upper case words
// joined by underscores.
var variableName = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)*\b`)

// isolationTableFollows holds the table docs/guides/cli-agents.md lists the
// variables env may not set in to the engine's lists: a row of the variables
// of every CLI, and a row of each CLI's own, every CLI the API offers covered.
func isolationTableFollows(t *testing.T, agents []string, lists map[v1.CLIAgentName][]string) {
	t.Helper()
	const guide = "docs/guides/cli-agents.md"
	rows, err := docs.Table(filepath.Join("..", "..", guide), "`env` may not set")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s has no table headed %q", guide, "`env` may not set")
	}
	stated := map[v1.CLIAgentName][]string{}
	for _, row := range rows {
		names := codeSpans(row[0])
		if row[1] == "every CLI" {
			for _, agent := range agents {
				stated[v1.CLIAgentName(agent)] = append(stated[v1.CLIAgentName(agent)], names...)
			}
			continue
		}
		for _, agent := range codeSpans(row[1]) {
			if !slices.Contains(agents, agent) {
				t.Errorf("%s: the isolation table names %s, which the API does not offer", guide, agent)
			}
			stated[v1.CLIAgentName(agent)] = append(stated[v1.CLIAgentName(agent)], names...)
		}
	}
	for _, agent := range agents {
		name := v1.CLIAgentName(agent)
		got, want := slices.Sorted(slices.Values(stated[name])), slices.Sorted(slices.Values(lists[name]))
		if !slices.Equal(got, want) {
			t.Errorf("%s: the isolation table says env may not set %v for %s, the engine refuses %v",
				guide, got, agent, want)
		}
	}
}

// codeSpan is a Markdown code span.
var codeSpan = regexp.MustCompile("`([^`]+)`")

// codeSpans is the text of every code span in cell, in order.
func codeSpans(cell string) []string {
	matches := codeSpan.FindAllStringSubmatch(cell, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// isolationRefusal is the engine's refusal of a variable in a providers.llm
// entry's env that it sets on every call of the CLI, naming every such
// variable for that CLI (crewlet's internal/config, validateCredentials):
// here, of HOME.
var isolationRefusal = regexp.MustCompile(`HOME is a variable the engine sets for every call of this CLI to ` +
	`keep it inside its seat's own home and its call's working directory \(([A-Za-z0-9_, ]+)\)`)

// admittedSignIn is a sign-in the API admits for agent, one of signInShapes:
// its headless token, its key, its model provider's key in env, or a bundle,
// the first it has.
func admittedSignIn(agent v1.CLIAgentName, variables map[v1.CLIAgentName]cliVariable) signInShape {
	name := shapeBundle
	switch v := variables[agent]; {
	case v.headless != "":
		name = shapeToken
	case v.key != "":
		name = shapeAPIKey
	case slices.Contains(envSignIn, agent):
		name = shapeEnv + "OPENROUTER_API_KEY"
	}
	shapes := signInShapes(variables)
	return shapes[slices.IndexFunc(shapes, func(s signInShape) bool { return s.name == name })]
}

// holdVerdicts holds the API server's verdict on each shape to the engine's.
func holdVerdicts(t *testing.T, verdicts map[string]map[string]verdict) {
	t.Helper()
	for shape, byAgent := range verdicts {
		for agent, v := range byAgent {
			admitted := v.refusal == ""
			// A bundle signs the CLI in from the login the engine restores
			// from it (the CLI's credential files, or the document
			// OpenCode's export printed). `llm list` restores the configured
			// bundle before it reports, as the engine does, but the probe's
			// is a placeholder no restore can read, so the column cannot
			// show it: a bundle that restored would be written under the
			// rendered state directory, in /var/lib/crewlet of the machine
			// running the test.
			signedIn := slices.Contains([]string{"token", "api key", signInEnvironment, signInEnvMode}, v.signIn) ||
				strings.HasPrefix(shape, shapeBundle)
			// What the API admits the engine admits, without a doubt, so a
			// revision is refused on the object rather than at the apply.
			if admitted && v.problem != "" {
				t.Errorf("%s, %s: the API admits it and the engine refuses it: %s", agent, shape, v.problem)
			}
			if admitted && v.warning != "" {
				t.Errorf("%s, %s: the API admits it and the engine warns: %s", agent, shape, v.warning)
			}
			// What the API admits signs the CLI in.
			if admitted && !signedIn {
				t.Errorf("%s, %s: the API admits it and the engine reports it signed in with %q", agent, shape, v.signIn)
			}
			// What the engine admits and signs in, the API admits.
			engineAdmits := v.problem == "" && v.warning == ""
			if !admitted && engineAdmits && signedIn {
				t.Errorf("%s, %s: the engine admits it, signed in with %q, and the API refuses it: %s",
					agent, shape, v.signIn, v.refusal)
			}
		}
	}
}

// signInTableFollows holds the table docs/guides/cli-agents.md states what
// each CLI takes in to what the engine and the API said: a row for every CLI
// the API offers, naming its token and key variable as the engine's profile
// does, a key in env where the engine signs the CLI in from one, under auth
// mode Env the names the engine signs it in by there (its profile's
// sign_in_env), a key in env where it signs it in from one there instead and
// no where it signs it in from nothing there, a bundle where the API admits
// one, and that it runs a seat's executor where it is one of
// render.RunnerCLIs, which TestCLIAgentRunInFollowsTheEngine holds to the
// engine.
func signInTableFollows(t *testing.T, agents []string, variables map[v1.CLIAgentName]cliVariable,
	fromEnv []v1.CLIAgentName, verdicts map[string]map[string]verdict) {
	t.Helper()
	const guide = "docs/guides/cli-agents.md"
	rows, err := docs.Table(filepath.Join("..", "..", guide), "`agent`")
	if err != nil {
		t.Fatal(err)
	}
	// variable is the variable a cell names, its first code span, or ""
	// for a cell that says no.
	variable := func(cell string) string {
		if strings.HasPrefix(cell, "no") {
			return ""
		}
		_, rest, _ := strings.Cut(cell, "`")
		name, _, _ := strings.Cut(rest, "`")
		return name
	}
	yes := func(cell string) bool { return !strings.HasPrefix(cell, "no") }
	// envMode is what the engine signs agent in by under auth mode Env, which
	// it reports as env whatever the CLI: a key in env where it admits a
	// model provider's key, the names one of which it admits where it does
	// not, and no where it admits neither.
	envMode := func(agent string) string {
		if v := verdicts[shapeEnvMode+"OPENROUTER_API_KEY"][agent]; v.problem == "" && v.signIn == signInEnvMode {
			return "a key in `env`"
		}
		var names []string
		for shape, byAgent := range verdicts {
			name, ok := strings.CutPrefix(shape, shapeEnvMode)
			if v := byAgent[agent]; ok && v.refusal == "" && v.problem == "" && v.signIn == signInEnvMode {
				names = append(names, "`"+name+"`")
			}
		}
		if len(names) > 0 {
			slices.Sort(names)
			return strings.Join(names, ", ")
		}
		return "no"
	}
	// stated is what a cell of that column says: no, a key in env, or the
	// names its code spans quote, sorted, but env itself.
	stated := func(cell string) string {
		switch {
		case strings.HasPrefix(cell, "no"):
			return "no"
		case strings.HasPrefix(cell, "a key in"):
			return cell
		}
		var quoted []string
		for _, span := range codeSpans(cell) {
			if span != "env" {
				quoted = append(quoted, "`"+span+"`")
			}
		}
		slices.Sort(quoted)
		return strings.Join(quoted, ", ")
	}
	seen := map[string]bool{}
	for _, row := range rows {
		agent := strings.Trim(row[0], "`")
		if !slices.Contains(agents, agent) {
			t.Errorf("%s: the sign-in table has a row for %s, which the API does not offer", guide, agent)
			continue
		}
		seen[agent] = true
		want := variables[v1.CLIAgentName(agent)]
		for _, c := range []struct {
			column string
			got    any
			want   any
		}{
			{"tokenSecretRef", variable(row[1]), want.headless},
			{"auth.mode: APIKey", variable(row[2]), want.key},
			{"a key in env", yes(row[3]), slices.Contains(fromEnv, v1.CLIAgentName(agent))},
			{"auth.mode: Env signs in with", stated(row[4]), envMode(agent)},
			{"credentialBundleSecretRef", yes(row[5]), verdicts[shapeBundle][agent].refusal == ""},
			{"runs a seat's executor", yes(row[6]), slices.Contains(render.RunnerCLIs, v1.CLIAgentName(agent))},
		} {
			if c.got != c.want {
				t.Errorf("%s: the sign-in table's %s cell for %s says %v, want %v", guide, c.column, agent, c.got, c.want)
			}
		}
	}
	for _, agent := range agents {
		if !seen[agent] {
			t.Errorf("%s: the sign-in table has no row for %s", guide, agent)
		}
	}
}

// engineVariables reads from the engine the token and key variable
// its profile for each of agents names. One provider of each CLI asks for
// both, in auth mode APIKey with a token, which the engine refuses naming
// the token variable it would have removed or saying the profile names none,
// and with every key variable the API's rules name set in env, of which the
// engine refuses the one auth owns for that CLI, or says the profile names no
// key variable for auth mode APIKey to fill. A profile that gains a key
// variable no rule names answers neither, which fails the read.
func engineVariables(t *testing.T, namespace string, agents []string) map[v1.CLIAgentName]cliVariable {
	t.Helper()
	probe := signInShape{auth: v1.CLIAgentAuth{
		Mode:            v1.CLIAgentAuthAPIKey,
		TokenSecretRef:  secretKey("cli", "token"),
		APIKeySecretRef: secretKey("cli", apiKeyKey),
	}}
	for _, v := range cliVariables {
		if v.key != "" && !slices.Contains(probe.env, v.key) {
			probe.env = append(probe.env, v.key)
		}
	}
	slices.Sort(probe.env)
	providers := make([]v1.LLMProvider, 0, len(agents))
	for _, agent := range agents {
		providers = append(providers, *shapedProvider(namespace, v1.CLIAgentName(agent), &probe))
	}
	report := askEngine(t, providers, true)
	// Each says what one problem means: the variable it names, or none.
	read := func(problems []string, names, none *regexp.Regexp) (string, error) {
		var found []string
		for _, p := range problems {
			if m := names.FindStringSubmatch(p); m != nil {
				found = append(found, m[1])
			} else if none.MatchString(p) {
				found = append(found, "")
			}
		}
		if len(found) != 1 {
			return "", fmt.Errorf("want one problem matching %q or %q, got %q", names, none, problems)
		}
		return found[0], nil
	}
	out := map[v1.CLIAgentName]cliVariable{}
	for i := range providers {
		p := &providers[i]
		problems := report.problems[p.Name]
		key, err := read(problems, keyVariable, noKeyVariable)
		if err != nil {
			t.Fatalf("%s: reading its key variable: %v", p.Spec.Agent, err)
		}
		token, err := read(problems, tokenVariable, noTokenVariable)
		if err != nil {
			t.Fatalf("%s: reading its token variable: %v", p.Spec.Agent, err)
		}
		if key != "" || token != "" {
			out[p.Spec.Agent] = cliVariable{headless: token, key: key}
		}
	}
	return out
}

// The engine's refusals of a token and of a key a profile has or lacks the
// variable for (crewlet's internal/config, validateCredentials).
var (
	keyVariable   = regexp.MustCompile(`([A-Za-z0-9_]+) is this CLI's key variable, which auth owns`)
	noKeyVariable = regexp.MustCompile(`profile names none \(api_key_env\)`)
	tokenVariable = regexp.MustCompile(
		`a token is read only under auth\.mode subscription: api-key removes ([A-Za-z0-9_]+) from`)
	noTokenVariable = regexp.MustCompile(`profile names no token_env`)
)

// first is the first of messages, or "".
func first(messages []string) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[0]
}

// verdict is what the API server and the engine said of one provider.
type verdict struct {
	// refusal is the API server's message, empty when it admitted it.
	refusal string
	// problem is the engine's first problem with the provider or the seat
	// it drives, empty when it admitted both.
	problem string
	// warning is the engine's first warning about either, empty when it
	// has none.
	warning string
	// signIn is the SIGN-IN column of `crewlet llm list`.
	signIn string
}

// The SIGN-IN words of `crewlet llm list` the suite reads beside its token
// and its key: a CLI signed in by a model provider's key in its environment
// (env_sign_in) under auth.mode subscription, and any CLI signed in by its env
// under auth.mode env.
const (
	signInEnvironment = "environment"
	signInEnvMode     = "env"
)

// runnerProbe is the provider askEngine runs a seat's executor on when the
// provider the seat probes is of a CLI Crewlet runs no executor on: Claude
// Code on a key, which the engine admits.
const runnerProbe = "probe-runner"

// The names of the sign-in shapes the API's cases are built on, and the start
// of the name of each shape that signs a subscription in with one variable in
// env.
const (
	shapeAPIKey  = "api key"
	shapeToken   = "token"
	shapeBundle  = "bundle"
	shapeEnv     = "subscription, env "
	shapeEnvMode = "env mode, env "
)

// signInShape is one way to sign a CLI in.
type signInShape struct {
	name string
	auth v1.CLIAgentAuth
	// env is the names the provider's env sets.
	env []string
}

// signInShapes are the ways a CLI may be signed in that the API can express:
// a key, a token, a bundle, and the names env may carry, under Subscription
// and under auth mode Env, where env is the whole sign-in. The names are probe
// input, not a list of what any CLI reads: names with each part the engine's
// docs call a credential, names that only hold such a part inside a longer
// word, one in lower case, and the names a profile signs its CLI in by from
// env (signInEnv); and, beside a bundle so the variable is the only question,
// every token and key variable the engine's profiles name (variables), those
// names of signInEnv, also beside a key, and one that is no credential.
// Whether each signs a CLI in is the engine's answer.
func signInShapes(variables map[v1.CLIAgentName]cliVariable) []signInShape {
	inEnv := []string{
		"OPENROUTER_API_KEY", "openrouter_api_key", "GEMINI_API_KEYS", "VENDOR_APIKEY",
		"AWS_ACCESSKEY", "AWS_SECRETKEY", "GH_TOKEN", "OAUTH2_TOKEN", "DB_SECRET",
		"DB_PASSWORD", "DB_PASSWD", "GCP_CREDENTIAL", "GCP_CREDENTIALS", "PROXY_AUTH",
		"VENDOR_OAUTH", "GITLAB_PAT",
		envProxy, "HERMES_MAX_TOKENS", "MY_KEY2", "MONKEY", "KEYRING_PATH", "AUTHOR",
	}
	var signIns []string
	for _, names := range signInEnv {
		signIns = append(signIns, names...)
	}
	slices.Sort(signIns)
	besideBundle := []string{envProxy}
	for _, v := range variables {
		for _, name := range []string{v.headless, v.key} {
			if name != "" && !slices.Contains(besideBundle, name) {
				besideBundle = append(besideBundle, name)
			}
		}
	}
	slices.Sort(besideBundle[1:])
	besideBundle = append(besideBundle, signIns...)
	bundle := v1.CLIAgentAuth{CredentialBundleSecretRef: secretKey("cli", "bundle")}
	metered := v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey, APIKeySecretRef: secretKey("cli", apiKeyKey)}
	shapes := make([]signInShape, 0, 4+2*len(inEnv)+len(besideBundle)+2*len(signIns))
	shapes = append(shapes,
		signInShape{name: shapeAPIKey, auth: metered},
		signInShape{name: shapeToken, auth: v1.CLIAgentAuth{TokenSecretRef: secretKey("cli", "token")}},
		signInShape{name: shapeBundle, auth: bundle},
		signInShape{name: "env mode, no env", auth: v1.CLIAgentAuth{Mode: v1.CLIAgentAuthEnv}},
	)
	for _, name := range inEnv {
		shapes = append(shapes, signInShape{
			name: shapeEnv + name,
			auth: v1.CLIAgentAuth{Mode: v1.CLIAgentAuthSubscription},
			env:  []string{name},
		})
	}
	for _, name := range slices.Concat(inEnv, signIns) {
		shapes = append(shapes, signInShape{
			name: shapeEnvMode + name,
			auth: v1.CLIAgentAuth{Mode: v1.CLIAgentAuthEnv},
			env:  []string{name},
		})
	}
	for _, name := range besideBundle {
		shapes = append(shapes, signInShape{name: "bundle, env " + name, auth: bundle, env: []string{name}})
	}
	for _, name := range signIns {
		shapes = append(shapes, signInShape{name: "api key, env " + name, auth: metered, env: []string{name}})
	}
	return shapes
}

// shapedProvider is a provider of agent, signed in as s, named for its agent
// so a document can hold one of each.
func shapedProvider(namespace string, agent v1.CLIAgentName, s *signInShape) *v1.LLMProvider {
	p := llmProvider(namespace, strings.ToLower(string(agent)))
	p.Spec.Agent = agent
	p.Spec.Model = "probe/probe-model"
	p.Spec.Auth = s.auth
	for _, name := range s.env {
		p.Spec.Env = append(p.Spec.Env, v1.SecretEnvVar{Name: name, ValueFrom: fromSecret("cli", "env")})
	}
	return p
}

// apiVerdict is the API server's refusal of obj, empty when it admits it. The
// create is a dry run, so a document's objects can share their names.
func apiVerdict(t *testing.T, obj client.Object) string {
	t.Helper()
	err := k8sClient.Create(ctx, obj.DeepCopyObject().(client.Object), client.DryRunAll)
	switch {
	case err == nil:
		return ""
	case apierrors.IsInvalid(err):
		return err.Error()
	default:
		t.Fatalf("creating %s: %v", obj.GetName(), err)
		return ""
	}
}

// engineReport is what the engine said of a document's providers,
// each by its name.
type engineReport struct {
	// problems are what `crewlet validate` refused in the provider or in
	// the seat it drives.
	problems map[string][]string
	// warnings are what it doubted in either.
	warnings map[string][]string
	// models are the problems it found with the provider's model, and
	// runIns with its run_in.
	models, runIns map[string][]string
	// signIns are the SIGN-IN column of `crewlet llm list`.
	signIns map[string]string
}

// askEngine renders providers into one company document, as the operator
// would apply it, each on an agent seat of its own name: a provider whose CLI
// Crewlet runs a seat's executor on (render.RunnerCLIs) drives the seat's
// executor, so that the engine's rules over a seat's executor run on it run
// too, and any other serves the seat's review, as such a provider serves a
// seat's phases but the executor's, the seat's executor running on
// runnerProbe. It asks the engine about each provider: what `crewlet
// validate` refuses and warns about in it or in its seat, and the SIGN-IN
// column `crewlet llm list` prints for it, when list is true. Every reference
// the document makes resolves to a value, as the operator's secret sync makes
// it resolve, and the engine logging one that did not fails the test.
func askEngine(t *testing.T, providers []v1.LLMProvider, list bool) engineReport {
	t.Helper()
	runner := llmProvider("probe", runnerProbe)
	runner.Spec.Model = "probe/probe-model"
	runner.Spec.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey, APIKeySecretRef: secretKey("cli", apiKeyKey)}
	in := &render.Input{
		Company: company("probe", companyName),
		Class:   engineClass("", "basic"),
		// Every provider is rendered whatever the API said of it: the
		// question is the engine's verdict on each shape.
		LLMProviders:    append(slices.Clone(providers), *runner),
		StoreName:       secretsync.StoreName,
		ProvisionedName: secretsync.ProvisionedName,
		// A run in E2B calls the seat's tools back through the company's
		// public address.
		PublicBaseURL: "https://probe.hooks.example.com",
	}
	in.Company.UID = types.UID("00000000-0000-4000-8000-000000000001")
	in.Company.Spec.EngineClassName = "basic"
	// Both cells, so a provider's runIn names a configured one.
	in.Company.Spec.Sandbox = v1.SandboxSettings{
		DefaultRunIn: v1.SandboxPlacementDirect,
		E2B:          v1.E2BSandbox{APIKeySecretRef: secretKey("e2b", apiKeyKey)},
	}
	for i := range providers {
		s := seat("probe", providers[i].Name)
		s.Spec.Goal = "Probe the provider."
		probed := []v1.LLMProviderReference{{Name: providers[i].Name}}
		if slices.Contains(render.RunnerCLIs, providers[i].Spec.Agent) {
			s.Spec.LLM.Default = probed
		} else {
			s.Spec.LLM.Default = []v1.LLMProviderReference{{Name: runner.Name}}
			s.Spec.LLM.Review = probed
		}
		in.Seats = append(in.Seats, *s)
	}
	// A person on a seat of their own, on the company's own chat, where
	// Crewlet asks no account of them. Without the seat, Crewlet advises
	// that nobody can be invited into the company, which is a verdict on
	// the chart rather than on any provider; a tolerance for it here would
	// hide that advisory on a provider's shape too, so the probe gives the
	// engine nothing about the chart to say.
	founder := seat("probe", "founder")
	founder.Spec.Kind = v1.SeatKindHuman
	founder.Spec.Contact = v1.SeatContact{GitHubLogin: "probe-founder"}
	in.Seats = append(in.Seats, *founder)
	doc, err := render.Company(in)
	if err != nil {
		t.Fatalf("rendering the providers: %v", err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "company.yaml")
	if err := os.WriteFile(file, doc.YAML, 0o600); err != nil {
		t.Fatal(err)
	}
	env := make([]string, 0, 2+len(doc.Refs))
	env = append(env, "PATH="+os.Getenv("PATH"), "HOME="+dir)
	for _, k := range doc.Refs {
		env = append(env, secretsync.StoreName(k)+"=probe-"+k.Name+"-"+k.Key)
	}
	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(t.Context(), enginetest.Binary(t), args...)
		cmd.Dir, cmd.Env = dir, env
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if exit := (*exec.ExitError)(nil); err != nil && !errors.As(err, &exit) {
			t.Fatalf("running crewlet %s: %v", args[0], err)
		}
		if err != nil && stdout.Len() == 0 {
			t.Fatalf("crewlet %s failed (%v):\n%s", strings.Join(args, " "), err, stderr.String())
		}
		// A CLI's env is resolved where the engine builds the provider,
		// and one that resolves to nothing is only logged: for a CLI
		// signed in through its env, that is a CLI with no key.
		for _, unresolved := range []string{"config_reference_unresolved", "cli_agent_env_unresolved"} {
			if strings.Contains(stderr.String(), unresolved) {
				t.Errorf("crewlet %s: a reference resolved to nothing:\n%s", args[0], stderr.String())
			}
		}
		return stdout.Bytes(), err
	}

	out, _ := run("validate", "-json", "-tier", "company", file)
	type finding struct {
		Segments []engine.Segment `json:"segments"`
		Message  string           `json:"message"`
	}
	var validation struct {
		Problems []finding `json:"problems"`
		Warnings []finding `json:"warnings"`
	}
	if err := json.Unmarshal(out, &validation); err != nil {
		t.Fatalf("crewlet validate printed no verdict (%v):\n%s", err, out)
	}
	// owner is the provider a finding is about: the provider it is in, or
	// the one the seat it is in drives, which shares its name.
	owner := func(kind string, f finding) string {
		got, ok := doc.Attribute(render.PathOf(f.Segments))
		if !ok || (got.Kind != render.KindLLMProvider && got.Kind != render.KindSeat) {
			t.Fatalf("crewlet validate %s the document outside its providers and seats (%v, %v): %s", kind, got, ok, f.Message)
		}
		return got.Name
	}
	report := engineReport{
		problems: map[string][]string{}, warnings: map[string][]string{},
		models: map[string][]string{}, runIns: map[string][]string{},
	}
	for _, p := range validation.Problems {
		name := owner("refused", p)
		report.problems[name] = append(report.problems[name], p.Message)
		switch render.PathOf(p.Segments).String() {
		case "providers.llm." + name + ".model":
			report.models[name] = append(report.models[name], p.Message)
		case "providers.llm." + name + ".run_in":
			report.runIns[name] = append(report.runIns[name], p.Message)
		}
	}
	for _, w := range validation.Warnings {
		name := owner("doubted", w)
		report.warnings[name] = append(report.warnings[name], w.Message)
	}
	// The probe's own runner is no question: a finding on it is a probe
	// that asks nothing of the providers it carries.
	if findings := slices.Concat(report.problems[runner.Name], report.warnings[runner.Name]); len(findings) > 0 {
		t.Fatalf("crewlet validate finds fault with the probe's runner %s: %q", runner.Name, findings)
	}

	if !list {
		return report
	}
	// No Tier A: llm list then resolves a reference from its environment
	// alone, which holds every one.
	out, err = run("llm", "list", "-company", file, "-config", filepath.Join(dir, "absent.yaml"))
	if err != nil {
		t.Fatalf("crewlet llm list failed (%v):\n%s", err, out)
	}
	report.signIns, err = signInColumn(out)
	if err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	for i := range providers {
		if name := providers[i].Name; report.signIns[name] == "" {
			t.Fatalf("crewlet llm list printed no row for %s:\n%s", name, out)
		}
	}
	return report
}

// signInColumn reads `crewlet llm list`'s table, which aligns its columns
// under a header line and prints no JSON: the SIGN-IN cell of each PROVIDER,
// cut at the header's column offsets, since a cell such as "api key" holds a
// space.
func signInColumn(out []byte) (map[string]string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(out))
	var header string
	for scanner.Scan() {
		if line := scanner.Text(); strings.HasPrefix(line, "PROVIDER ") {
			header = line
			break
		}
	}
	from, to := strings.Index(header, "SIGN-IN"), strings.Index(header, "STATE DIR")
	if header == "" || from < 0 || to < from {
		return nil, fmt.Errorf("crewlet llm list printed no PROVIDER .. SIGN-IN .. STATE DIR header")
	}
	rows := map[string]string{}
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < to {
			return nil, fmt.Errorf("crewlet llm list printed a row shorter than its header: %q", line)
		}
		name, _, _ := strings.Cut(line, " ")
		rows[name] = strings.TrimSpace(line[from:to])
	}
	return rows, scanner.Err()
}
