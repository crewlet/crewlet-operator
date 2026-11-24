package render

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// What signs a seat's executor run in where it runs.
//
// A run is handed the environment of the provider it launches on, beneath the
// seat's own environment (Crewlet's entryEnv): the provider's whole env, a
// gateway's address and the bearer token it takes or a cloud switch
// included, with its sign-in, its headless token or its API key, in either
// cell ((*Provider).SandboxEnv). A Direct box of a provider that signs in with
// a login, under Subscription, is also seeded with it, its credential files
// or the document OpenCode 2's export printed, and writes a refreshed one
// back, so on the engine pod a credential bundle signs a run in as it signs
// the provider's other phases in; one under APIKey or Env is seeded with none
// ((*Provider).SandboxCredentials), as its key or its env is its sign-in. An E2B
// box never receives it: a bundle's login carries a refresh token the engine
// keeps rotating, and the run there needs a variable of its environment that
// signs its harness in, a token or a key, the provider's env under auth mode Env
// (a gateway's token or a cloud switch for ClaudeCode, Vertex AI's switch for
// GeminiCLI), or an OpenCode, Hermes or Pi
// provider's key in its env (executorSignIn), as a Hermes login (the
// .hermes/auth.json of its home) or a Pi login (.pi/agent/auth.json) is a
// login like any other.
// Crewlet refuses each launch of a run whose box could not authenticate,
// judged on the member the launch picked (sandboxCredentials), and never
// refuses a Direct one for it (Placement.OnEngineHost).
//
// The seat's own environment is the union of its spec.sandbox.env, its
// spec.sandbox.setup steps' env and the Company's spec.sandbox.setup steps'
// env, which Crewlet merges into every run (runEnvironment). The operator
// sees the names a Secret's values are given under, not the values: a name
// counts here where Crewlet would count a non-blank value under it. A run
// starts without every value that resolves to nothing, whether or not an
// optionalKeys list names its key, so a name counts for nothing where its
// value is known to hold nothing once the reconcile has read the Secrets: an
// empty value (Input.Empty), and one missing under a key its block declares
// optional (Input.Unset). A value missing under a key that is not optional
// holds the document, whose running revision keeps the value it had, so it
// counts.
//
// The seat's own credential at an integration, handed to a tool in its box,
// never counts: a value the engine provisions for the seat, and one under a
// Secret key the seat gives one of its credentials under (seatCredentialKeys),
// such as doctl's DIGITALOCEAN_ACCESS_TOKEN set to the seat's DigitalOcean
// token. Crewlet keeps it in the box and judges the run's sign-in without it
// (newExecutorRun, over runEnvironment's tools), although OpenCode reads that
// name as a model provider's key, and refuses it under a name the seat's
// executor signs in by (its validateSeatSignIns), which the engine's dry run
// reports as Invalid on the Seat.
//
// Under auth mode Env the provider's env is its whole sign-in: Crewlet hands
// the harness neither its token variable nor its key variable and seeds no login,
// in the engine's pod and in a box of either cell ((*Provider).SandboxEnv),
// so the run signs in, Direct or E2B, by what env carries, which the API
// requires to sign the harness in. Under every other mode the API keeps
// a harness's env sign-ins (its sign_in_env) out of env, so there its sign-in
// is its token, its key or its login.
//
// A seat whose environment signs in a harness whose profile names a
// sign_in_env (ClaudeCode, GeminiCLI, Copilot) runs its run on that sign-in alone:
// Crewlet hands the run none of the provider's sign-in, its token, its key
// and every sign-in its env carries, but the rest of its env (entryEnv,
// entrySignInName), since such a harness ranks its sign-ins by name, and its
// guard passes the run on any of them (sandboxCredentials, through its
// entrySignsIn). Crewlet decides that by the profile's list of the names that
// sign the harness in (CredentialEnvNames). A GitHub integration's
// GITHUB_TOKEN, which the engine puts in a box for the agent's tools, is one
// of Copilot's, and the engine withholds it from a Copilot box rather than
// let it sign the run in; the render never counts it (runEnvNames).
//
// Crewlet decides all of it at a launch, where no validation shows it, so
// TestMirroredEngineDeclarationsAreUnchanged holds the engine functions this
// copies to the ones it was read against, and fails when one changes.

// claudeCodeEnvSignIn are the variables that sign ClaudeCode in from a
// provider's env, beyond its token and key variables: a gateway's bearer
// token, or a toggle moving it onto a cloud provider's credentials (the
// sign_in_env of Crewlet's claude-code profile). Under auth mode Env the API
// requires one in env, and under every other mode it refuses each there.
var claudeCodeEnvSignIn = []string{
	"ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
	"CLAUDE_CODE_USE_ANTHROPIC_AWS", "CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD", "CLAUDE_CODE_USE_MANTLE",
	"CLAUDE_CODE_USE_GATEWAY",
}

// profileSignIn is what the engine's profile of a harness that runs a seat's
// executor says signs it in inside a box: its token_env, its api_key_env, its
// sign_in_env and its env_sign_in, which Crewlet's CredentialEnvNames reads.
type profileSignIn struct {
	tokenEnv, apiKeyEnv string
	signInEnv           []string
	// envSignIn is a harness that reads its model provider's key from its
	// environment, which any name read as a credential signs in.
	envSignIn bool
}

// names are the variables that sign the harness in from a run's environment
// whatever the provider's env, in the order of Crewlet's CredentialEnvNames.
func (p profileSignIn) names() []string {
	var out []string
	for _, name := range slices.Concat([]string{p.tokenEnv, p.apiKeyEnv}, p.signInEnv) {
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// executorSignIn is the sign-in of each harness in ExecutorHarnesses, to whose
// profiles TestExecutorSignInFollowsTheEngineSource holds it. When the seat's own
// environment sets one of the names of a harness with a sign_in_env, Crewlet
// hands the run none of its provider's sign-in (its entryEnv), since such a
// harness ranks its sign-ins by name and a provider's key beside a seat's plan
// token would win, and its guard passes the run on it, as on one the
// provider's env carries into the run (sandboxCredentials, through its
// entrySignsIn).
//
//nolint:gosec // G101: the names of the variables that sign a harness in, which hold no credential.
var executorSignIn = map[v1alpha1.Harness]profileSignIn{
	v1alpha1.HarnessClaudeCode: {
		tokenEnv: "CLAUDE_CODE_OAUTH_TOKEN", apiKeyEnv: "ANTHROPIC_API_KEY", signInEnv: claudeCodeEnvSignIn,
	},
	v1alpha1.HarnessCodex:       {apiKeyEnv: "CODEX_API_KEY"},
	v1alpha1.HarnessGeminiCLI:   {apiKeyEnv: "GEMINI_API_KEY", signInEnv: []string{"GOOGLE_GENAI_USE_VERTEXAI"}},
	v1alpha1.HarnessQwenCode:    {apiKeyEnv: "OPENAI_API_KEY"},
	v1alpha1.HarnessOpenCode:    {envSignIn: true},
	v1alpha1.HarnessCursorAgent: {apiKeyEnv: "CURSOR_API_KEY"},
	v1alpha1.HarnessCopilot:     {apiKeyEnv: "COPILOT_GITHUB_TOKEN", signInEnv: []string{"GH_TOKEN", "GITHUB_TOKEN"}},
	v1alpha1.HarnessGrok:        {apiKeyEnv: "XAI_API_KEY"},
	v1alpha1.HarnessMuseCode:    {apiKeyEnv: "META_API_KEY"},
	v1alpha1.HarnessKimiCode:    {apiKeyEnv: "KIMI_MODEL_API_KEY"},
	v1alpha1.HarnessHermes:      {envSignIn: true},
	v1alpha1.HarnessPi:          {envSignIn: true},
}

// credentialName is the names Crewlet reads as holding a credential, which
// sign an env sign-in harness in: one of their parts, cut at every character that
// is not a letter or a digit, is, in any case, one of the words below
// (harnessprofile.IsCredentialName). It is the pattern of every rule of the
// API that reads a credential's name, to which a test holds this copy: the
// LLMProvider and platform provider API's rules over env, which the CRD suite
// holds to the engine's sign-in, and the tool-server API's refusal of a literal value
// under such a name, which the CRD suite holds to the names the engine reads
// a seat's tool credential under.
var credentialName = regexp.MustCompile(credentialNamePattern)

//nolint:gosec // G101: the pattern a credential's name matches, which holds no credential.
const credentialNamePattern = `(?i)(^|[^A-Za-z0-9])(KEY|KEYS|APIKEY|ACCESSKEY|SECRETKEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|CREDENTIALS|AUTH|OAUTH|PAT)([^A-Za-z0-9]|$)`

// harnessEntry is what the verdicts on a seat read of a model provider's harness, an
// LLMProvider's or a platform provider's, the two kinds read alike.
type harnessEntry struct {
	harness v1alpha1.Harness
	// runIn is the cell an LLMProvider names for its executor runs; a
	// platform provider names none.
	runIn v1alpha1.SandboxPlacement
	// authMode is auth.mode, and token and bundle whether auth names a
	// headless token or a credential bundle.
	authMode      v1alpha1.HarnessAuthMode
	token, bundle bool
	// env is the names of the provider's env.
	env []string
}

// ownEntry is what the verdicts read of an LLMProvider's spec.
func ownEntry(s *v1alpha1.LLMProviderSpec) harnessEntry {
	e := harnessEntry{
		harness: s.Harness, runIn: s.RunIn, authMode: s.Auth.Mode,
		token: s.Auth.TokenSecretRef.Name != "", bundle: s.Auth.CredentialBundleSecretRef.Name != "",
	}
	for _, v := range s.Env {
		e.env = append(e.env, v.Name)
	}
	return e
}

// platformEntry is what the verdicts read of a platform provider: a harness
// signed in with a key, never a token or a bundle, and placed by no runIn of
// its own.
func platformEntry(p *v1alpha1.PlatformLLMProvider) harnessEntry {
	e := harnessEntry{harness: p.Harness, authMode: p.Auth.Mode}
	for _, v := range p.Env {
		e.env = append(e.env, v.Name)
	}
	return e
}

// remoteSignIn is how the login of a provider that runs an executor reaches a
// box off the engine's pod, given the names of the seat's own environment
// (Crewlet's entryEnv, and sandboxCredentials through its entrySignsIn):
// own when the provider's settings carry a sign-in that follows the run, and
// the variables of the seat's environment that sign its harness in, both read
// off its profile (executorSignIn). The provider's own travels as its token
// under Subscription, its key under APIKey, and a variable of its env that
// signs the harness in (its sign_in_env), which the run carries whatever the
// mode and is all Env hands it. A harness that signs in through its
// environment (OpenCode, Hermes, Pi) is signed in by any variable named as a credential,
// in the provider's env or the seat's, the seat's winning a name.
func (c *harnessEntry) remoteSignIn(env envNames) (own bool, seat []string) {
	p := executorSignIn[c.harness]
	switch c.authMode {
	case v1alpha1.HarnessAuthAPIKey:
		own = p.apiKeyEnv != ""
	case v1alpha1.HarnessAuthEnv:
		// Neither the token nor the key reaches the harness: env alone
		// signs it in, below.
	case v1alpha1.HarnessAuthSubscription, "":
		own = c.token && p.tokenEnv != ""
	}
	names := p.names()
	signs := func(name string) bool {
		return slices.Contains(names, name) || p.envSignIn && credentialName.MatchString(name)
	}
	own = own || slices.ContainsFunc(c.env, signs)
	seat = env.among(names)
	for _, name := range slices.Sorted(maps.Keys(env)) {
		if p.envSignIn && signs(name) && !slices.Contains(seat, name) {
			seat = append(seat, name)
		}
	}
	return own, seat
}

// envNames is the set of variable names of a seat's executor runs.
type envNames map[string]bool

// among is the names of names the seat's environment has, in their order.
func (e envNames) among(names []string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(name string) bool { return !e[name] })
}

// runEnvNames is the names a seat's executor runs are given values under,
// and the names its blocks write that every run goes without: its own
// spec.sandbox.env and setup steps', and the Company's setup steps'. Crewlet
// lays them as its runEnvironment does, the Company's steps, then the seat's,
// then its env, each later one winning a name, and leaves a name out of every
// run where the value that wins resolves to nothing, as the package doc above
// says which values the operator knows to. What the engine adds itself is left
// out: the agent's identity names no harness's sign-in variable, and the engine
// judges every sign-in without an integration's box settings or the variables
// naming its box files. So is the seat's own credential for a tool in its box,
// as the package doc above says.
func (r *renderer) runEnvNames(s *v1alpha1.Seat) (names, leftOut envNames) {
	type value struct {
		key secretref.Key
		// provisioned is the vendor of a credential the engine mints,
		// unset for a Secret's value.
		provisioned v1alpha1.ProvisionedVendor
		optional    bool
	}
	won := map[string]value{}
	tools := r.seatCredentialKeys(s)
	sb := &s.Spec.Sandbox
	for _, steps := range [][]v1alpha1.SandboxSetupStep{r.in.Company.Spec.Sandbox.Setup, sb.Setup} {
		for i := range steps {
			for _, e := range steps[i].Env {
				won[e.Name] = value{
					key: r.key(e.ValueFrom.SecretKeyRef), optional: slices.Contains(steps[i].OptionalKeys, e.Name),
				}
			}
		}
	}
	for _, e := range sb.Env {
		v := value{provisioned: e.ValueFrom.Provisioned.Vendor, optional: slices.Contains(sb.OptionalKeys, e.Name)}
		if e.ValueFrom.SecretKeyRef.Name != "" {
			v.key = r.key(e.ValueFrom.SecretKeyRef)
		}
		won[e.Name] = v
	}
	names, leftOut = envNames{}, envNames{}
	for name, v := range won {
		if v.provisioned != "" || tools[v.key] {
			continue
		}
		if r.empty[v.key] || v.optional && r.unset[v.key] {
			leftOut[name] = true
			continue
		}
		names[name] = true
	}
	return names, leftOut
}

// leftOutNote is the end of a message saying that names, variables of the
// seat's environment that would sign its executor's harness in, are left out of
// every run, or nothing when there are none.
func leftOutNote(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf(". The Company's or the seat's spec.sandbox gives %s, which every run goes without, as its "+
		"value resolves to nothing", strings.Join(names, ", "))
}

// key is ref's Secret key, in the Company's namespace.
func (r *renderer) key(ref v1alpha1.SecretKeyReference) secretref.Key {
	return secretref.Key{Namespace: r.namespace(), Name: ref.Name, Key: ref.Key}
}
