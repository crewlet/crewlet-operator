package render

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// What a seat's code work, a run_sandbox launch in the Company's E2B sandbox,
// can run on.
//
// A launch hands the seat's coding agent (spec.sandbox.codingAgent, else the
// Company's spec.sandbox.defaultCodingAgent, else ClaudeCode) the model and the
// sign-in of the provider at the head of the seat's sandbox chain
// (spec.llm.sandbox, else spec.llm.default, else the head of the document's
// llm_order). Every provider is a coding CLI, an LLMProvider or a platform
// provider, and Crewlet refuses two pairings, each before a box exists:
//
//   - A provider driving another CLI than the coding agent. Its model is
//     written in its own CLI's grammar and its sign-in is that CLI's, so the
//     coding agent can read neither. Crewlet refuses this on every write
//     (config's validateSandboxCodingAgents), so the document is refused whole
//     either way; refused here, the Seat is told so in its own fields.
//   - A provider whose login cannot follow the run into the remote sandbox:
//     the login a bundle restores (a CLI's credential files, or the document
//     OpenCode 2's export printed) stays on the engine's data volume, because
//     it carries a refresh token the engine keeps rotating there, so the run
//     needs a token or a key in its environment. A platform provider always
//     has one: it signs in with a key alone.
//
// Crewlet applies a document holding the second and refuses each launch of the
// seat's code work, while the seat runs its turns. It is reported on the
// Seat's CodeWorkRunnable rather than refused here: refusing it would hold the
// whole company on its last revision over one seat's code work, which the
// engine itself would run without it (ADR-0005).
//
// The seat's own environment is the union of its spec.sandbox.env, its
// spec.sandbox.setup steps' env and the Company's spec.sandbox.setup steps'
// env, which Crewlet merges into every run. The operator sees the names a
// Secret's values are given under, not the values: a name counts here where
// Crewlet would count a non-blank value under it. A name whose value is
// missing or empty and declared optional is left out of every run, so it
// counts for nothing once the reconcile has read the Secrets (Input.Unset);
// a required one that is missing holds the document, whose running revision
// keeps the value it had, so it counts. A required value Crewlet reads as
// nothing wherever the document is applied, an empty one (Input.Empty) or a
// provisioned one nothing mints, has every run refused before a box exists
// (runEnvironment), whatever the head, and so does a step's file that is
// empty and not optional: that is the verdict then, SecretValueEmpty or
// NoProvisioner, which the Seat's ResolvedRefs reports too, or the Company's
// SecretsSynced for a value of the Company's own steps.
//
// A seat whose sandbox environment signs ClaudeCode in itself runs a
// ClaudeCode run on that sign-in alone: Crewlet hands the run none of the
// provider's (its entrySignIn), since ClaudeCode ranks its sign-ins by name,
// and its guard passes the run on any of them (sandboxCredentials).
//
// A seat whose executor is a CLI in mode Agent has no code work of its own:
// Crewlet never offers it run_sandbox (its Runner.offersSandbox), as it codes
// in its executor's run, on that provider's CLI, and neither pairing applies
// to it. Its spec.sandbox.codingAgent drives nothing, so Crewlet refuses on
// every write one other than that CLI (config's validateSandboxGates), and the
// render refuses it too (agentModeCodingAgent); config's
// validateSandboxCodingAgents judges every other seat alone.
//
// Crewlet decides all of it at a launch, where no validation shows it, so
// TestMirroredEngineDeclarationsAreUnchanged holds the engine functions this
// copies to the ones it was read against, and fails when one changes.

// claudeCodeSignIn are the variables that sign ClaudeCode in from a run's
// environment: a plan's headless token, an Anthropic key or bearer token, or a
// toggle moving it onto a cloud provider's credentials (Crewlet's
// codingagent.ClaudeCodeCredentialEnv, to which a test holds this copy). When
// the seat's own environment sets one, Crewlet hands a ClaudeCode run none of
// its provider's sign-in (its entrySignIn), since ClaudeCode ranks its sign-ins
// by name and a provider's key beside a seat's plan token would win, and its
// guard passes the run on it (sandboxCredentials).
var claudeCodeSignIn = []string{
	"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
	"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
}

// defaultCodingAgent is the coding agent of a seat's code work when neither
// the seat nor the Company names one: Crewlet's sandbox.DefaultCodingAgent,
// to which a test holds this copy.
const defaultCodingAgent = v1alpha1.CodingAgentClaudeCode

// credentialName is the names Crewlet reads as holding a credential, which
// sign an env sign-in CLI in: one of their parts, cut at every character that
// is not a letter or a digit, is, in any case, one of the words below
// (cliprofile.IsCredentialName). It is the pattern of every rule of the API
// that reads a credential's name, to which a test holds this copy: the
// LLMProvider and platform provider API's rules over env, which the CRD suite
// holds to the engine's sign-in, and the tool-server API's refusal of a literal value
// under such a name, which the CRD suite holds to the names the engine reads
// a seat's tool credential under.
var credentialName = regexp.MustCompile(credentialNamePattern)

//nolint:gosec // G101: the pattern a credential's name matches, which holds no credential.
const credentialNamePattern = `(?i)(^|[^A-Za-z0-9])(KEY|KEYS|APIKEY|ACCESSKEY|SECRETKEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|CREDENTIALS|AUTH|OAUTH|PAT)([^A-Za-z0-9]|$)`

// CodeWork is what the operator can tell of a seat's code work from the
// objects: whether its coding agent can run on the head of its sandbox chain,
// or on the sign-in the seat's sandbox environment gives it.
type CodeWork struct {
	// Field is the field of the Seat that names the head, or spec.llm for
	// the head of the document's llm_order.
	Field string
	// Reason is empty when the coding agent can run, on the head's sign-in
	// or on the seat's own, and otherwise v1alpha1.ReasonCodingAgentMismatch,
	// which the render refuses, or v1alpha1.ReasonSandboxLoginNotPortable,
	// v1alpha1.ReasonSecretValueEmpty or v1alpha1.ReasonNoProvisioner, which
	// it only reports.
	Reason string
	// Message says what the coding agent runs on and, when it cannot, what
	// to change, for a condition message after Field.
	Message string
}

// CodeWorkOf is the code work of every Seat Crewlet offers run_sandbox,
// keyed by the Seat's name: an Agent seat with spec.sandbox whose executor
// is no CLI in mode Agent, in a Company with a sandbox. A Seat none of
// whose sandbox chain resolves has no entry, as each reference that does
// not is refused on its own. It is apart from Company because it is a
// statement about each Seat's spec, which holds while another object's
// refusal keeps Company from returning a document.
func CodeWorkOf(in *Input) map[string]CodeWork {
	r := newRenderer(in)
	out := map[string]CodeWork{}
	for i := range in.Seats {
		s := &in.Seats[i]
		if !r.offersCodeWork(s) {
			continue
		}
		if w, ok := r.codeWork(s); ok {
			out[s.Name] = w
		}
	}
	return out
}

// offersCodeWork reports whether Crewlet offers a seat run_sandbox: a seat
// with spec.sandbox, which the Seat API admits on an Agent seat only, in a
// Company whose spec.sandbox is the catalogue of boxes Crewlet requires for
// it (without one the engine refuses the seat's gate instead), unless the
// seat's executor is a CLI in mode Agent, whose run is the one box the seat
// codes in (Runner.offersSandbox).
func (r *renderer) offersCodeWork(s *v1alpha1.Seat) bool {
	if _, _, agentMode := r.agentModeExecutor(s); agentMode {
		return false
	}
	return s.Spec.Sandbox != nil && r.in.Company.Spec.Sandbox.E2B.APIKeySecretRef.Name != ""
}

// agentModeCodingAgent is the refusal of an agent-mode seat's
// spec.sandbox.codingAgent (config's validateSandboxGates): empty when the
// seat's executor is no CLI in mode Agent, or when the field is unset or names
// that CLI, which is the run the seat codes in.
func (r *renderer) agentModeCodingAgent(s *v1alpha1.Seat) string {
	head, cli, ok := r.agentModeExecutor(s)
	if !ok || s.Spec.Sandbox == nil || s.Spec.Sandbox.CodingAgent == "" ||
		string(s.Spec.Sandbox.CodingAgent) == string(cli.agent) {
		return ""
	}
	named := "at " + head.field
	if head.field == fieldLLM {
		named = "which a seat whose spec.llm.default is empty runs on"
	}
	return fmt.Sprintf("names the %s coding agent, but the seat's executor runs in agent mode on LLMProvider %s, "+
		"%s: Crewlet never offers such a seat run_sandbox, as it does its code work in that %s run, so the field "+
		"drives nothing, and Crewlet refuses one naming another CLI than %s on every write. Leave it out",
		s.Spec.Sandbox.CodingAgent, head.provider, named, cli.agent, cli.agent)
}

// codeWork is a seat's code work, as the comment above describes; false when
// none of its sandbox chain resolves.
func (r *renderer) codeWork(s *v1alpha1.Seat) (CodeWork, bool) {
	head, ok := r.codeWorkHead(s)
	if !ok {
		return CodeWork{}, false
	}
	runner := cmp.Or(s.Spec.Sandbox.CodingAgent, r.in.Company.Spec.Sandbox.DefaultCodingAgent, defaultCodingAgent)
	env, gone, refused := r.sandboxEnvNames(s)
	w := CodeWork{Field: head.field}
	switch {
	case string(head.cli.agent) != string(runner):
		w.Reason, w.Message = v1alpha1.ReasonCodingAgentMismatch, codingAgentMismatch(runner, &head)
	case len(refused) > 0:
		w.Field = "spec.sandbox"
		w.Reason, w.Message = r.refusedRun(&refused[0])
	default:
		own, seat := head.remoteSignIn(env)
		switch {
		case len(seat) > 0 && head.cli.agent == v1alpha1.CLIAgentClaudeCode:
			w.Message = fmt.Sprintf("runs the seat's code work with the ClaudeCode coding agent on %s, whose model "+
				"it uses, signed in by %s from the seat's sandbox environment, which Crewlet hands the run in place "+
				"of the provider's own sign-in", head.describe(), strings.Join(seat, ", "))
		case own:
			w.Message = runsOn(runner, &head)
		case len(seat) > 0:
			w.Message = fmt.Sprintf("runs the seat's code work with the %s coding agent on %s, whose model it "+
				"uses, signed in by %s from the seat's sandbox environment: the provider's own %s login cannot "+
				"follow the run into the Company's E2B sandbox",
				runner, head.describe(), strings.Join(seat, ", "), head.cli.agent)
		default:
			_, lost := head.remoteSignIn(gone)
			w.Reason, w.Message = v1alpha1.ReasonSandboxLoginNotPortable, loginNotPortable(&head)+leftOutNote(lost)
		}
	}
	return w, true
}

// leftOutNote is the end of a message saying that names, variables of the
// seat's sandbox environment that would sign its coding agent in, are left
// out of every run, or nothing when there are none.
func leftOutNote(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf(". The seat's sandbox environment gives %s, and leaves it out of every run: its value is "+
		"missing, and the optionalKeys beside it let a run go without it", strings.Join(names, ", "))
}

// runsOn is the message of a seat whose coding agent runs on the model and
// the sign-in of its sandbox chain's head.
func runsOn(runner v1alpha1.CodingAgent, head *sandboxHead) string {
	return fmt.Sprintf("runs the seat's code work with the %s coding agent on %s, whose model and sign-in it "+
		"can use", runner, head.describe())
}

// codingAgentMismatch is the refusal of a code-work head that drives another
// CLI than runner.
func codingAgentMismatch(runner v1alpha1.CodingAgent, head *sandboxHead) string {
	cli := head.cli.agent
	remedy := fmt.Sprintf("Name a provider driving %s first in spec.llm.sandbox", runner)
	if slices.Contains([]v1alpha1.CodingAgent{v1alpha1.CodingAgentClaudeCode, v1alpha1.CodingAgentOpenCode},
		v1alpha1.CodingAgent(cli)) {
		remedy += fmt.Sprintf(", or set spec.sandbox.codingAgent: %s", cli)
	}
	return fmt.Sprintf("runs the seat's code work with the %s coding agent on %s, a provider driving %s: "+
		"its model is written for %s and its sign-in is %s's, so %s can read neither, and Crewlet refuses the "+
		"pairing. %s", runner, head.describe(), cli, cli, cli, runner, remedy)
}

// loginNotPortable is the report of a code-work head of the runner's own CLI
// whose login cannot follow a run into the remote sandbox. Only an LLMProvider
// is ever one: a platform provider signs in with a key.
func loginNotPortable(head *sandboxHead) string {
	remedy := fmt.Sprintf("sign the provider in with auth.tokenSecretRef or auth mode APIKey, or sign ClaudeCode "+
		"in for the seat in spec.sandbox.env (%s)", strings.Join(claudeCodeSignIn, ", "))
	if head.cli.agent == v1alpha1.CLIAgentOpenCode {
		remedy = "put its model provider's key in the provider's env, or in the seat's spec.sandbox.env, " +
			"under a name Crewlet reads as a credential, such as OPENROUTER_API_KEY"
	}
	return fmt.Sprintf("runs the seat's code work in the Company's E2B sandbox on %s, whose %s login cannot "+
		"follow it there: a credential bundle stays on the engine's data volume, as it carries a refresh token "+
		"the engine keeps rotating, and nothing in the run's environment signs %s in, so Crewlet refuses each "+
		"run_sandbox launch of the seat. To fix it, %s",
		head.describe(), head.cli.agent, head.cli.agent, remedy)
}

// sandboxHead is the provider at the head of a seat's sandbox chain.
type sandboxHead struct {
	name string
	// field is the field of the Seat that names it, or spec.llm for the
	// head of the document's llm_order.
	field string
	// platform is true for one of the EngineClass's platform providers.
	platform bool
	// cli is what the verdicts read of the provider's CLI.
	cli cliEntry
}

func (h *sandboxHead) describe() string {
	if h.platform {
		return "the platform model provider " + h.name
	}
	return "LLMProvider " + h.name
}

// cliEntry is what the verdicts on a seat read of a model provider's CLI, an
// LLMProvider's or a platform provider's, the two kinds read alike.
type cliEntry struct {
	agent v1alpha1.CLIAgentName
	mode  v1alpha1.CLIAgentMode
	// authMode is auth.mode, and token and bundle whether auth names a
	// headless token or a credential bundle.
	authMode      v1alpha1.CLIAgentAuthMode
	token, bundle bool
	// env is the names of the provider's env.
	env []string
}

// ownEntry is what the verdicts read of an LLMProvider's spec.
func ownEntry(s *v1alpha1.LLMProviderSpec) cliEntry {
	e := cliEntry{
		agent: s.Agent, mode: s.Mode, authMode: s.Auth.Mode,
		token: s.Auth.TokenSecretRef.Name != "", bundle: s.Auth.CredentialBundleSecretRef.Name != "",
	}
	for _, v := range s.Env {
		e.env = append(e.env, v.Name)
	}
	return e
}

// platformEntry is what the verdicts read of a platform provider: a Text-mode
// CLI signed in with a key, never a token or a bundle.
func platformEntry(p *v1alpha1.PlatformLLMProvider) cliEntry {
	e := cliEntry{agent: p.Agent, authMode: p.Auth.Mode}
	for _, v := range p.Env {
		e.env = append(e.env, v.Name)
	}
	return e
}

// remoteSignIn is how the login of a head that drives one of the coding agents
// reaches a remote box, given the names of the seat's own environment
// (Crewlet's entrySignIn and sandboxCredentials): own when the provider's
// settings carry a sign-in that follows the run, and the variables of the
// seat's environment that sign its coding agent in. ClaudeCode's own travels
// as its token or key, and the seat's as any variable that signs ClaudeCode in,
// which then replaces the provider's; OpenCode's, which signs in through its
// environment, as any variable named as a credential, in its env or the
// seat's environment, the seat's winning a name.
func (h *sandboxHead) remoteSignIn(env envNames) (own bool, seat []string) {
	if h.cli.agent == v1alpha1.CLIAgentClaudeCode {
		return h.cli.token || h.cli.authMode == v1alpha1.CLIAgentAuthAPIKey, env.among(claudeCodeSignIn)
	}
	own = slices.ContainsFunc(h.cli.env, credentialName.MatchString)
	for _, name := range slices.Sorted(maps.Keys(env)) {
		if credentialName.MatchString(name) {
			seat = append(seat, name)
		}
	}
	return own, seat
}

// codeWorkHead resolves the head of a seat's sandbox chain as Crewlet's
// SandboxProvider does: the first provider of spec.llm.sandbox that resolves,
// else of spec.llm.default, else the head of the document's llm_order. A chain
// none of whose providers resolves has no head: each reference is refused on
// its own.
func (r *renderer) codeWorkHead(s *v1alpha1.Seat) (sandboxHead, bool) {
	refs, field := s.Spec.LLM.Sandbox, "spec.llm.sandbox"
	if len(refs) == 0 {
		refs, field = s.Spec.LLM.Default, fieldLLMDefault
	}
	for i, ref := range refs {
		at := fmt.Sprintf("%s[%d].name", field, i)
		switch cmp.Or(ref.Kind, v1alpha1.LLMProviderKindLLMProvider) {
		case v1alpha1.LLMProviderKindLLMProvider:
			if r.providers[ref.Name] {
				return r.ownHead(ref.Name, at), true
			}
		case v1alpha1.LLMProviderKindPlatform:
			if r.usesPlatformProviders() && r.platform[ref.Name] != nil {
				return r.platformHead(ref.Name, at), true
			}
		}
	}
	if len(refs) > 0 {
		return sandboxHead{}, false
	}
	return r.documentHead()
}

// documentHead is the head of the document's llm_order, as providersDoc writes
// it: the first of the Company's spec.llm.order that resolves, then its own
// providers by name, then the platform's by name.
func (r *renderer) documentHead() (sandboxHead, bool) {
	for _, ref := range r.in.Company.Spec.LLM.Order {
		switch cmp.Or(ref.Kind, v1alpha1.LLMProviderKindLLMProvider) {
		case v1alpha1.LLMProviderKindLLMProvider:
			if r.providers[ref.Name] {
				return r.ownHead(ref.Name, fieldLLM), true
			}
		case v1alpha1.LLMProviderKindPlatform:
			if r.usesPlatformProviders() && r.platform[ref.Name] != nil {
				return r.platformHead(ref.Name, fieldLLM), true
			}
		}
	}
	if own := sortedByName(r.in.LLMProviders); len(own) > 0 {
		return r.ownHead(own[0].Name, fieldLLM), true
	}
	if !r.usesPlatformProviders() {
		return sandboxHead{}, false
	}
	providers := r.in.Class.Spec.PlatformLLMProviders
	names := make([]string, 0, len(providers))
	for i := range providers {
		names = append(names, providers[i].Name)
	}
	if len(names) == 0 {
		return sandboxHead{}, false
	}
	return r.platformHead(slices.Min(names), fieldLLM), true
}

// ownHead is the head that is the Company's own LLMProvider name.
func (r *renderer) ownHead(name, field string) sandboxHead {
	return sandboxHead{name: name, field: field, cli: ownEntry(r.own[name])}
}

// platformHead is the head that is the EngineClass's platform provider name.
func (r *renderer) platformHead(name, field string) sandboxHead {
	return sandboxHead{name: name, field: field, platform: true, cli: platformEntry(r.platform[name])}
}

// envNames is the set of variable names of a seat's sandbox runs.
type envNames map[string]bool

// among is the names of names the seat's environment has, in their order.
func (e envNames) among(names []string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(name string) bool { return !e[name] })
}

// runValue is a value a seat's sandbox runs are given: a variable of their
// environment, or a setup step's file.
type runValue struct {
	// name is the variable or the file's path, field the field it is written
	// at and list the optionalKeys that governs it, each of the Seat's own or
	// named with the Company's.
	name, field, list string
	// key is the Secret key the value is read from, zero for a provisioned
	// one, which provisioned names.
	key         secretref.Key
	provisioned string
	optional    bool
}

// sandboxEnvNames is the names the seat's sandbox runs are given values
// under, the names left out of them, and the values every run is refused for:
// its own spec.sandbox.env and setup steps', and the Company's setup steps'.
// Crewlet lays them as its runEnvironment does, the Company's steps, then the
// seat's, then its env, each later one winning a name, and every step's files
// alike. It leaves a name out of every run where the value that wins is under
// a key its block declares optional and is known to hold nothing: its Secret
// key is one the reconcile found missing or empty (Input.Unset), or it is
// provisioned and no Integration provisions. Under a key not declared
// optional, such a value refuses every run, before a box exists, where it is
// known to stay so: an empty value (Input.Empty), which the engine reads as
// nothing, and a provisioned one nothing mints. A missing one holds the
// document, whose running revision keeps the value it had.
func (r *renderer) sandboxEnvNames(s *v1alpha1.Seat) (names, leftOut envNames, refused []runValue) {
	won := map[string]runValue{}
	stepsAt := []struct {
		owner string
		steps []v1alpha1.SandboxSetupStep
	}{
		{owner: fmt.Sprintf("Company %s's ", r.in.Company.Name), steps: r.in.Company.Spec.Sandbox.Setup},
		{steps: s.Spec.Sandbox.Setup},
	}
	for _, at := range stepsAt {
		for i := range at.steps {
			step := &at.steps[i]
			list := fmt.Sprintf("%sspec.sandbox.setup[%d].optionalKeys", at.owner, i)
			for j, e := range step.Env {
				won[e.Name] = runValue{
					name: e.Name, field: fmt.Sprintf("%sspec.sandbox.setup[%d].env[%d].valueFrom", at.owner, i, j), list: list,
					key: r.key(e.ValueFrom.SecretKeyRef), optional: slices.Contains(step.OptionalKeys, e.Name),
				}
			}
			for j, f := range step.Files {
				file := runValue{
					name: f.Path, field: fmt.Sprintf("%sspec.sandbox.setup[%d].files[%d].valueFrom", at.owner, i, j), list: list,
					key: r.key(f.ValueFrom.SecretKeyRef), optional: slices.Contains(step.OptionalKeys, f.Path),
				}
				if !file.optional && r.empty[file.key] {
					refused = append(refused, file)
				}
			}
		}
	}
	for j, e := range s.Spec.Sandbox.Env {
		v := runValue{
			name: e.Name, field: fmt.Sprintf("spec.sandbox.env[%d].valueFrom", j), list: "spec.sandbox.optionalKeys",
			provisioned: e.ValueFrom.Provisioned.Name, optional: slices.Contains(s.Spec.Sandbox.OptionalKeys, e.Name),
		}
		if e.ValueFrom.SecretKeyRef.Name != "" {
			v.key = r.key(e.ValueFrom.SecretKeyRef)
		}
		won[e.Name] = v
	}
	names, leftOut = envNames{}, envNames{}
	for _, name := range slices.Sorted(maps.Keys(won)) {
		v := won[name]
		unminted := v.provisioned != "" && !r.engineProvisions()
		switch {
		case v.optional && (r.unset[v.key] || unminted):
			leftOut[name] = true
			continue
		case r.empty[v.key] || unminted:
			refused = append(refused, v)
		}
		names[name] = true
	}
	return names, leftOut, refused
}

// refusedRun is the verdict on a seat every run of which Crewlet refuses for
// v, a value it is given under a key not declared optional that holds
// nothing.
func (r *renderer) refusedRun(v *runValue) (reason, message string) {
	held := fmt.Sprintf("Secret %s/%s holds an empty value under %q", v.key.Namespace, v.key.Name, v.key.Key)
	reason = v1alpha1.ReasonSecretValueEmpty
	if v.provisioned != "" {
		held = fmt.Sprintf("it is the provisioned credential %s, and no Integration of Company %s has provisioning "+
			"Engine to create it", v.provisioned, r.in.Company.Name)
		reason = v1alpha1.ReasonNoProvisioner
	}
	return reason, fmt.Sprintf("Crewlet refuses each run_sandbox launch of the seat before a box exists, as its "+
		"sandbox gives it %s at %s and %s: a run is started only with every value it is given. Give it a value, "+
		"or list %s in %s to have the run go without it", v.name, v.field, held, v.name, v.list)
}

// key is ref's Secret key, in the Company's namespace.
func (r *renderer) key(ref v1alpha1.SecretKeyReference) secretref.Key {
	return secretref.Key{Namespace: r.namespace(), Name: ref.Name, Key: ref.Key}
}
