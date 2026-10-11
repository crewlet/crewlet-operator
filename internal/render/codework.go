package render

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// What a seat's code work, a run_sandbox launch in the Company's E2B sandbox,
// can run on.
//
// A launch hands the seat's coding agent (spec.sandbox.codingAgent, else the
// Company's spec.sandbox.defaultCodingAgent, else ClaudeCode) the model and the
// sign-in of the provider at the head of the seat's sandbox chain
// (spec.llm.sandbox, else spec.llm.default, else the head of the document's
// llm_order). Crewlet refuses three pairings, each before a box exists:
//
//   - A CLIAgent provider driving another CLI than the coding agent. Its model
//     is written in its own CLI's grammar and its sign-in is that CLI's, so the
//     coding agent can read neither. Crewlet refuses this on every write
//     (config's validateSandboxCodingAgents), so the document is refused whole
//     either way; refused here, the Seat is told so in its own fields.
//   - ClaudeCode on a provider of type OpenAI or OpenAICompatible, which speaks
//     a wire ClaudeCode does not: unless the seat's own environment signs
//     ClaudeCode in, when it runs on that sign-in and its own model choice.
//   - A CLIAgent provider whose login cannot follow the run into the remote
//     sandbox: the credential files of a bundle stay on the engine's data
//     volume, because they carry a refresh token the engine keeps rotating
//     there, so the run needs a token or a key in its environment.
//
// Crewlet applies a document holding either of the last two and refuses each
// launch of the seat's code work, while the seat runs its turns. They are
// reported on the Seat's CodeWorkRunnable rather than refused here: refusing
// them would hold the whole company on its last revision over one seat's
// code work, which the engine itself would run without it (ADR-0005).
//
// The seat's own environment is the union of its spec.sandbox.env, its
// spec.sandbox.setup steps' env and the Company's spec.sandbox.setup steps'
// env, which Crewlet merges into every run. The operator sees the names a
// Secret's values are given under, not the values: a name counts here where
// Crewlet would count a non-blank value under it.
//
// Crewlet decides all of it at a launch, where no validation shows it, so
// TestMirroredEngineFunctionsAreUnchanged holds the engine functions this
// copies to the ones it was read against, and fails when one changes.

// claudeCodeSignIn are the variables that sign ClaudeCode in from its own
// environment: a plan's headless token, an Anthropic key or bearer token, or a
// toggle moving it onto a cloud provider's credentials (Crewlet's
// codingagent.ClaudeCodeCredentialEnv, to which a test holds this copy).
var claudeCodeSignIn = []string{
	"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
	"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
}

// defaultCodingAgent is the coding agent of a seat's code work when neither
// the seat nor the Company names one: Crewlet's sandbox.DefaultCodingAgent,
// to which a test holds this copy.
const defaultCodingAgent = v1alpha1.CodingAgentClaudeCode

// claudeCodeLogin are the variables Crewlet reads as a ClaudeCode CLIAgent
// provider's own sign-in inside a box: its profile's token and key variables
// (token_env and api_key_env, to which a test holds this copy).
var claudeCodeLogin = []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY"}

// signInName is the names Crewlet reads as holding a credential, which signs
// an env sign-in CLI in: one of their parts between underscores is, in any
// case, one of the words below (cliprofile.IsCredentialName). It is the
// pattern the LLMProvider API's rules over cli.env match, to which a test
// holds this copy, and which the CRD suite holds to the engine.
var signInName = regexp.MustCompile(signInNamePattern)

const signInNamePattern = `(?i)(^|_)(KEY|KEYS|APIKEY|ACCESSKEY|SECRETKEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|CREDENTIALS|AUTH|OAUTH|PAT)(_|$)`

// CodeWork is what the operator can tell of a seat's code work from the
// objects: whether its coding agent can run on the head of its sandbox chain,
// or on the sign-in the seat's sandbox environment gives it.
type CodeWork struct {
	// Field is the field of the Seat that names the head, or spec.llm for
	// the head of the document's llm_order.
	Field string
	// Reason is empty when the coding agent can run, on the head or on the
	// seat's own sign-in, and otherwise v1alpha1.ReasonCodingAgentMismatch, which the render refuses,
	// v1alpha1.ReasonSandboxModelUnsupported or
	// v1alpha1.ReasonSandboxLoginNotPortable, which it only reports.
	Reason string
	// Message says what the coding agent runs on and, when it cannot, what
	// to change, for a condition message after Field.
	Message string
}

// CodeWorkOf is the code work of every Seat Crewlet offers run_sandbox,
// keyed by the Seat's name: an Agent seat with spec.sandbox, in a Company
// with a sandbox. A Seat none of whose sandbox chain resolves has no entry,
// as each reference that does not is refused on its own. It is apart from
// Company because it is a statement about each Seat's spec, which holds while
// another object's refusal keeps Company from returning a document.
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
// it (without one the engine refuses the seat's gate instead).
func (r *renderer) offersCodeWork(s *v1alpha1.Seat) bool {
	return s.Spec.Sandbox != nil && r.in.Company.Spec.Sandbox.E2B.APIKeySecretRef.Name != ""
}

// codeWork is a seat's code work, as the comment above describes; false when
// none of its sandbox chain resolves.
func (r *renderer) codeWork(s *v1alpha1.Seat) (CodeWork, bool) {
	head, ok := r.codeWorkHead(s)
	if !ok {
		return CodeWork{}, false
	}
	runner := cmp.Or(s.Spec.Sandbox.CodingAgent, r.in.Company.Spec.Sandbox.DefaultCodingAgent, defaultCodingAgent)
	env := r.sandboxEnvNames(s)
	w := CodeWork{Field: head.field}
	switch {
	case head.cli != nil && string(head.cli.Agent) != string(runner):
		w.Reason, w.Message = v1alpha1.ReasonCodingAgentMismatch, codingAgentMismatch(runner, head)
	case head.cli == nil && runner == v1alpha1.CodingAgentClaudeCode && head.kind != v1alpha1.LLMProviderAnthropic:
		if signIn := env.among(claudeCodeSignIn); len(signIn) > 0 {
			// Crewlet hands the run none of the provider then, so the
			// message must not say it runs on the provider's model.
			w.Message = fmt.Sprintf("runs the seat's code work with the ClaudeCode coding agent on the sign-in "+
				"the seat's sandbox environment gives it (%s), under its own model choice: ClaudeCode speaks the "+
				"Anthropic API only, so it reads neither the model nor the key of %s, of type %s",
				strings.Join(signIn, ", "), head.describe(), head.kind)
			break
		}
		w.Reason, w.Message = v1alpha1.ReasonSandboxModelUnsupported, fmt.Sprintf(
			"runs the seat's code work with the ClaudeCode coding agent on %s, of type %s, and ClaudeCode speaks "+
				"the Anthropic API only: it can read neither that provider's model nor its key, so Crewlet refuses "+
				"each run_sandbox launch of the seat. Name a provider of type Anthropic first in "+
				"spec.llm.sandbox, set spec.sandbox.codingAgent: OpenCode, which reads the OpenAI wire too, or "+
				"sign ClaudeCode in for the seat in spec.sandbox.env (%s), on which it runs under its own model "+
				"choice",
			head.describe(), head.kind, strings.Join(claudeCodeSignIn, ", "))
	case head.cli != nil:
		own, seat := head.remoteSignIn(env)
		switch {
		case own:
			w.Message = runsOn(runner, head)
		case len(seat) > 0:
			w.Message = fmt.Sprintf("runs the seat's code work with the %s coding agent on %s, whose model it "+
				"uses, signed in by %s from the seat's sandbox environment: the provider's own %s login cannot "+
				"follow the run into the Company's E2B sandbox",
				runner, head.describe(), strings.Join(seat, ", "), head.cli.Agent)
		default:
			w.Reason, w.Message = v1alpha1.ReasonSandboxLoginNotPortable, loginNotPortable(head)
		}
	default:
		w.Message = runsOn(runner, head)
	}
	return w, true
}

// runsOn is the message of a seat whose coding agent runs on the model and
// the sign-in of its sandbox chain's head.
func runsOn(runner v1alpha1.CodingAgent, head sandboxHead) string {
	return fmt.Sprintf("runs the seat's code work with the %s coding agent on %s, whose model and sign-in it "+
		"can use", runner, head.describe())
}

// codingAgentMismatch is the refusal of a code-work head that is a CLIAgent
// provider of another CLI than runner.
func codingAgentMismatch(runner v1alpha1.CodingAgent, head sandboxHead) string {
	cli := head.cli.Agent
	api := "Anthropic, OpenAI or OpenAICompatible"
	if runner == v1alpha1.CodingAgentClaudeCode {
		// ClaudeCode reads Anthropic's wire alone, which the next refusal
		// would say of an OpenAI provider named here.
		api = "Anthropic"
	}
	remedy := fmt.Sprintf("Name a provider of type %s, or a CLIAgent provider driving %s, first in spec.llm.sandbox",
		api, runner)
	if slices.Contains([]v1alpha1.CodingAgent{v1alpha1.CodingAgentClaudeCode, v1alpha1.CodingAgentOpenCode},
		v1alpha1.CodingAgent(cli)) {
		remedy += fmt.Sprintf(", or set spec.sandbox.codingAgent: %s", cli)
	}
	return fmt.Sprintf("runs the seat's code work with the %s coding agent on %s, a CLIAgent provider driving %s: "+
		"its model is written for %s and its sign-in is %s's, so %s can read neither, and Crewlet refuses the "+
		"pairing. %s", runner, head.describe(), cli, cli, cli, runner, remedy)
}

// loginNotPortable is the report of a code-work head that is a CLIAgent
// provider of the runner's own CLI whose login cannot follow a run into the
// remote sandbox.
func loginNotPortable(head sandboxHead) string {
	remedy := fmt.Sprintf("sign the provider in with cli.auth.tokenSecretRef or auth mode APIKey, or give the "+
		"seat %s in spec.sandbox.env", strings.Join(claudeCodeLogin, " or "))
	if head.cli.Agent == v1alpha1.CLIAgentOpenCode {
		remedy = "put its model provider's key in the provider's cli.env, or in the seat's spec.sandbox.env, " +
			"under a name Crewlet reads as a credential, such as OPENROUTER_API_KEY"
	}
	return fmt.Sprintf("runs the seat's code work in the Company's E2B sandbox on %s, whose %s login cannot "+
		"follow it there: a credential bundle stays on the engine's data volume, as it carries a refresh token "+
		"the engine keeps rotating, and nothing in the run's environment signs %s in, so Crewlet refuses each "+
		"run_sandbox launch of the seat. To fix it, %s",
		head.describe(), head.cli.Agent, head.cli.Agent, remedy)
}

// sandboxHead is the provider at the head of a seat's sandbox chain.
type sandboxHead struct {
	name string
	// field is the field of the Seat that names it, or spec.llm for the
	// head of the document's llm_order.
	field string
	// platform is true for one of the EngineClass's platform providers.
	platform bool
	kind     v1alpha1.LLMProviderType
	// cli is a CLIAgent provider's cli block, nil for every other type.
	cli *v1alpha1.CLIAgentSettings
}

func (h sandboxHead) describe() string {
	if h.platform {
		return "the platform model provider " + h.name
	}
	return "LLMProvider " + h.name
}

// remoteSignIn is how the login of a CLIAgent head that drives one of the
// coding agents reaches a remote box, given the names of the seat's own
// environment (Crewlet's sandboxCredentials): own when the provider's settings
// carry it, and otherwise the variables of the seat's environment that do,
// none when nothing does. ClaudeCode's travels as its token or key, from auth
// or the seat's environment; OpenCode's, which signs in through its
// environment, as any variable named as a credential, in its cli.env or the
// seat's environment.
func (h sandboxHead) remoteSignIn(env envNames) (own bool, seat []string) {
	if h.cli.Agent == v1alpha1.CLIAgentClaudeCode {
		auth := h.cli.Auth
		return auth.TokenSecretRef.Name != "" || auth.Mode == v1alpha1.CLIAgentAuthAPIKey,
			env.among(claudeCodeLogin)
	}
	own = slices.ContainsFunc(h.cli.Env, func(e v1alpha1.SecretEnvVar) bool { return signInName.MatchString(e.Name) })
	for _, name := range slices.Sorted(maps.Keys(env)) {
		if signInName.MatchString(name) {
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
			if r.usesPlatformProviders() && r.platform[ref.Name] {
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
			if r.usesPlatformProviders() && r.platform[ref.Name] {
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
	head := sandboxHead{name: name, field: field, cli: r.cliProviders[name]}
	for i := range r.in.LLMProviders {
		if p := &r.in.LLMProviders[i]; p.Name == name {
			head.kind = p.Spec.Type
		}
	}
	return head
}

// platformHead is the head that is the EngineClass's platform provider name.
// None drives a CLI: the API refuses type CLIAgent there.
func (r *renderer) platformHead(name, field string) sandboxHead {
	head := sandboxHead{name: name, field: field, platform: true}
	for i := range r.in.Class.Spec.PlatformLLMProviders {
		if p := &r.in.Class.Spec.PlatformLLMProviders[i]; p.Name == name {
			head.kind = p.Type
		}
	}
	return head
}

// envNames is the set of variable names of a seat's sandbox runs.
type envNames map[string]bool

// among is the names of names the seat's environment has, in their order.
func (e envNames) among(names []string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(name string) bool { return !e[name] })
}

// sandboxEnvNames is the names the seat's sandbox runs are given values
// under: its own spec.sandbox.env and setup steps', and the Company's setup
// steps'.
func (r *renderer) sandboxEnvNames(s *v1alpha1.Seat) envNames {
	names := envNames{}
	for _, e := range s.Spec.Sandbox.Env {
		names[e.Name] = true
	}
	for _, steps := range [][]v1alpha1.SandboxSetupStep{s.Spec.Sandbox.Setup, r.in.Company.Spec.Sandbox.Setup} {
		for _, step := range steps {
			for _, e := range step.Env {
				names[e.Name] = true
			}
		}
	}
	return names
}
