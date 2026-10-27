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
// A run is handed the sign-in of the provider it launches on, beneath the
// seat's own environment (Crewlet's entrySignIn): its headless token, its API
// key, or, for OpenCode, the credential-named variables of its env. Nothing
// else of the provider's env reaches a run, in either cell
// ((*Provider).SandboxEnv). A Direct box is also seeded with the provider's
// login, its credential files or the document OpenCode 2's export printed,
// and writes a refreshed one back, so on the engine pod a credential bundle
// signs a run in as it signs the provider's other phases in. An E2B box never
// receives it: a bundle's login carries a refresh token the engine keeps
// rotating, and the run there needs a token or a key in its environment.
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
// empty value (Input.Empty), one missing under a key its block declares
// optional (Input.Unset), and a provisioned one nothing mints. A value
// missing under a key that is not optional holds the document, whose running
// revision keeps the value it had, so it counts.
//
// A seat whose environment signs ClaudeCode in itself runs a ClaudeCode run
// on that sign-in alone: Crewlet hands the run none of the provider's
// (entrySignIn), since ClaudeCode ranks its sign-ins by name, and its guard
// passes the run on any of them (sandboxCredentials, through its
// entrySignsIn).
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
// guard passes the run on it (sandboxCredentials, through its entrySignsIn).
var claudeCodeSignIn = []string{
	"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
	"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
}

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
// (Crewlet's entrySignIn, and sandboxCredentials through its entrySignsIn):
// own when the provider's settings carry a sign-in that follows the run, and
// the variables of the seat's environment that sign its CLI in. ClaudeCode's
// own travels as its token or key, and the seat's as any variable that signs
// ClaudeCode in, which then replaces the provider's; OpenCode's, which signs
// in through its environment, as any variable named as a credential, in its
// env or the seat's environment, the seat's winning a name.
func (c *cliEntry) remoteSignIn(env envNames) (own bool, seat []string) {
	if c.agent == v1alpha1.CLIAgentClaudeCode {
		return c.token || c.authMode == v1alpha1.CLIAgentAuthAPIKey, env.among(claudeCodeSignIn)
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
		key         secretref.Key
		provisioned bool
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
		v := value{provisioned: e.ValueFrom.Provisioned.Name != "", optional: slices.Contains(sb.OptionalKeys, e.Name)}
		if e.ValueFrom.SecretKeyRef.Name != "" {
			v.key = r.key(e.ValueFrom.SecretKeyRef)
		}
		won[e.Name] = v
	}
	names, leftOut = envNames{}, envNames{}
	for name, v := range won {
		if r.empty[v.key] || v.optional && r.unset[v.key] || v.provisioned && !r.engineProvisions() {
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
