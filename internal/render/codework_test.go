package render

import (
	"cmp"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// The objects of the opencode-agent case the code-work tests break, and the
// names their providers and Secrets use.
const (
	codeWorkSeat  = "fixer"
	codeWorkModel = "openrouter/anthropic/claude-sonnet-5"
	keyAPIKey     = "apiKey"
	keyBundle     = "bundle"
	keyValue      = "value"
	envGatewayKey = "OPENROUTER_API_KEY"
	platformGPT   = "shared-gpt"
)

// codeWorkProvider is a provider of agent for the opencode-agent case, named
// head- and its CLI, in mode Text, signed in as the API admits: on its
// headless token, its key, its model provider's key in env, or a bundle, the
// first its CLI takes.
func codeWorkProvider(agent v1alpha1.CLIAgentName) v1alpha1.LLMProvider {
	key := func(name string) v1alpha1.SecretKeyReference {
		return v1alpha1.SecretKeyReference{Name: name, Key: keyAPIKey}
	}
	p := v1alpha1.LLMProvider{
		Namespace: testNamespace,
		Name:      "head-" + strings.ToLower(string(agent)),
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef:          v1alpha1.CompanyReference{Name: "harbor"},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{Agent: agent, Model: codeWorkModel},
		},
	}
	switch agent {
	case v1alpha1.CLIAgentClaudeCode:
		p.Spec.Auth.TokenSecretRef = key(p.Name)
	case v1alpha1.CLIAgentCodex, v1alpha1.CLIAgentGeminiCLI, v1alpha1.CLIAgentQwenCode,
		v1alpha1.CLIAgentCursorAgent, v1alpha1.CLIAgentCopilot, v1alpha1.CLIAgentGrok, v1alpha1.CLIAgentMuseCode:
		p.Spec.Auth = v1alpha1.CLIAgentAuth{Mode: v1alpha1.CLIAgentAuthAPIKey, APIKeySecretRef: key(p.Name)}
	case v1alpha1.CLIAgentOpenCode, v1alpha1.CLIAgentHermes, v1alpha1.CLIAgentPi:
		p.Spec.Auth.Mode = v1alpha1.CLIAgentAuthSubscription
		p.Spec.Env = []v1alpha1.SecretEnvVar{{
			Name:      envGatewayKey,
			ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: key("openrouter")},
		}}
	case v1alpha1.CLIAgentKimiCode:
		p.Spec.Auth.CredentialBundleSecretRef = v1alpha1.SecretKeyReference{Name: p.Name, Key: keyBundle}
	}
	return p
}

// codeWorkKeyed is a provider of agent for the opencode-agent case, named
// key- and its CLI, signed in with auth mode APIKey, whose key every box of
// the seat's code work carries.
func codeWorkKeyed(agent v1alpha1.CLIAgentName) v1alpha1.LLMProvider {
	p := codeWorkProvider(agent)
	p.Name = "key-" + strings.ToLower(string(agent))
	p.Spec.Env = nil
	p.Spec.Auth = v1alpha1.CLIAgentAuth{
		Mode:            v1alpha1.CLIAgentAuthAPIKey,
		APIKeySecretRef: v1alpha1.SecretKeyReference{Name: p.Name, Key: keyAPIKey},
	}
	return p
}

// codeWorkBundled is a provider of agent for the opencode-agent case, named
// bundle- and its CLI, signed in by a credential bundle alone, whose login
// cannot follow a run into a remote sandbox.
func codeWorkBundled(agent v1alpha1.CLIAgentName) v1alpha1.LLMProvider {
	p := codeWorkProvider(agent)
	p.Name = "bundle-" + strings.ToLower(string(agent))
	p.Spec.Env = nil
	p.Spec.Auth = v1alpha1.CLIAgentAuth{
		CredentialBundleSecretRef: v1alpha1.SecretKeyReference{Name: p.Name, Key: keyBundle},
	}
	return p
}

// codeWorkPlatform gives the opencode-agent case's class the platform
// provider platformGPT, driving agent on a key the way codeWorkProvider signs
// such a CLI in, at the head of the order of its Company, which uses it, and
// makes fixer a seat with no chain of its own, which runs on it.
func codeWorkPlatform(agent v1alpha1.CLIAgentName) func(*Input) {
	return func(in *Input) {
		p := v1alpha1.PlatformLLMProvider{Name: platformGPT, Agent: agent, Model: codeWorkModel}
		key := v1alpha1.NamespacedSecretKeyReference{Namespace: "crewlet-system", Name: platformGPT, Key: keyAPIKey}
		if agent == v1alpha1.CLIAgentOpenCode {
			p.Auth.Mode = v1alpha1.CLIAgentAuthSubscription
			p.Env = []v1alpha1.NamespacedSecretEnvVar{{
				Name: envGatewayKey, ValueFrom: v1alpha1.NamespacedSecretValueSource{SecretKeyRef: key},
			}}
		} else {
			p.Auth = v1alpha1.PlatformLLMProviderAuth{Mode: v1alpha1.CLIAgentAuthAPIKey, APIKeySecretRef: key}
		}
		in.Company.Spec.LLM.UsePlatformProviders = new(true)
		in.Class.Spec.PlatformLLMProviders = []v1alpha1.PlatformLLMProvider{p}
		in.Company.Spec.LLM.Order = []v1alpha1.LLMProviderReference{
			{Kind: v1alpha1.LLMProviderKindPlatform, Name: platformGPT},
		}
		seat(in, codeWorkSeat).Spec.LLM = v1alpha1.SeatLLM{}
	}
}

// codeWorkInput is the opencode-agent case with seat fixer's code work on
// head, a new provider, driven by runner.
func codeWorkInput(t *testing.T, head *v1alpha1.LLMProvider, runner v1alpha1.CodingAgent) *Input {
	t.Helper()
	in := companyCaseNamed(t, "opencode-agent").input(t)
	in.LLMProviders = append(in.LLMProviders, *head)
	s := seat(in, codeWorkSeat)
	s.Spec.LLM.Sandbox = []v1alpha1.LLMProviderReference{{Name: head.Name}}
	s.Spec.Sandbox.CodingAgent = runner
	return in
}

// TestCodeWork covers every seat whose code work no run_sandbox launch could
// run: a provider driving another CLI than its coding agent, which the render
// refuses as Crewlet does on every write, and a login a remote sandbox cannot
// carry, which Crewlet applies and the render reports without refusing the
// document. Each is reported at the field that names the provider, every way
// out of each the engine takes is reported runnable, and a seat Crewlet
// offers no code work is reported nothing.
func TestCodeWork(t *testing.T) {
	cli := codeWorkProvider
	bundled := codeWorkBundled
	seatEnv := func(name string) func(*Input) {
		return func(in *Input) {
			seat(in, codeWorkSeat).Spec.Sandbox.Env = []v1alpha1.SeatEnvVar{{
				Name:      name,
				ValueFrom: v1alpha1.SeatCredentialSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "seat", Key: keyValue}},
			}}
		}
	}
	setupEnv := func(company bool, name string) func(*Input) {
		return func(in *Input) {
			step := v1alpha1.SandboxSetupStep{Name: "login", Env: []v1alpha1.SandboxEnvVar{{
				Name:      name,
				ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "setup", Key: keyValue}},
			}}}
			if company {
				in.Company.Spec.Sandbox.Setup = append(in.Company.Spec.Sandbox.Setup, step)
				return
			}
			s := seat(in, codeWorkSeat)
			s.Spec.Sandbox.Setup = append(s.Spec.Sandbox.Setup, step)
		}
	}
	const (
		atSandbox = "spec.llm.sandbox[0].name "
		runnable  = atSandbox + "runnable"
		mismatch  = atSandbox + v1alpha1.ReasonCodingAgentMismatch
		stranded  = atSandbox + v1alpha1.ReasonSandboxLoginNotPortable
		// refusal is the render's refusal of a mismatch.
		refusal = "Seat/fixer " + mismatch
	)
	for _, c := range []struct {
		name   string
		head   v1alpha1.LLMProvider
		runner v1alpha1.CodingAgent
		mutate func(*Input)
		// refused is the render's refusals of the document.
		refused []string
		// reported is CodeWorkOf's entry for the seat, its field and its
		// reason, or "runnable"; empty for no entry.
		reported string
		// says is what the entry's message must say.
		says []string
	}{
		{
			name: "ClaudeCode on its key", head: codeWorkKeyed(v1alpha1.CLIAgentClaudeCode), runner: v1alpha1.CodingAgentClaudeCode,
			reported: runnable,
			says:     []string{"with the ClaudeCode coding agent on LLMProvider key-claudecode, whose model and sign-in"},
		},
		{
			// The Company's default coding agent is OpenCode; unset,
			// ClaudeCode.
			name: "the default coding agent on an OpenCode provider", head: cli(v1alpha1.CLIAgentOpenCode),
			mutate:  func(in *Input) { in.Company.Spec.Sandbox.DefaultCodingAgent = "" },
			refused: []string{refusal}, reported: mismatch,
			says: []string{"with the ClaudeCode coding agent on LLMProvider head-opencode, a provider driving OpenCode"},
		},
		{
			name: "the Company's default coding agent, OpenCode, on an OpenCode provider", head: cli(v1alpha1.CLIAgentOpenCode),
			reported: runnable,
		},
		{
			name: "ClaudeCode on a Codex provider", head: cli(v1alpha1.CLIAgentCodex), runner: v1alpha1.CodingAgentClaudeCode,
			refused: []string{refusal}, reported: mismatch,
			says: []string{"on LLMProvider head-codex, a provider driving Codex", "Name a provider driving ClaudeCode first"},
		},
		{
			name: "OpenCode on a ClaudeCode provider", head: cli(v1alpha1.CLIAgentClaudeCode), runner: v1alpha1.CodingAgentOpenCode,
			refused: []string{refusal}, reported: mismatch,
			says: []string{"Name a provider driving OpenCode first", "or set spec.sandbox.codingAgent: ClaudeCode"},
		},
		{
			name: "ClaudeCode on an OpenCode provider", head: cli(v1alpha1.CLIAgentOpenCode), runner: v1alpha1.CodingAgentClaudeCode,
			refused: []string{refusal}, reported: mismatch, says: []string{"or set spec.sandbox.codingAgent: OpenCode"},
		},
		{
			name: "ClaudeCode on its headless token", head: cli(v1alpha1.CLIAgentClaudeCode), runner: v1alpha1.CodingAgentClaudeCode,
			reported: runnable, says: []string{"on LLMProvider head-claudecode, whose model and sign-in it can use"},
		},
		{
			// The seat's own sign-in replaces the provider's, which Crewlet
			// hands the run none of.
			name: "ClaudeCode on its headless token beside the seat's key", head: cli(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: seatEnv("ANTHROPIC_API_KEY"), reported: runnable,
			says: []string{"signed in by ANTHROPIC_API_KEY from the seat's sandbox environment, which Crewlet hands " +
				"the run in place of the provider's own sign-in"},
		},
		{
			name: "OpenCode on its key in env", head: cli(v1alpha1.CLIAgentOpenCode), runner: v1alpha1.CodingAgentOpenCode,
			reported: runnable, says: []string{"on LLMProvider head-opencode, whose model and sign-in it can use"},
		},
		{
			name: "ClaudeCode on a bundle", head: bundled(v1alpha1.CLIAgentClaudeCode), runner: v1alpha1.CodingAgentClaudeCode,
			reported: stranded,
			says: []string{
				"auth.tokenSecretRef", "CLAUDE_CODE_OAUTH_TOKEN, ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN",
				"Crewlet refuses each run_sandbox launch of the seat.",
			},
		},
		{
			name: "ClaudeCode on a bundle, signed in by the seat's key", head: bundled(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: seatEnv("ANTHROPIC_API_KEY"), reported: runnable,
			says: []string{
				"on LLMProvider bundle-claudecode, whose model it uses, signed in by ANTHROPIC_API_KEY from the seat's sandbox environment",
			},
		},
		{
			// A bearer token signs ClaudeCode in as its token or key does.
			name: "ClaudeCode on a bundle beside a bearer token", head: bundled(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: seatEnv("ANTHROPIC_AUTH_TOKEN"), reported: runnable,
			says: []string{"signed in by ANTHROPIC_AUTH_TOKEN from the seat's sandbox environment"},
		},
		{
			name: "ClaudeCode on a bundle, moved onto Bedrock by the seat's setup", head: bundled(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: setupEnv(false, "CLAUDE_CODE_USE_BEDROCK"), reported: runnable,
			says: []string{"signed in by CLAUDE_CODE_USE_BEDROCK"},
		},
		{
			name: "ClaudeCode on a bundle, signed in by the Company's setup", head: bundled(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: setupEnv(true, "CLAUDE_CODE_OAUTH_TOKEN"), reported: runnable,
		},
		{
			// A key no Claude Code reads signs it in to nothing.
			name: "ClaudeCode on a bundle beside an OpenAI key", head: bundled(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: seatEnv("OPENAI_API_KEY"), reported: stranded,
		},
		{
			name: "OpenCode on a bundle", head: bundled(v1alpha1.CLIAgentOpenCode), runner: v1alpha1.CodingAgentOpenCode,
			reported: stranded, says: []string{envGatewayKey},
		},
		{
			// Any variable named as a credential passes, as it passes
			// Crewlet's guard.
			name: "OpenCode on a bundle beside a credential in the Company's setup", head: bundled(v1alpha1.CLIAgentOpenCode),
			runner: v1alpha1.CodingAgentOpenCode, mutate: setupEnv(true, "GH_TOKEN"), reported: runnable,
			says: []string{"signed in by GH_TOKEN from the seat's sandbox environment"},
		},
		{
			name: "OpenCode on a bundle beside a proxy", head: bundled(v1alpha1.CLIAgentOpenCode),
			runner: v1alpha1.CodingAgentOpenCode, mutate: seatEnv("HTTPS_PROXY"), reported: stranded,
		},
		{
			// The sandbox chain is the code work's, ahead of the seat's
			// own chain.
			name: "a Codex executor whose code work runs on ClaudeCode", head: cli(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode,
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, cli(v1alpha1.CLIAgentCodex))
				seat(in, codeWorkSeat).Spec.LLM.Default = []v1alpha1.LLMProviderReference{{Name: "head-codex"}}
			},
			reported: runnable,
		},
		{
			name: "a Codex executor that runs the code work too", head: cli(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode,
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, cli(v1alpha1.CLIAgentCodex))
				s := seat(in, codeWorkSeat)
				s.Spec.LLM.Default = []v1alpha1.LLMProviderReference{{Name: "head-codex"}}
				s.Spec.LLM.Sandbox = nil
			},
			refused:  []string{"Seat/fixer spec.llm.default[0].name " + v1alpha1.ReasonCodingAgentMismatch},
			reported: "spec.llm.default[0].name " + v1alpha1.ReasonCodingAgentMismatch,
		},
		{
			// A seat with no chain runs on the head of the document's
			// order, a platform provider among them, which signs in with a
			// key that follows the run.
			name: "a chainless seat on a platform ClaudeCode provider", head: cli(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: codeWorkPlatform(v1alpha1.CLIAgentClaudeCode),
			reported: "spec.llm runnable",
			says:     []string{"on the platform model provider shared-gpt, whose model and sign-in it can use"},
		},
		{
			name: "a chainless seat on a platform OpenCode provider", head: cli(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentOpenCode, mutate: codeWorkPlatform(v1alpha1.CLIAgentOpenCode),
			reported: "spec.llm runnable",
		},
		{
			name: "a chainless seat on a platform Codex provider", head: cli(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: codeWorkPlatform(v1alpha1.CLIAgentCodex),
			refused:  []string{"Seat/fixer spec.llm " + v1alpha1.ReasonCodingAgentMismatch},
			reported: "spec.llm " + v1alpha1.ReasonCodingAgentMismatch,
			says:     []string{"on the platform model provider shared-gpt, a provider driving Codex"},
		},
		{
			// Crewlet offers no run_sandbox without the Company's
			// sandbox, and refuses the seat's gate itself.
			name: "a company with no sandbox", head: cli(v1alpha1.CLIAgentCodex), runner: v1alpha1.CodingAgentClaudeCode,
			mutate: func(in *Input) {
				in.Company.Spec.Sandbox = v1alpha1.SandboxSettings{}
				in.LLMProviders = slices.DeleteFunc(in.LLMProviders, func(p v1alpha1.LLMProvider) bool {
					return p.Spec.Mode == v1alpha1.CLIAgentModeAgent
				})
				seat(in, "porter").Spec.LLM.Default = nil
			},
		},
		{
			name: "a seat with no sandbox", head: cli(v1alpha1.CLIAgentCodex), runner: v1alpha1.CodingAgentClaudeCode,
			mutate: func(in *Input) { seat(in, codeWorkSeat).Spec.Sandbox = nil },
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := codeWorkInput(t, &c.head, c.runner)
			if c.mutate != nil {
				c.mutate(in)
			}
			_, err := Company(in)
			if got := refusalsOf(err); !slices.Equal(got, c.refused) {
				t.Fatalf("refused %v (%v), want %v", got, err, c.refused)
			}
			w, ok := CodeWorkOf(in)[codeWorkSeat]
			got := ""
			if ok {
				got = w.Field + " " + cmp.Or(w.Reason, "runnable")
			}
			if got != c.reported {
				t.Fatalf("reported %q (%s), want %q", got, w.Message, c.reported)
			}
			for _, phrase := range c.says {
				if !strings.Contains(w.Message, phrase) {
					t.Errorf("the report does not say %q: %s", phrase, w.Message)
				}
			}
			if refusal := slices.IndexFunc(RefErrors(err), func(r *RefError) bool {
				return r.Kind == KindSeat && r.Name == codeWorkSeat
			}); refusal >= 0 && RefErrors(err)[refusal].Message != w.Message {
				t.Errorf("the refusal says %q, the report %q", RefErrors(err)[refusal].Message, w.Message)
			}
		})
	}
}

// The agent-mode seat of the opencode-agent case, whose executor is
// LLMProvider opencode, OpenCode in mode Agent, and the field the render
// refuses on it.
const (
	agentModeSeat     = "porter"
	agentModeProvider = "opencode"
	atCodingAgent     = "spec.sandbox.codingAgent"
)

// TestAgentModeSeatHasNoCodeWork covers a seat whose executor runs in agent
// mode, which Crewlet never offers run_sandbox: it codes in its executor's
// run, on that provider's CLI, so the render reports no code work for it
// whatever its sandbox chain and coding agent, and refuses, as Crewlet does
// on every write, a spec.sandbox.codingAgent other than that CLI, which
// drives nothing. The Company's default coding agent is not the seat's own,
// and is refused nowhere.
func TestAgentModeSeatHasNoCodeWork(t *testing.T) {
	refusal := "Seat/" + agentModeSeat + " " + atCodingAgent + " " + v1alpha1.ReasonCodingAgentMismatch
	sandboxOn := func(head v1alpha1.LLMProvider, runner v1alpha1.CodingAgent) func(*Input) {
		return func(in *Input) {
			in.LLMProviders = append(in.LLMProviders, head)
			s := seat(in, agentModeSeat)
			s.Spec.LLM.Sandbox = []v1alpha1.LLMProviderReference{{Name: head.Name}}
			s.Spec.Sandbox.CodingAgent = runner
		}
	}
	for _, c := range []struct {
		name   string
		mutate func(*Input)
		// refused is the render's refusals of the document.
		refused []string
		// says is what the refusal's message must say.
		says []string
		// reported is whether CodeWorkOf has an entry for the seat.
		reported bool
	}{
		{name: "a seat that names no coding agent"},
		{
			// Its own CLI's coding agent is the run it codes in.
			name: "a seat naming its executor's own CLI",
			mutate: func(in *Input) {
				seat(in, agentModeSeat).Spec.Sandbox.CodingAgent = v1alpha1.CodingAgentOpenCode
			},
		},
		{
			// No run_sandbox launch ever pairs the coding agent with a
			// provider of another CLI.
			name:   "a seat whose sandbox chain is a Codex provider",
			mutate: sandboxOn(codeWorkProvider(v1alpha1.CLIAgentCodex), v1alpha1.CodingAgentOpenCode),
		},
		{
			name:    "a seat naming Claude Code beside a ClaudeCode sandbox chain",
			mutate:  sandboxOn(codeWorkProvider(v1alpha1.CLIAgentClaudeCode), v1alpha1.CodingAgentClaudeCode),
			refused: []string{refusal},
			says: []string{
				"names the ClaudeCode coding agent, but the seat's executor runs in agent mode on LLMProvider " +
					"opencode, at spec.llm.default[0].name:",
				"in that OpenCode run", "Leave it out",
			},
		},
		{
			// The Company's default is the catalogue's, which an agent-mode
			// seat's run never reads. The other seat's code work keeps to
			// OpenCode, the CLI of its sandbox chain.
			name: "the Company's default coding agent, Claude Code",
			mutate: func(in *Input) {
				in.Company.Spec.Sandbox.DefaultCodingAgent = v1alpha1.CodingAgentClaudeCode
				seat(in, codeWorkSeat).Spec.Sandbox.CodingAgent = v1alpha1.CodingAgentOpenCode
			},
		},
		{
			name: "a chainless seat on the agent-mode head of the Company's order naming Claude Code",
			mutate: func(in *Input) {
				in.Company.Spec.LLM.Order = []v1alpha1.LLMProviderReference{{Name: agentModeProvider}}
				s := seat(in, agentModeSeat)
				s.Spec.LLM.Default = nil
				s.Spec.Sandbox.CodingAgent = v1alpha1.CodingAgentClaudeCode
			},
			refused: []string{refusal},
			says:    []string{"on LLMProvider opencode, which a seat whose spec.llm.default is empty runs on:"},
		},
		{
			// With no executor in agent mode it is an ordinary seat's code
			// work again, judged on its sandbox chain.
			name: "the seat in text mode",
			mutate: func(in *Input) {
				for i := range in.LLMProviders {
					if in.LLMProviders[i].Name == agentModeProvider {
						in.LLMProviders[i].Spec.Mode = v1alpha1.CLIAgentModeText
					}
				}
				seat(in, agentModeSeat).Spec.Sandbox.CodingAgent = v1alpha1.CodingAgentClaudeCode
			},
			refused:  []string{"Seat/" + agentModeSeat + " " + fieldLLMDefault + "[0].name " + v1alpha1.ReasonCodingAgentMismatch},
			reported: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, "opencode-agent").input(t)
			if c.mutate != nil {
				c.mutate(in)
			}
			_, err := Company(in)
			if got := refusalsOf(err); !slices.Equal(got, c.refused) {
				t.Fatalf("refused %v (%v), want %v", got, err, c.refused)
			}
			if _, reported := CodeWorkOf(in)[agentModeSeat]; reported != c.reported {
				t.Errorf("CodeWorkOf reports the seat's code work: %v, want %v", reported, c.reported)
			}
			for _, phrase := range c.says {
				if !strings.Contains(err.Error(), phrase) {
					t.Errorf("the refusal does not say %q: %v", phrase, err)
				}
			}
		})
	}
}

// TestContractCodeWorkOnWrite holds the render's verdict on a seat's code work
// to the engine's on a write. Every coding agent the API offers is paired
// with a provider of every CLI the API offers at the head of the seat's
// sandbox chain, signed in as codeWorkProvider signs it and, for each coding
// agent's own CLI, by a credential bundle alone, and ClaudeCode on its key:
// where the render refuses the seat (CodingAgentMismatch), `crewlet validate`
// refuses the document at the seat's llm_sandbox, and where it only reports
// the seat (SandboxLoginNotPortable) the engine accepts the document the
// render writes, which is what makes reporting rather than refusing it right:
// an engine that refused it on a write would hold the company on its last
// revision over it anyway. A seat whose executor runs in agent mode is paired
// with each coding agent, and none, and with a sandbox chain of a provider of
// another CLI and of ClaudeCode: the render reports no code work for it and
// refuses a coding agent other than its executor's CLI alone, and `crewlet
// validate` refuses that alone, at the seat's sandbox coding_agent, and never
// its sandbox chain. A refused seat is handed to the engine in the document
// the render writes without its sandbox, with the block put back as the
// render writes it.
func TestContractCodeWorkOnWrite(t *testing.T) {
	runners := []v1alpha1.CodingAgent{v1alpha1.CodingAgentClaudeCode, v1alpha1.CodingAgentOpenCode}
	agents := cliAgentNames(t)
	heads := make([]v1alpha1.LLMProvider, 0, 3+len(agents))
	for _, agent := range agents {
		heads = append(heads, codeWorkProvider(agent))
	}
	heads = append(heads, codeWorkKeyed(v1alpha1.CLIAgentClaudeCode),
		codeWorkBundled(v1alpha1.CLIAgentClaudeCode), codeWorkBundled(v1alpha1.CLIAgentOpenCode))
	reasons := map[string]bool{}
	for _, runner := range runners {
		for _, head := range heads {
			in := codeWorkInput(t, &head, runner)
			w, ok := CodeWorkOf(in)[codeWorkSeat]
			if !ok {
				t.Fatalf("%s on %s: the render reports no code work", runner, head.Name)
			}
			reasons[w.Reason] = true
			t.Run(string(runner)+" on "+head.Name, func(t *testing.T) {
				refused := w.Reason == v1alpha1.ReasonCodingAgentMismatch
				doc, err := Company(in)
				if refused != (err != nil) {
					t.Fatalf("the render reports %q and refuses the document with %v", w.Reason, err)
				}
				var document []byte
				if refused {
					doc, document = withoutThenWithSandbox(t, codeWorkInput(t, &head, runner), codeWorkSeat, runner)
				} else {
					document = doc.YAML
				}
				engineRefuses := engineRefusesOnlyAt(t, doc.Refs, document, "llm_sandbox")
				if refused != engineRefuses {
					t.Errorf("the render refuses the pairing (%q): %v; the engine refuses it: %v", w.Reason, refused, engineRefuses)
				}
			})
		}
	}
	agentModeInput := func(head v1alpha1.LLMProvider, runner v1alpha1.CodingAgent) *Input {
		in := companyCaseNamed(t, "opencode-agent").input(t)
		in.LLMProviders = append(in.LLMProviders, head)
		s := seat(in, agentModeSeat)
		s.Spec.LLM.Sandbox = []v1alpha1.LLMProviderReference{{Name: head.Name}}
		s.Spec.Sandbox.CodingAgent = runner
		return in
	}
	for _, runner := range []v1alpha1.CodingAgent{"", v1alpha1.CodingAgentOpenCode, v1alpha1.CodingAgentClaudeCode} {
		for _, head := range []v1alpha1.LLMProvider{
			codeWorkProvider(v1alpha1.CLIAgentCodex),
			codeWorkProvider(v1alpha1.CLIAgentClaudeCode),
		} {
			t.Run(fmt.Sprintf("an agent-mode OpenCode seat naming %q on %s", runner, head.Name), func(t *testing.T) {
				in := agentModeInput(head, runner)
				if w, ok := CodeWorkOf(in)[agentModeSeat]; ok {
					t.Errorf("the render reports code work for the seat: %s %s", w.Field, w.Message)
				}
				doc, err := Company(in)
				refused := err != nil
				want := []string{"Seat/" + agentModeSeat + " " + atCodingAgent + " " + v1alpha1.ReasonCodingAgentMismatch}
				if refused && !slices.Equal(refusalsOf(err), want) {
					t.Fatalf("the render refuses %v (%v), want %v or nothing", refusalsOf(err), err, want)
				}
				var document []byte
				if refused {
					doc, document = withoutThenWithSandbox(t, agentModeInput(head, runner), agentModeSeat, runner)
				} else {
					document = doc.YAML
				}
				engineRefuses := engineRefusesOnlyAt(t, doc.Refs, document, "coding_agent")
				if refused != engineRefuses {
					t.Errorf("the render refuses the coding agent: %v; the engine refuses it: %v", refused, engineRefuses)
				}
			})
		}
	}
	// The matrix sends the engine every verdict the render reaches, so that
	// a change to the heads cannot quietly stop asking about one.
	for _, reason := range []string{
		"", v1alpha1.ReasonCodingAgentMismatch, v1alpha1.ReasonSandboxLoginNotPortable,
	} {
		if !reasons[reason] {
			t.Errorf("no pairing reaches %q", reason)
		}
	}
}

// withoutThenWithSandbox is the document the render writes from in without
// the sandbox of seat name, which it refuses with it, and that document with
// the seat's sandbox block put back as the render writes it, naming runner.
func withoutThenWithSandbox(t *testing.T, in *Input, name string, runner v1alpha1.CodingAgent) (*Document, []byte) {
	t.Helper()
	seat(in, name).Spec.Sandbox = nil
	doc, err := Company(in)
	if err != nil {
		t.Fatalf("rendering the seat without its sandbox: %v", err)
	}
	gate, err := json.Marshal(roleSandboxDoc{Enabled: true, CodingAgent: codingAgent(runner)})
	if err != nil {
		t.Fatal(err)
	}
	return doc, withRoleSandbox(t, doc.YAML, name, gate)
}

// engineRefusesOnlyAt is whether `crewlet validate`, given a value for every
// Secret key refs names, refuses document at a path whose last segment is
// last; a refusal anywhere else fails the test.
func engineRefusesOnlyAt(t *testing.T, refs []secretref.Key, document []byte, last string) bool {
	t.Helper()
	env := map[string]string{}
	for _, k := range refs {
		env[secretsync.StoreName(k)] = dummyCredential(k)
	}
	got, _, _, _ := verdict(t, env, "-tier", "company", write(t, "company.yaml", document))
	refuses := false
	for _, raw := range got.Problems {
		var p struct {
			Segments []engine.Segment `json:"segments"`
			Message  string           `json:"message"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		if path := PathOf(p.Segments); len(path) > 0 && path[len(path)-1] == last {
			refuses = true
			continue
		}
		t.Errorf("crewlet validate refuses the document elsewhere: %s", raw)
	}
	return refuses
}

// withRoleSandbox is document with the role of handle given gate as its
// sandbox block.
func withRoleSandbox(t *testing.T, document []byte, handle string, gate []byte) []byte {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal(document, &doc); err != nil {
		t.Fatal(err)
	}
	var block map[string]any
	if err := json.Unmarshal(gate, &block); err != nil {
		t.Fatal(err)
	}
	roles, _ := doc["roles"].([]any)
	for _, r := range roles {
		if role, ok := r.(map[string]any); ok && role["handle"] == handle {
			role["sandbox"] = block
			out, err := yaml.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	t.Fatalf("the document has no role %s", handle)
	return nil
}

// cliAgentNames is every CLI the API offers: the enum of the installed
// LLMProvider CRD's spec.agent, so a CLI added later is paired too.
func cliAgentNames(t *testing.T) []v1alpha1.CLIAgentName {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "crewlet.ai_llmproviders.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var crd struct {
		Spec struct {
			Versions []struct {
				Schema struct {
					OpenAPIV3Schema struct {
						Properties struct {
							Spec struct {
								Properties struct {
									Agent struct {
										Enum []v1alpha1.CLIAgentName `json:"enum"`
									} `json:"agent"`
								} `json:"properties"`
							} `json:"spec"`
						} `json:"properties"`
					} `json:"openAPIV3Schema"`
				} `json:"schema"`
			} `json:"versions"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(raw, &crd); err != nil || len(crd.Spec.Versions) != 1 {
		t.Fatalf("reading the LLMProvider CRD's versions (%v)", err)
	}
	agents := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties.Spec.Properties.Agent.Enum
	if len(agents) == 0 {
		t.Fatal("the LLMProvider CRD offers no spec.agent")
	}
	return agents
}

// TestCodeWorkFollowsTheEngineSource holds the render's copies of what a
// launch runs and signs in, which no validation shows, to the engine's
// source: the coding agent of a seat and a catalogue that name none
// (sandbox.DefaultCodingAgent), the variables that sign ClaudeCode in from a
// run's environment (codingagent.ClaudeCodeCredentialEnv), and among them
// ClaudeCode's token and key variables (its profile's token_env and
// api_key_env), which carry a provider's own sign-in into the run. The pattern
// of a credential's name is held by TestCredentialNameFollowsTheEngineSource.
func TestCodeWorkFollowsTheEngineSource(t *testing.T) {
	source, err := os.OpenRoot(enginetest.Source(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })

	const manager = "internal/sandbox/manager.go"
	src, err := source.ReadFile(manager)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stringConst(t, manager, src, "DefaultCodingAgent"), codingAgent(defaultCodingAgent); got != want {
		t.Errorf("the engine runs %s for a seat and a catalogue that name no coding agent, the render %s", got, want)
	}

	const file = "internal/sandbox/codingagent/claudecode.go"
	if src, err = source.ReadFile(file); err != nil {
		t.Fatal(err)
	}
	if got := stringSliceVar(t, file, src, "ClaudeCodeCredentialEnv"); !slices.Equal(got, claudeCodeSignIn) {
		t.Errorf("the engine signs ClaudeCode in from a run's %v, the render from %v", got, claudeCodeSignIn)
	}

	raw, err := source.ReadFile("internal/providers/llm/cliagent/cliprofile/profiles.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var profiles map[string]struct {
		TokenEnv  string `json:"token_env"`
		APIKeyEnv string `json:"api_key_env"`
	}
	if err := yaml.Unmarshal(raw, &profiles); err != nil {
		t.Fatal(err)
	}
	claude, ok := profiles[cliAgentName(v1alpha1.CLIAgentClaudeCode)]
	for _, name := range []string{claude.TokenEnv, claude.APIKeyEnv} {
		if !ok || name == "" || !slices.Contains(claudeCodeSignIn, name) {
			t.Errorf("the engine's claude-code profile hands its own sign-in to a run in %q (token_env %q, "+
				"api_key_env %q), which the render does not read as signing ClaudeCode in: %v",
				name, claude.TokenEnv, claude.APIKeyEnv, claudeCodeSignIn)
		}
	}
}

// profileFile is the engine's file of CLI profiles, which declares how it
// reads a credential's name (IsCredentialName and its credentialWords).
const profileFile = "internal/providers/llm/cliagent/cliprofile/profile.go"

// credentialNameRules are the kinds whose CRDs read a credential's name, and
// how many of their rules do (every other kind's, none): the LLMProvider
// API's two over a CLI's sign-in in env and the platform provider's one, and
// the tool-server API's refusal of a literal value under such a name, on each
// type that takes one (an MCPServer's env and headers, and the values of a
// Seat's or a Unit's spec.mcp).
var credentialNameRules = map[string]int{
	"engineclasses": 1,
	"llmproviders":  2,
	"mcpservers":    2,
	"seats":         1,
	"units":         1,
}

// TestCredentialNameFollowsTheEngineSource holds the one pattern the operator
// reads a credential's name with to the engine's source, and every rule of
// the API that reads one to that pattern. The words are Crewlet's
// credentialWords, in its order; how a name is cut into parts is
// IsCredentialName's, which TestMirroredEngineDeclarationsAreUnchanged holds, as
// it holds the function and not the list. The LLMProvider API's rules are
// also held to the engine's sign-in by the CRD suite, and the tool-server
// API's to the names the engine reads a seat's tool credentials under.
func TestCredentialNameFollowsTheEngineSource(t *testing.T) {
	source := openSource(t, enginetest.Source(t))
	src, err := source.ReadFile(profileFile)
	if err != nil {
		t.Fatal(err)
	}
	words := stringSliceVar(t, profileFile, src, "credentialWords")
	const head, tail = `(?i)(^|[^A-Za-z0-9])(`, `)([^A-Za-z0-9]|$)`
	if want := head + strings.Join(words, "|") + tail; credentialNamePattern != want {
		t.Errorf("the engine reads a credential's name as one whose part is one of %v, which is the pattern %q; "+
			"the operator's is %q", words, want, credentialNamePattern)
	}

	files, err := filepath.Glob(filepath.Join("..", "..", "config", "crd", "bases", "crewlet.ai_*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("the CRDs in config/crd/bases: %v", err)
	}
	for _, file := range files {
		kind := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(file), "crewlet.ai_"), ".yaml")
		want := credentialNameRules[kind]
		crd, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			t.Fatal(err)
		}
		var schema any
		if err := yaml.Unmarshal(crd, &schema); err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, rule := range rules(schema) {
			// A rule reads a credential's name where it names the words.
			if !strings.Contains(rule, "|APIKEY|") {
				continue
			}
			found++
			_, rest, ok := strings.Cut(rule, "name.matches('")
			pattern, _, _ := strings.Cut(rest, "')")
			if !ok || pattern != credentialNamePattern {
				t.Errorf("a rule of the %s CRD reads a credential's name other than as %q: %s",
					kind, credentialNamePattern, rule)
			}
		}
		if found != want {
			t.Errorf("%d rules of the %s CRD read a credential's name, want %d", found, kind, want)
		}
	}
}

// stringSliceVar is the string literals of the []string var name declares in
// the Go source src of file.
func stringSliceVar(t *testing.T, file string, src []byte, name string) []string {
	t.Helper()
	return stringLiterals(t, file, name, declared(t, file, src, token.VAR, name))
}

// stringLiterals is the string literals of e, a literal of a []string of
// name's in file.
func stringLiterals(t *testing.T, file, name string, e ast.Expr) []string {
	t.Helper()
	list, ok := e.(*ast.CompositeLit)
	if !ok {
		t.Fatalf("%s in %s is no literal", name, file)
	}
	out := make([]string, 0, len(list.Elts))
	for _, elt := range list.Elts {
		out = append(out, stringLiteral(t, file, name, elt))
	}
	return out
}

// stringConst is the string literal the const name declares in the Go source
// src of file.
func stringConst(t *testing.T, file string, src []byte, name string) string {
	t.Helper()
	return stringLiteral(t, file, name, declared(t, file, src, token.CONST, name))
}

// stringLiteral is the value of e, a string literal of name's in file.
func stringLiteral(t *testing.T, file, name string, e ast.Expr) string {
	t.Helper()
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		t.Fatalf("%s in %s holds a value that is no string literal", name, file)
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// declared is the value a var or const (tok) of name alone is declared with
// in the Go source src of file.
func declared(t *testing.T, file string, src []byte, tok token.Token, name string) ast.Expr {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != tok {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if ok && len(value.Names) == 1 && value.Names[0].Name == name && len(value.Values) == 1 {
				return value.Values[0]
			}
		}
	}
	t.Fatalf("%s declares no %s %s", file, tok, name)
	return nil
}

// rules is every x-kubernetes-validations rule in a decoded CRD.
func rules(node any) []string {
	var out []string
	switch n := node.(type) {
	case map[string]any:
		if validations, ok := n["x-kubernetes-validations"].([]any); ok {
			for _, v := range validations {
				if rule, ok := v.(map[string]any)["rule"].(string); ok {
					out = append(out, rule)
				}
			}
		}
		for _, child := range n {
			out = append(out, rules(child)...)
		}
	case []any:
		for _, child := range n {
			out = append(out, rules(child)...)
		}
	}
	return out
}
