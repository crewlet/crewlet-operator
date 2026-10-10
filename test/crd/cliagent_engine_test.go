package crd_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
	"github.com/crewlet/crewlet-operator/test/docs"
)

// TestCLIAgentSignInFollowsTheEngine holds the LLMProvider API's rules over a
// CLIAgent provider's sign-in, and the table docs/guides/cli-agents.md states
// them in, to the engine the operator is built against rather than to a copy
// of its profile table. The token and key variable of each CLI are read from
// the pinned crewlet's own refusals, and held to the copy the API's
// own-variable rule and its cases name. Every CLI the API offers is then tried
// in every sign-in shape, those variables among them, each provider driving
// one seat's executor: the API server's verdict is taken from the installed
// CRD, and the engine's from the pinned crewlet binary, by `crewlet validate`
// (does it admit the provider and its seat, and does it doubt either) and
// `crewlet llm list` (what it reports the CLI signed in with). A bump of the
// engine that moves a profile's token or key variable, or marks another CLI
// as signing in through its environment, fails here until the API and the
// guide follow it.
//
// The engine admits some shapes the API refuses on purpose, which this does
// not hold against it: a Subscription signed in to nothing (the engine
// expects `crewlet llm login` at a terminal a pod does not have), and mode
// Agent for a CLI with no sandbox runner (the engine refuses it when the seat
// first has work). Mode Agent is offered for exactly the Company's coding
// agents, the CLIs the engine has a sandbox runner for, which hand an
// Agent-mode run the model as the provider's text calls pass it. The rest
// must agree.
func TestCLIAgentSignInFollowsTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	agents := enumOf(t, "llmproviders.crewlet.ai", "spec", "cli", "agent")
	// The coding agents an Agent-mode run of the pinned engine works for:
	// the render tests hold the enum to the engine's runners.
	agentRunners := enumOf(t, "companies.crewlet.ai", "spec", "sandbox", "defaultCodingAgent")

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
		for _, mode := range []v1.CLIAgentMode{v1.CLIAgentModeText, v1.CLIAgentModeAgent} {
			shape := s.name + ", mode " + string(mode)
			providers := make([]v1.LLMProvider, 0, len(agents))
			admitted := map[string]string{}
			for _, agent := range agents {
				p := shapedProvider(namespace, v1.CLIAgentName(agent), mode, &s)
				admitted[p.Name] = apiVerdict(t, p)
				providers = append(providers, *p)
			}
			report := askEngine(t, providers, true)
			verdicts[shape] = map[string]verdict{}
			for i := range providers {
				p := &providers[i]
				verdicts[shape][string(p.Spec.CLI.Agent)] = verdict{
					refusal: admitted[p.Name],
					problem: first(report.problems[p.Name]),
					warning: first(report.warnings[p.Name]),
					signIn:  report.signIns[p.Name],
				}
			}
		}
	}

	holdVerdicts(t, verdicts, agentRunners)

	// Mode Agent is offered exactly for the coding agents an Agent-mode run
	// works for, of those the Company's sandbox offers, which the render
	// tests hold to the engine's runners.
	var agentMode []string
	for _, agent := range agents {
		if admitsAgentMode(verdicts, agent) {
			agentMode = append(agentMode, agent)
		}
	}
	slices.Sort(agentMode)
	slices.Sort(agentRunners)
	if !slices.Equal(agentMode, agentRunners) {
		t.Errorf("the API admits mode Agent for %v, want the coding agents %v", agentMode, agentRunners)
	}

	// The CLIs a model provider's key in env signs in are the engine's, and
	// the ones the API's rules and their cases name.
	var fromEnv []v1.CLIAgentName
	for agent, v := range verdicts["subscription, env OPENROUTER_API_KEY, mode Text"] {
		if v.signIn == "environment" {
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
// CLIAgent provider's model to the pinned engine, which refuses a model of
// white space alone, and on every write a model with no provider segment for
// a CLI whose profile reads its model as <provider>/<model>
// (model_names_provider). Every CLI the API offers is
// tried, signed in a way the API admits for it, in both modes, with models
// with and without a provider, with empty segments and with segments of white
// space, Go's and Unicode's beyond ASCII's among it: the API refuses with its
// model rules exactly the providers whose model the engine refuses. A bump of
// the engine that marks another profile as naming its provider, or stops
// marking OpenCode's, fails here until the API follows it.
//
// The API's rule is the engine's test, transcribed: a slash, and something
// other than white space on each side of the first one, where white space is
// what Go's strings.TrimSpace removes, as CEL's trim() is. The engine judges
// the model as the profile's model_args render it, which the API offers no
// override of, so the value the rule judges is spec.model itself.
func TestCLIAgentModelFollowsTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	agents := enumOf(t, "llmproviders.crewlet.ai", "spec", "cli", "agent")
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
	for _, mode := range []v1.CLIAgentMode{v1.CLIAgentModeText, v1.CLIAgentModeAgent} {
		for _, model := range models {
			providers := make([]v1.LLMProvider, 0, len(agents))
			refused := map[string]bool{}
			for _, agent := range agents {
				shape := admittedSignIn(v1.CLIAgentName(agent), variables)
				p := shapedProvider(namespace, v1.CLIAgentName(agent), mode, &shape)
				p.Spec.Model = model
				refusal := apiVerdict(t, p)
				refused[p.Name] = strings.Contains(refusal, msgOpenCodeModel) || strings.Contains(refusal, msgBlankModel)
				// The shape is admitted but for its model, so the model
				// rule is the question; in mode Agent, but for a CLI with
				// no sandbox runner.
				if !refused[p.Name] && refusal != "" &&
					(mode == v1.CLIAgentModeText || !strings.Contains(refusal, msgCLIAgentRunner)) {
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
					t.Errorf("%s, mode %s, model %q: the API refuses the model and the engine admits it",
						p.Spec.CLI.Agent, mode, model)
				case !refused[p.Name] && engineRefuses:
					t.Errorf("%s, mode %s, model %q: the engine refuses the model and the API admits it: %s",
						p.Spec.CLI.Agent, mode, model, report.models[p.Name][0])
				}
				if engineRefuses {
					refusedModels[p.Spec.CLI.Agent] = true
				}
			}
		}
	}
	// The probe can fail: the engine refuses some model of OpenCode's.
	if !refusedModels[v1.CLIAgentOpenCode] {
		t.Errorf("the engine refused no OpenCode model; the probe's models no longer reach its rule")
	}
}

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

// holdVerdicts holds the API server's verdict on each shape to the engine's,
// but in mode Agent for a CLI outside agentRunners.
func holdVerdicts(t *testing.T, verdicts map[string]map[string]verdict, agentRunners []string) {
	t.Helper()
	for shape, byAgent := range verdicts {
		agentMode := strings.HasSuffix(shape, ", mode "+string(v1.CLIAgentModeAgent))
		for agent, v := range byAgent {
			admitted := v.refusal == ""
			// A bundle signs the CLI in from the login files the engine
			// restores, which `llm list` cannot see before a restore; an
			// Agent-mode run is given none of them, so there the bundle is
			// no sign-in.
			signedIn := slices.Contains([]string{"token", "api key", "environment"}, v.signIn) ||
				(strings.HasPrefix(shape, "bundle") && !agentMode)
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
			// What the engine admits and signs in, the API admits, but an
			// Agent-mode run no coding agent of the pinned engine works for.
			engineAdmits := v.problem == "" && v.warning == ""
			if !admitted && engineAdmits && signedIn && (!agentMode || slices.Contains(agentRunners, agent)) {
				t.Errorf("%s, %s: the engine admits it, signed in with %q, and the API refuses it: %s",
					agent, shape, v.signIn, v.refusal)
			}
		}
	}
}

// admitsAgentMode reports whether the API server admitted agent in mode Agent
// in any shape.
func admitsAgentMode(verdicts map[string]map[string]verdict, agent string) bool {
	for shape, byAgent := range verdicts {
		if strings.HasSuffix(shape, ", mode "+string(v1.CLIAgentModeAgent)) && byAgent[agent].refusal == "" {
			return true
		}
	}
	return false
}

// signInTableFollows holds the table docs/guides/cli-agents.md states what
// each CLI takes in to what the engine and the API said: a row for every CLI
// the API offers, naming its token and key variable as the engine's profile
// does, a key in env where the engine signs the CLI in from one, a bundle
// where the API admits one, and mode Agent where the API admits it.
func signInTableFollows(t *testing.T, agents []string, variables map[v1.CLIAgentName]cliVariable,
	fromEnv []v1.CLIAgentName, verdicts map[string]map[string]verdict) {
	t.Helper()
	const guide = "docs/guides/cli-agents.md"
	rows, err := docs.Table(filepath.Join("..", "..", guide), "`cli.agent`")
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
			{"credentialBundleSecretRef", yes(row[4]), verdicts["bundle, mode Text"][agent].refusal == ""},
			{"mode: Agent", yes(row[5]), admitsAgentMode(verdicts, agent)},
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

// engineVariables reads from the pinned engine the token and key variable
// its profile for each of agents names. One provider of each CLI asks for
// both, in auth mode APIKey with a token and no key, which the engine
// refuses naming the variable each would have filled, or saying the profile
// names none.
func engineVariables(t *testing.T, namespace string, agents []string) map[v1.CLIAgentName]cliVariable {
	t.Helper()
	probe := signInShape{auth: v1.CLIAgentAuth{
		Mode:           v1.CLIAgentAuthAPIKey,
		TokenSecretRef: secretKey("cli", "token"),
	}}
	providers := make([]v1.LLMProvider, 0, len(agents))
	for _, agent := range agents {
		providers = append(providers, *shapedProvider(namespace, v1.CLIAgentName(agent), v1.CLIAgentModeText, &probe))
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
			t.Fatalf("%s: reading its key variable: %v", p.Spec.CLI.Agent, err)
		}
		token, err := read(problems, tokenVariable, noTokenVariable)
		if err != nil {
			t.Fatalf("%s: reading its token variable: %v", p.Spec.CLI.Agent, err)
		}
		if key != "" || token != "" {
			out[p.Spec.CLI.Agent] = cliVariable{headless: token, key: key}
		}
	}
	return out
}

// The engine's refusals of a token and of a key a profile has or lacks the
// variable for (crewlet's internal/config, validateCLICredentials).
var (
	keyVariable = regexp.MustCompile(
		`auth\.mode api-key puts the api_keys value in ([A-Za-z0-9_]+), and this entry names none`)
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

// The names of the sign-in shapes the API's cases are built on, and the start
// of the name of each shape that signs a subscription in with one variable in
// env.
const (
	shapeAPIKey = "api key"
	shapeToken  = "token"
	shapeBundle = "bundle"
	shapeEnv    = "subscription, env "
)

// signInShape is one way to sign a CLI in.
type signInShape struct {
	name string
	auth v1.CLIAgentAuth
	// key gives the provider one apiKeySecretRefs key.
	key bool
	// env is the names the cli block's env sets.
	env []string
}

// signInShapes are the ways a CLI may be signed in that the API can express:
// a key, a token, a bundle, and the names env may carry. The names are probe
// input, not a list of what any CLI reads: names with each part the engine's
// docs call a credential, names that only hold such a part inside a longer
// word, one in lower case; and, beside a bundle so the variable is the only
// question, every token and key variable the engine's profiles name
// (variables) and one that is no credential. Whether each signs a CLI in is
// the engine's answer.
func signInShapes(variables map[v1.CLIAgentName]cliVariable) []signInShape {
	inEnv := []string{
		"OPENROUTER_API_KEY", "openrouter_api_key", "GEMINI_API_KEYS", "VENDOR_APIKEY",
		"AWS_ACCESSKEY", "AWS_SECRETKEY", "GH_TOKEN", "OAUTH2_TOKEN", "DB_SECRET",
		"DB_PASSWORD", "DB_PASSWD", "GCP_CREDENTIAL", "GCP_CREDENTIALS", "PROXY_AUTH",
		"VENDOR_OAUTH", "GITLAB_PAT",
		envProxy, "HERMES_MAX_TOKENS", "MY_KEY2", "MONKEY", "KEYRING_PATH", "AUTHOR",
	}
	besideBundle := []string{envProxy}
	for _, v := range variables {
		for _, name := range []string{v.headless, v.key} {
			if name != "" && !slices.Contains(besideBundle, name) {
				besideBundle = append(besideBundle, name)
			}
		}
	}
	slices.Sort(besideBundle[1:])
	bundle := v1.CLIAgentAuth{CredentialBundleSecretRef: secretKey("cli", "bundle")}
	shapes := make([]signInShape, 0, 3+len(inEnv)+len(besideBundle))
	shapes = append(shapes,
		signInShape{name: shapeAPIKey, auth: v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey}, key: true},
		signInShape{name: shapeToken, auth: v1.CLIAgentAuth{TokenSecretRef: secretKey("cli", "token")}},
		signInShape{name: shapeBundle, auth: bundle},
	)
	for _, name := range inEnv {
		shapes = append(shapes, signInShape{
			name: shapeEnv + name,
			auth: v1.CLIAgentAuth{Mode: v1.CLIAgentAuthSubscription},
			env:  []string{name},
		})
	}
	for _, name := range besideBundle {
		shapes = append(shapes, signInShape{name: "bundle, env " + name, auth: bundle, env: []string{name}})
	}
	return shapes
}

// shapedProvider is a CLIAgent provider of agent in mode, signed in as s,
// named for its agent so a document can hold one of each.
func shapedProvider(namespace string, agent v1.CLIAgentName, mode v1.CLIAgentMode, s *signInShape) *v1.LLMProvider {
	p := llmProvider(namespace, strings.ToLower(string(agent)))
	p.Spec.Type = v1.LLMProviderCLIAgent
	p.Spec.Model = "probe/probe-model"
	p.Spec.APIKeySecretRefs = nil
	if s.key {
		p.Spec.APIKeySecretRefs = []v1.SecretKeyReference{secretKey("cli", apiKeyKey)}
	}
	p.Spec.CLI = v1.CLIAgentSettings{Agent: agent, Mode: mode, Auth: s.auth}
	for _, name := range s.env {
		p.Spec.CLI.Env = append(p.Spec.CLI.Env, v1.SecretEnvVar{Name: name, ValueFrom: fromSecret("cli", "env")})
	}
	return p
}

// apiVerdict is the API server's refusal of p, empty when it admits it. The
// create is a dry run, so a document's providers can share their names.
func apiVerdict(t *testing.T, p *v1.LLMProvider) string {
	t.Helper()
	err := k8sClient.Create(ctx, p.DeepCopy(), client.DryRunAll)
	switch {
	case err == nil:
		return ""
	case apierrors.IsInvalid(err):
		return err.Error()
	default:
		t.Fatalf("creating %s: %v", p.Name, err)
		return ""
	}
}

// engineReport is what the pinned engine said of a document's providers,
// each by its name.
type engineReport struct {
	// problems are what `crewlet validate` refused in the provider or in
	// the seat it drives.
	problems map[string][]string
	// warnings are what it doubted in either.
	warnings map[string][]string
	// models are the problems it found with the provider's model.
	models map[string][]string
	// signIns are the SIGN-IN column of `crewlet llm list`.
	signIns map[string]string
}

// askEngine renders providers into one company document, as the operator
// would apply it, each driving the executor of an agent seat of its own name,
// so that the engine's rules over a seat whose executor reaches a provider in
// mode Agent run too. It asks the pinned engine about each provider: what
// `crewlet validate` refuses and warns about in it or in its seat, and the
// SIGN-IN column `crewlet llm list` prints for it, when list is true. Every
// reference the document makes resolves to a value, as the operator's secret
// sync makes it resolve, and the engine logging one that did not fails the
// test.
func askEngine(t *testing.T, providers []v1.LLMProvider, list bool) engineReport {
	t.Helper()
	in := &render.Input{
		Company: company("probe", companyName),
		Class:   engineClass("", "basic"),
		// Every provider is rendered whatever the API said of it: the
		// question is the engine's verdict on each shape.
		LLMProviders:    providers,
		StoreName:       secretsync.StoreName,
		ProvisionedName: secretsync.ProvisionedName,
		// A run in mode Agent calls the seat's tools back through the
		// company's public address.
		PublicBaseURL: "https://probe.hooks.example.com",
	}
	in.Company.UID = types.UID("00000000-0000-4000-8000-000000000001")
	in.Company.Spec.EngineClassName = "basic"
	in.Company.Spec.Sandbox = v1.SandboxSettings{E2B: v1.E2BSandbox{APIKeySecretRef: secretKey("e2b", apiKeyKey)}}
	for i := range providers {
		s := seat("probe", providers[i].Name)
		s.Spec.Goal = "Probe the provider."
		s.Spec.LLM.Default = []v1.LLMProviderReference{{Name: providers[i].Name}}
		in.Seats = append(in.Seats, *s)
	}
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
	report := engineReport{problems: map[string][]string{}, warnings: map[string][]string{}, models: map[string][]string{}}
	for _, p := range validation.Problems {
		name := owner("refused", p)
		report.problems[name] = append(report.problems[name], p.Message)
		if path := render.PathOf(p.Segments); path.String() == "providers.llm."+name+".model" {
			report.models[name] = append(report.models[name], p.Message)
		}
	}
	for _, w := range validation.Warnings {
		name := owner("doubted", w)
		report.warnings[name] = append(report.warnings[name], w.Message)
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
