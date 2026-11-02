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
// signs its CLI in, a token or a key, the provider's env under auth mode Env
// (a gateway's token or a cloud switch for ClaudeCode), or an OpenCode
// provider's key in its env. Crewlet refuses each launch
// of a run whose box could not authenticate, judged on the member the launch
// picked (sandboxCredentials), and never refuses a Direct one for it
// (Placement.OnEngineHost).
//
// The seat's own environment is the union of its spec.sandbox.env, its
// spec.sandbox.setup steps' env and the Company's spec.sandbox.setup steps'
// env, which Crewlet merges into every run (runEnvironment). The operator
// sees the names a Secret's values are given under, not the values: a name
// counts here where Crewlet would count a non-blank value under it. A run
// starts without every value that resolves to nothing, whether or not an
// optionalKeys list names its key, so a name counts for nothing where its
// value is known to hold nothing once the reconcile has read the Secrets: an
// empty value (Input.Empty), one missing under a key its block declares
// optional (Input.Unset), and a provisioned one nothing mints. A value
// missing under a key that is not optional holds the document, whose running
// revision keeps the value it had, so it counts.
//
// Under auth mode Env the provider's env is its whole sign-in: Crewlet hands
// the CLI neither its token variable nor its key variable and seeds no login,
// in the engine's pod and in a box of either cell ((*Provider).SandboxEnv),
// so the run signs in, Direct or E2B, by what env carries, which the API
// requires to sign the CLI in. Under every other mode the API keeps
// ClaudeCode's env sign-ins out of env, so there its sign-in is its token,
// its key or its login.
//
// A seat whose environment signs ClaudeCode in itself runs a ClaudeCode run
// on that sign-in alone: Crewlet hands the run none of the provider's sign-in,
// its token, its key and every sign-in its env carries, but the rest of its
// env (entryEnv, entrySignInName), since ClaudeCode ranks its sign-ins by
// name, and its guard passes the run on any of them (sandboxCredentials,
// through its entrySignsIn). Crewlet decides that by the profile's list of the
// names that sign the CLI in (CredentialEnvNames), which only ClaudeCode's
// profile extends beyond its token and key variables.
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
}

// claudeCodeSignIn are the variables that sign ClaudeCode in from a run's
// environment: a plan's headless token and an Anthropic key (the profile's
// token_env and api_key_env), then claudeCodeEnvSignIn, in the order of
// Crewlet's CredentialEnvNames, which reads them from the claude-code profile
// and to whose profile a test holds this copy. When the seat's own
// environment sets one, Crewlet hands a ClaudeCode run none of its provider's
// sign-in (its entryEnv), since ClaudeCode ranks its sign-ins by name and a
// provider's key beside a seat's plan token would win, and its guard passes
// the run on it, as on one the provider's env carries into the run
// (sandboxCredentials, through its entrySignsIn).
var claudeCodeSignIn = slices.Concat([]string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY"}, claudeCodeEnvSignIn)

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

// cliEntry is what the verdicts on a seat read of a model provider's CLI, an
// LLMProvider's or a platform provider's, the two kinds read alike.
type cliEntry struct {
	agent v1alpha1.CLIAgentName
	// runIn is the cell an LLMProvider names for its executor runs; a
	// platform provider names none.
	runIn v1alpha1.SandboxPlacement
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
		agent: s.Agent, runIn: s.RunIn, authMode: s.Auth.Mode,
		token: s.Auth.TokenSecretRef.Name != "", bundle: s.Auth.CredentialBundleSecretRef.Name != "",
	}
	for _, v := range s.Env {
		e.env = append(e.env, v.Name)
	}
	return e
}

// platformEntry is what the verdicts read of a platform provider: a CLI
// signed in with a key, never a token or a bundle, and placed by no runIn of
// its own.
func platformEntry(p *v1alpha1.PlatformLLMProvider) cliEntry {
	e := cliEntry{agent: p.Agent, authMode: p.Auth.Mode}
	for _, v := range p.Env {
		e.env = append(e.env, v.Name)
	}
	return e
}

// remoteSignIn is how the login of a provider that runs an executor reaches a
// box off the engine's pod, given the names of the seat's own environment
// (Crewlet's entryEnv, and sandboxCredentials through its entrySignsIn):
// own when the provider's settings carry a sign-in that follows the run, and
// the variables of the seat's environment that sign its CLI in. ClaudeCode's
// own travels as what its auth mode hands the CLI, its token under
// Subscription and its key under APIKey, and as a variable of its env that
// signs ClaudeCode in, a gateway's bearer token or a cloud switch, which the
// run carries with the rest of the env whatever the mode. Under Env that is
// the only one: the mode hands the CLI neither the token nor the key, and the
// API requires such a variable in env then and refuses one under every other
// mode. The seat's travels as any variable that signs ClaudeCode in, which
// then replaces the provider's. OpenCode's, which signs in through its
// environment, under Subscription and Env alike, travels as any variable
// named as a credential, in its env or the seat's environment, the seat's
// winning a name.
func (c *cliEntry) remoteSignIn(env envNames) (own bool, seat []string) {
	if c.agent == v1alpha1.CLIAgentClaudeCode {
		switch c.authMode {
		case v1alpha1.CLIAgentAuthAPIKey:
			own = true
		case v1alpha1.CLIAgentAuthEnv:
			// Neither the token nor the key reaches the CLI: env alone
			// signs it in, below.
		case v1alpha1.CLIAgentAuthSubscription, "":
			own = c.token
		}
		own = own || slices.ContainsFunc(c.env, func(name string) bool { return slices.Contains(claudeCodeSignIn, name) })
		return own, env.among(claudeCodeSignIn)
	}
	own = slices.ContainsFunc(c.env, credentialName.MatchString)
	for _, name := range slices.Sorted(maps.Keys(env)) {
		if credentialName.MatchString(name) {
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
// says which values the operator knows to.
func (r *renderer) runEnvNames(s *v1alpha1.Seat) (names, leftOut envNames) {
	type value struct {
		key secretref.Key
		// provisioned is the vendor of a credential the engine mints,
		// unset for a Secret's value.
		provisioned v1alpha1.ProvisionedVendor
		optional    bool
	}
	won := map[string]value{}
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
		if r.empty[v.key] || v.optional && r.unset[v.key] || v.provisioned != "" && !r.provisions(v.provisioned) {
			leftOut[name] = true
			continue
		}
		names[name] = true
	}
	return names, leftOut
}

// leftOutNote is the end of a message saying that names, variables of the
// seat's environment that would sign its executor's CLI in, are left out of
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
