package render

import (
	"cmp"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// The objects of the optional-keys case the tests break, and the keys and
// Secret keys they write.
const (
	optionalSeat        = "coder"
	optionalTavily      = "tavily"
	optionalToken       = "token"
	envTavilyKey        = "TAVILY_API_KEY"
	envRegistry         = "NPM_CONFIG_REGISTRY"
	headerAuthorization = "Authorization"
	envGitHubSignIn     = "GITHUB_TOKEN"
	envDatabaseURL      = "DATABASE_URL"
	envPGUser           = "PGUSER"
	filePGPass          = ".pgpass"
	provisionedGitLab   = "gitlab-token"
	envClaudeSignIn     = "CLAUDE_CODE_OAUTH_TOKEN"
	envGitLabToken      = "GITLAB_TOKEN"
	fileNPMRC           = ".npmrc"
	// headerLower and headerUpper are the Authorization header as a Seat or
	// a Unit may spell it, unlike the server and unlike each other.
	headerLower = "authorization"
	headerUpper = "AUTHORIZATION"
	// unitEng is the Unit the optional-keys case's seat is a member of.
	unitEng = "eng"
	// coderTavily is the Secret the seat gives the Tavily server its
	// own key from.
	coderTavily = "coder-tavily"
	// fieldSecondOptionalKey is the second entry of an MCPServer's
	// optionalKeys, and fieldSecondServerValue the first value a Seat gives
	// its second tool server.
	fieldSecondOptionalKey = "spec.optionalKeys[1]"
	fieldSecondServerValue = "spec.mcp[1].env[0].valueFrom"
)

// optionalSecret is the Secret key name/key in the test namespace.
func optionalSecret(name, key string) secretref.Key {
	return secretref.Key{Namespace: testNamespace, Name: name, Key: key}
}

// TestStartValuesSayWhichValuesMayGoMissing holds what the render records of
// every Secret key a tool server or a coding run is started with: where it is
// written, under which key, for which server, which optionalKeys list governs
// it and whether that list lets the key go without it, and whether the engine
// starts anything with it. A key every reference to which may go without it is
// Optional, and only such a key: the secret sync takes a missing Secret or key
// for nothing there, and for a failure anywhere else. The Unit's header is
// started with nothing: the one seat it would reach replaces it with its own,
// as Crewlet lays a seat's block over its unit's.
func TestStartValuesSayWhichValuesMayGoMissing(t *testing.T) {
	in := companyCaseNamed(t, caseOptionalKeys).input(t)
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	company := ObjectRef{Kind: KindCompany, Name: in.Company.Name}
	coder := ObjectRef{Kind: KindSeat, Name: optionalSeat}
	tavily := ObjectRef{Kind: KindMCPServer, Name: optionalTavily}
	github := ObjectRef{Kind: KindMCPServer, Name: sampleGitHub}
	const (
		firstStep   = "spec.sandbox.setup[0].optionalKeys"
		seatSandbox = "spec.sandbox.optionalKeys"
	)
	value := func(k secretref.Key, o ObjectRef, field, name, server string, optional bool, list ObjectRef, listField string) StartValue {
		return StartValue{
			Key: k, Object: o, Field: field, Name: name, Server: server, Optional: optional,
			List: list, ListField: listField, Started: true,
		}
	}
	unitHeader := value(optionalSecret("eng-github", headerLower), ObjectRef{Kind: KindUnit, Name: unitEng},
		"spec.mcp[0].env[0].valueFrom", headerAuthorization, sampleGitHub, true, github, fieldOptionalKeys)
	unitHeader.Started = false
	want := []StartValue{
		value(optionalSecret("npm", optionalToken), company, "spec.sandbox.setup[0].env[0].valueFrom", "NPM_TOKEN", "", false,
			company, firstStep),
		value(optionalSecret("npm", "npmrc"), company, "spec.sandbox.setup[0].files[0].valueFrom", fileNPMRC, "", true,
			company, firstStep),
		value(optionalSecret("tavily", keyAPIKey), tavily, "spec.env[0].valueFrom", envTavilyKey, optionalTavily, false,
			tavily, fieldOptionalKeys),
		value(optionalSecret("npm", "registry"), tavily, "spec.env[1].valueFrom", envRegistry, optionalTavily, true,
			tavily, fieldOptionalKeys),
		unitHeader,
		value(optionalSecret("coder-github", headerLower), coder, "spec.mcp[0].env[0].valueFrom", headerAuthorization,
			sampleGitHub, true, github, fieldOptionalKeys),
		value(optionalSecret("coder-github", optionalToken), coder, "spec.sandbox.env[0].valueFrom", envGitHubSignIn, "", false,
			coder, seatSandbox),
		value(optionalSecret("coder-db", "url"), coder, "spec.sandbox.env[1].valueFrom", envDatabaseURL, "", true, coder, seatSandbox),
		value(optionalSecret("coder-db", "user"), coder, "spec.sandbox.setup[0].env[0].valueFrom", envPGUser, "", false,
			coder, firstStep),
		value(optionalSecret("coder-db", "pgpass"), coder, "spec.sandbox.setup[0].files[0].valueFrom", filePGPass, "", true,
			coder, firstStep),
	}
	order := func(a, b StartValue) int {
		return cmp.Or(strings.Compare(a.Object.String(), b.Object.String()), strings.Compare(a.Field, b.Field))
	}
	got := slices.SortedFunc(slices.Values(doc.StartValues), order)
	slices.SortFunc(want, order)
	if !slices.Equal(got, want) {
		t.Errorf("the start values are\n%v\nwant\n%v", got, want)
	}

	var optional []secretref.Key
	for _, v := range want {
		if v.Optional {
			optional = append(optional, v.Key)
		}
	}
	slices.SortFunc(optional, secretref.Compare)
	if !slices.Equal(doc.Optional, optional) {
		t.Errorf("Optional is %v, want %v", doc.Optional, optional)
	}
	for _, k := range optional {
		if !slices.Contains(doc.Refs, k) {
			t.Errorf("Refs lacks the optional key %v: the Secret it is in is never watched", k)
		}
	}

	// A key one reference requires is required, however many others let
	// it go missing: the store entry is one, and every reference reads it.
	shared := seat(in, optionalSeat)
	shared.Spec.Sandbox.Env[0].ValueFrom.SecretKeyRef = v1alpha1.SecretKeyReference{Name: "coder-db", Key: "url"}
	doc, err = Company(in)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(doc.Optional, optionalSecret("coder-db", "url")) {
		t.Errorf("coder-db/url is Optional while spec.sandbox.env[0], which is not optional, reads it")
	}
}

// TestStartedValuesAreTheOnesTheEngineStartsWith holds Started to what the
// engine starts each tool server and coding run with: a value nothing starts
// with, because no instance of its server runs or another value replaces it
// wherever it would be read, is not started, whatever the Secret holds.
func TestStartedValuesAreTheOnesTheEngineStartsWith(t *testing.T) {
	const own, registry = "spec.env[0].valueFrom", "spec.env[1].valueFrom"
	cases := []struct {
		name   string
		mutate func(in *Input)
		object ObjectRef
		field  string
		want   bool
	}{
		{
			name:   "a shared server's own value",
			object: ObjectRef{Kind: KindMCPServer, Name: optionalTavily}, field: own, want: true,
		},
		{
			name:   "a per-seat server's own value no seat is granted it for",
			mutate: func(in *Input) { toolServer(in, optionalTavily).Spec.Shared = new(false) },
			object: ObjectRef{Kind: KindMCPServer, Name: optionalTavily}, field: own,
		},
		{
			name: "a per-seat server's own value a seat's block leaves alone",
			mutate: func(in *Input) {
				toolServer(in, optionalTavily).Spec.Shared = new(false)
				seat(in, optionalSeat).Spec.MCP = append(seat(in, optionalSeat).Spec.MCP, v1alpha1.MCPCredentials{
					Name: optionalTavily, Env: []v1alpha1.MCPCredentialValue{{Name: "TAVILY_PROJECT", Value: new("docs")}},
				})
			},
			object: ObjectRef{Kind: KindMCPServer, Name: optionalTavily}, field: own, want: true,
		},
		{
			name: "a per-seat server's own value every granted seat replaces",
			mutate: func(in *Input) {
				toolServer(in, optionalTavily).Spec.Shared = new(false)
				seat(in, optionalSeat).Spec.MCP = append(seat(in, optionalSeat).Spec.MCP, v1alpha1.MCPCredentials{
					Name: optionalTavily, Env: []v1alpha1.MCPCredentialValue{{
						Name: envTavilyKey, ValueFrom: v1alpha1.SeatCredentialSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: coderTavily, Key: keyAPIKey}},
					}},
				})
			},
			object: ObjectRef{Kind: KindMCPServer, Name: optionalTavily}, field: own,
		},
		{
			name: "a per-seat server's own value a coding run's box is given",
			mutate: func(in *Input) {
				toolServer(in, optionalTavily).Spec.Shared = new(false)
				seat(in, optionalSeat).Spec.Sandbox.MCPServerRefs = []v1alpha1.LocalObjectReference{{Name: optionalTavily}}
			},
			object: ObjectRef{Kind: KindMCPServer, Name: optionalTavily}, field: registry, want: true,
		},
		{
			name: "a seat's value for a shared server no box of its is given",
			mutate: func(in *Input) {
				seat(in, optionalSeat).Spec.MCP = append(seat(in, optionalSeat).Spec.MCP, v1alpha1.MCPCredentials{
					Name: optionalTavily, Env: []v1alpha1.MCPCredentialValue{{
						Name: envTavilyKey, ValueFrom: v1alpha1.SeatCredentialSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: coderTavily, Key: keyAPIKey}},
					}},
				})
			},
			object: ObjectRef{Kind: KindSeat, Name: optionalSeat}, field: fieldSecondServerValue,
		},
		{
			name: "a seat's value for a shared server its box is given",
			mutate: func(in *Input) {
				seat(in, optionalSeat).Spec.MCP = append(seat(in, optionalSeat).Spec.MCP, v1alpha1.MCPCredentials{
					Name: optionalTavily, Env: []v1alpha1.MCPCredentialValue{{
						Name: envTavilyKey, ValueFrom: v1alpha1.SeatCredentialSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: coderTavily, Key: keyAPIKey}},
					}},
				})
				seat(in, optionalSeat).Spec.Sandbox.MCPServerRefs = []v1alpha1.LocalObjectReference{{Name: optionalTavily}}
			},
			object: ObjectRef{Kind: KindSeat, Name: optionalSeat}, field: fieldSecondServerValue, want: true,
		},
		{
			name:   "a unit's header a member writes in another case",
			mutate: func(in *Input) { seat(in, optionalSeat).Spec.MCP[0].Env[0].Name = headerUpper },
			object: ObjectRef{Kind: KindUnit, Name: unitEng}, field: "spec.mcp[0].env[0].valueFrom",
		},
		{
			name:   "a unit's header no member replaces",
			mutate: func(in *Input) { seat(in, optionalSeat).Spec.MCP = nil },
			object: ObjectRef{Kind: KindUnit, Name: unitEng}, field: "spec.mcp[0].env[0].valueFrom", want: true,
		},
		{
			name: "a Company step's variable a seat's env replaces",
			mutate: func(in *Input) {
				seat(in, optionalSeat).Spec.Sandbox.Env[0].Name = "NPM_TOKEN"
			},
			object: ObjectRef{Kind: KindCompany, Name: optionalInput(t).Company.Name}, field: "spec.sandbox.setup[0].env[0].valueFrom",
		},
		{
			name: "a Company step's file, which no seat replaces",
			mutate: func(in *Input) {
				seat(in, optionalSeat).Spec.Sandbox.Env[0].Name = "NPM_TOKEN"
			},
			object: ObjectRef{Kind: KindCompany, Name: optionalInput(t).Company.Name}, field: "spec.sandbox.setup[0].files[0].valueFrom", want: true,
		},
		{
			name:   "a Company step's variable with no seat to run it",
			mutate: func(in *Input) { seat(in, optionalSeat).Spec.Sandbox = nil },
			object: ObjectRef{Kind: KindCompany, Name: optionalInput(t).Company.Name}, field: "spec.sandbox.setup[0].env[0].valueFrom",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input := optionalInput(t)
			if c.mutate != nil {
				c.mutate(input)
			}
			starts := StartValuesOf(input)
			i := slices.IndexFunc(starts, func(v StartValue) bool { return v.Object == c.object && v.Field == c.field })
			if i < 0 {
				t.Fatalf("no start value of %s at %s among %v", c.object, c.field, starts)
			}
			if starts[i].Started != c.want {
				t.Errorf("%s at %s: Started = %t, want %t", c.object, c.field, starts[i].Started, c.want)
			}
		})
	}
}

// optionalInput is the optional-keys case's input.
func optionalInput(t *testing.T) *Input {
	t.Helper()
	return companyCaseNamed(t, caseOptionalKeys).input(t)
}

// TestOptionalKeysAreRenderedSorted holds the three blocks' optionalKeys to
// Crewlet's optional_keys beside the values they govern, in an order that does
// not depend on how they were written.
func TestOptionalKeysAreRenderedSorted(t *testing.T) {
	in := companyCaseNamed(t, caseOptionalKeys).input(t)
	toolServer(in, optionalTavily).Spec.OptionalKeys = []string{envRegistry, envTavilyKey}
	slices.Reverse(toolServer(in, optionalTavily).Spec.OptionalKeys)
	coder := seat(in, optionalSeat)
	coder.Spec.Sandbox.OptionalKeys = []string{envGitHubSignIn, envDatabaseURL}
	coder.Spec.Sandbox.Setup[0].OptionalKeys = []string{envPGUser, filePGPass}
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(doc.YAML, &tree); err != nil {
		t.Fatal(err)
	}
	var tavily map[string]any
	for _, server := range tree[keyMCPServers].([]any) {
		if entry := server.(map[string]any); entry[keyName] == optionalTavily {
			tavily = entry
		}
	}
	role := roleIn(tree, optionalSeat)
	sandbox := role["sandbox"].(map[string]any)
	step := sandbox["setup"].([]any)[0].(map[string]any)
	companyStep := tree[keyProviders].(map[string]any)["sandbox"].(map[string]any)["setup"].([]any)[0].(map[string]any)
	for _, c := range []struct {
		where string
		got   any
		want  []any
	}{
		{"the tavily server", tavily["optional_keys"], []any{envRegistry, envTavilyKey}},
		{"the coder's sandbox", sandbox["optional_keys"], []any{envDatabaseURL, envGitHubSignIn}},
		{"the coder's setup step", step["optional_keys"], []any{filePGPass, envPGUser}},
		{"the Company's setup step", companyStep["optional_keys"], []any{fileNPMRC}},
	} {
		got, _ := c.got.([]any)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: optional_keys is %v, want %v", c.where, c.got, c.want)
		}
	}
}

// TestAnOptionalKeyNeedsAValueThatCanGoMissing holds the render's refusal of
// an MCPServer's optionalKeys entry that leaves nothing out, which Crewlet
// refuses on every write: a key no value read from a Secret, or provisioned,
// is written under, by the server or by any Seat's or Unit's spec.mcp for it.
// A key a Seat or a Unit alone writes is a key of the server, an HTTP
// server's in any case, and a Stdio server's as written; a key the server
// writes as a literal is taken where a Seat writes it from a Secret, whose
// value replaces the server's whole and may go missing. A refusal names who
// writes the literals, and the spelling a Stdio key is written in.
func TestAnOptionalKeyNeedsAValueThatCanGoMissing(t *testing.T) {
	const (
		unwritten = "which no value is written under"
		literal   = "under which every value is a literal (Seat coder)"
	)
	cases := []struct {
		name   string
		mutate func(*Input)
		// field and says are the refusal, empty when the render takes it.
		field, says string
	}{
		{name: "a header a Unit and a Seat write in another case is taken", mutate: func(*Input) {}},
		{
			name: "a key nothing writes is refused",
			mutate: func(in *Input) {
				s := toolServer(in, optionalTavily)
				s.Spec.OptionalKeys = append(s.Spec.OptionalKeys, "NPM_CONFIG_USERCONFIG")
			},
			field: fieldSecondOptionalKey, says: unwritten,
		},
		{
			name: "a Stdio server's key in another case is refused",
			mutate: func(in *Input) {
				toolServer(in, optionalTavily).Spec.OptionalKeys = []string{"npm_config_registry"}
			},
			field: "spec.optionalKeys[0]", says: unwritten + ": not this server's env, nor any Seat's or Unit's " +
				"spec.mcp entry for this server. Nothing is left out for it, and the key it was meant for stays one " +
				"the server is not started without, so Crewlet refuses the document. Name the key a value is read " +
				"from a Secret under, or remove the entry (a Stdio server's keys are variables, which an " +
				"environment tells apart by case: the one written is NPM_CONFIG_REGISTRY)",
		},
		{
			name: "a key the server writes as a literal alone is refused",
			mutate: func(in *Input) {
				s := toolServer(in, sampleGitHub)
				s.Spec.OptionalKeys = append(s.Spec.OptionalKeys, "x-mcp-toolsets")
			},
			field: fieldSecondOptionalKey, says: "under which every value is a literal (this server's headers)",
		},
		{
			name: "a key the server writes as a literal and a Seat from a Secret is taken",
			mutate: func(in *Input) {
				github := &seat(in, optionalSeat).Spec.MCP[0]
				github.Env = append(github.Env, v1alpha1.MCPCredentialValue{
					Name:      "X-MCP-Toolsets",
					ValueFrom: v1alpha1.SeatCredentialSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "coder-github", Key: "toolsets"}},
				})
				s := toolServer(in, sampleGitHub)
				s.Spec.OptionalKeys = append(s.Spec.OptionalKeys, "x-mcp-toolsets")
			},
		},
		{
			name: "a key a Seat writes from a Secret for another server is refused",
			mutate: func(in *Input) {
				toolServer(in, optionalTavily).Spec.OptionalKeys = []string{headerAuthorization}
			},
			field: "spec.optionalKeys[0]", says: unwritten,
		},
		{
			name: "a key a Seat writes only as a literal is refused",
			mutate: func(in *Input) {
				github := &seat(in, optionalSeat).Spec.MCP[0]
				github.Env = append(github.Env, v1alpha1.MCPCredentialValue{Name: "X-Debug", Value: new("1")})
				s := toolServer(in, sampleGitHub)
				s.Spec.OptionalKeys = append(s.Spec.OptionalKeys, "x-debug")
			},
			field: fieldSecondOptionalKey, says: literal,
		},
		{
			name: "a key a Seat writes as a literal and a Unit from a Secret is taken",
			mutate: func(in *Input) {
				const readonly = "X-MCP-Readonly"
				github := &seat(in, optionalSeat).Spec.MCP[0]
				github.Env = append(github.Env, v1alpha1.MCPCredentialValue{Name: readonly, Value: new("true")})
				eng := &unit(in, unitEng).Spec.MCP[0]
				eng.Env = append(eng.Env, v1alpha1.MCPCredentialValue{
					Name:      readonly,
					ValueFrom: v1alpha1.SeatCredentialSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "eng-github", Key: "readonly"}},
				})
				s := toolServer(in, sampleGitHub)
				s.Spec.OptionalKeys = append(s.Spec.OptionalKeys, strings.ToLower(readonly))
			},
		},
		{
			name: "a key a Seat's provisioned value is written under is taken",
			mutate: func(in *Input) {
				github := &seat(in, optionalSeat).Spec.MCP[0]
				github.Env = append(github.Env, v1alpha1.MCPCredentialValue{
					Name:      "X-Gitlab-Token",
					ValueFrom: v1alpha1.SeatCredentialSource{Provisioned: v1alpha1.ProvisionedCredential{Name: provisionedGitLab}},
				})
				s := toolServer(in, sampleGitHub)
				s.Spec.OptionalKeys = append(s.Spec.OptionalKeys, "X-Gitlab-Token")
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, caseOptionalKeys).input(t)
			c.mutate(in)
			_, err := Company(in)
			refusals := RefErrors(err)
			if c.field == "" {
				if err != nil {
					t.Fatalf("the render refuses it: %v", err)
				}
				return
			}
			if len(refusals) != 1 {
				t.Fatalf("the render refuses %d findings, want one: %v", len(refusals), err)
			}
			r := refusals[0]
			if r.Kind != KindMCPServer || r.Field != c.field || r.Reason != v1alpha1.ReasonOptionalKeyUnused ||
				r.Basis != EngineRefuses || !strings.Contains(r.Message, c.says) {
				t.Errorf("the render refuses %s %s %s %v: %s, want MCPServer %s OptionalKeyUnused EngineRefuses, saying %q",
					r.Kind, r.Field, r.Reason, r.Basis, r.Message, c.field, c.says)
			}
		})
	}
}

// TestNoProvisionerSaysWhatGoesWithoutIt holds the report on a provisioned
// credential nothing creates to what Crewlet does without its value, by where
// it is written: a tool server is not started, nor a run_sandbox coding run,
// unless the key is optional, when they start without it.
func TestNoProvisionerSaysWhatGoesWithoutIt(t *testing.T) {
	provisioned := v1alpha1.SeatCredentialSource{Provisioned: v1alpha1.ProvisionedCredential{Name: provisionedGitLab}}
	in := companyCaseNamed(t, caseOptionalKeys).input(t)
	coder := seat(in, optionalSeat)
	coder.Spec.MCP[0].Env = append(coder.Spec.MCP[0].Env,
		v1alpha1.MCPCredentialValue{Name: "X-Required", ValueFrom: provisioned},
		v1alpha1.MCPCredentialValue{Name: "X-Optional", ValueFrom: provisioned})
	github := toolServer(in, sampleGitHub)
	github.Spec.OptionalKeys = append(github.Spec.OptionalKeys, "X-Optional")
	coder.Spec.Sandbox.Env = append(coder.Spec.Sandbox.Env,
		v1alpha1.SeatEnvVar{Name: envGitLabToken, ValueFrom: provisioned},
		v1alpha1.SeatEnvVar{Name: "GITLAB_BACKUP_TOKEN", ValueFrom: provisioned})
	coder.Spec.Sandbox.OptionalKeys = append(coder.Spec.Sandbox.OptionalKeys, "GITLAB_BACKUP_TOKEN")
	reported := map[string]string{}
	for _, r := range ReportedOf(in) {
		if r.Reason == v1alpha1.ReasonNoProvisioner {
			reported[r.Field] = r.Message
		}
	}
	for field, says := range map[string]string{
		"spec.mcp[0].env[1].valueFrom":  "the engine does not start the seat's instance of MCPServer github",
		"spec.mcp[0].env[2].valueFrom":  "starts the seat's instance of MCPServer github without it",
		"spec.sandbox.env[2].valueFrom": "Crewlet refuses each of the seat's run_sandbox coding runs",
		"spec.sandbox.env[3].valueFrom": "the seat's coding runs start without it",
	} {
		if got, ok := reported[field]; !ok || !strings.Contains(got, says) {
			t.Errorf("%s is reported %q, want it to say %q", field, got, says)
		}
	}
}

// TestCodeWorkGoesWithoutAMissingOptionalSignIn holds the code-work verdict
// to the run's environment as Crewlet lays it, on a ClaudeCode provider whose
// credential bundle cannot follow the run: a variable that would sign
// ClaudeCode in counts for nothing where the value that wins is optional and
// missing or empty, since the run goes without it; a required one that is
// missing counts, since the document is held and the running revision keeps
// the value it had; and a required one that is empty, which the engine reads
// as nothing, has every run refused, whatever signs in.
func TestCodeWorkGoesWithoutAMissingOptionalSignIn(t *testing.T) {
	head := codeWorkBundled(v1alpha1.CLIAgentClaudeCode)
	signIn := v1alpha1.SecretKeyReference{Name: "fixer-claude", Key: optionalToken}
	stepSignIn := v1alpha1.SecretKeyReference{Name: "fixer-claude", Key: "step-token"}
	cases := []struct {
		name string
		// optional lists the variable in spec.sandbox.optionalKeys, step
		// writes it in a setup step of the seat's too, and stepOptional
		// lists it there.
		optional, step, stepOptional bool
		unset, empty                 []secretref.Key
		reason                       string
		// says is what the message must say.
		says string
	}{
		{name: "an optional sign-in whose value is there", optional: true},
		{
			name: "an optional sign-in whose value is missing", optional: true, unset: []secretref.Key{optionalSecretOf(signIn)},
			reason: v1alpha1.ReasonSandboxLoginNotPortable, says: "leaves it out of every run",
		},
		{
			name: "an optional sign-in whose value is empty", optional: true, empty: []secretref.Key{optionalSecretOf(signIn)},
			reason: v1alpha1.ReasonSandboxLoginNotPortable, says: "leaves it out of every run",
		},
		{name: "a required sign-in whose value is missing", unset: []secretref.Key{optionalSecretOf(signIn)}},
		{
			name: "a required sign-in whose value is empty", empty: []secretref.Key{optionalSecretOf(signIn)},
			reason: v1alpha1.ReasonSecretValueEmpty,
			says: "Crewlet refuses each run_sandbox launch of the seat before a box exists, as its sandbox gives it " +
				envClaudeSignIn + " at spec.sandbox.env[0].valueFrom and Secret " + testNamespace + "/fixer-claude holds an empty value " +
				`under "token"`,
		},
		{
			name: "a step's optional sign-in the seat's own env replaces", step: true, stepOptional: true,
			unset: []secretref.Key{optionalSecretOf(stepSignIn)},
		},
		{
			name: "a step's required empty sign-in the seat's own env replaces", step: true,
			empty: []secretref.Key{optionalSecretOf(stepSignIn)},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := codeWorkInput(t, &head, v1alpha1.CodingAgentClaudeCode)
			in.Unset, in.Empty = c.unset, c.empty
			fixer := seat(in, codeWorkSeat)
			fixer.Spec.Sandbox.Env = append(fixer.Spec.Sandbox.Env, v1alpha1.SeatEnvVar{
				Name: envClaudeSignIn, ValueFrom: v1alpha1.SeatCredentialSource{SecretKeyRef: signIn},
			})
			if c.optional {
				fixer.Spec.Sandbox.OptionalKeys = append(fixer.Spec.Sandbox.OptionalKeys, envClaudeSignIn)
			}
			if c.step {
				step := v1alpha1.SandboxSetupStep{Name: "sign-in", Env: []v1alpha1.SandboxEnvVar{{
					Name: envClaudeSignIn, ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: stepSignIn},
				}}}
				if c.stepOptional {
					step.OptionalKeys = []string{envClaudeSignIn}
				}
				fixer.Spec.Sandbox.Setup = append(fixer.Spec.Sandbox.Setup, step)
			}
			w, ok := CodeWorkOf(in)[codeWorkSeat]
			if !ok {
				t.Fatal("the seat has no code work")
			}
			if w.Reason != c.reason {
				t.Fatalf("the code work's reason is %q (%s), want %q", w.Reason, w.Message, c.reason)
			}
			if !strings.Contains(w.Message, c.says) {
				t.Errorf("the message is %q, want it to say %q", w.Message, c.says)
			}
		})
	}
}

// TestCodeWorkIsRefusedForAValueThatHoldsNothing holds the code-work verdict
// to every value a run is given that keeps it from starting where it is not
// optional, whatever signs the coding agent in: a provisioned variable no
// Integration mints, and an empty file of a step, the Company's or the
// seat's; and to none where the step lets the file go.
func TestCodeWorkIsRefusedForAValueThatHoldsNothing(t *testing.T) {
	head := codeWorkProvider(v1alpha1.CLIAgentClaudeCode)
	company := codeWorkInput(t, &head, v1alpha1.CodingAgentClaudeCode).Company.Name
	npmrc := v1alpha1.SecretKeyReference{Name: "npm", Key: "npmrc"}
	step := v1alpha1.SandboxSetupStep{Name: "npm", Files: []v1alpha1.SandboxFile{{
		Path: fileNPMRC, ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: npmrc},
	}}}
	cases := []struct {
		name   string
		mutate func(in *Input)
		reason string
		says   string
	}{
		{name: "nothing holds nothing"},
		{
			name: "a provisioned variable nothing mints",
			mutate: func(in *Input) {
				in.Integrations = nil
				seat(in, codeWorkSeat).Spec.Sandbox.Env = []v1alpha1.SeatEnvVar{{
					Name: envGitLabToken, ValueFrom: v1alpha1.SeatCredentialSource{Provisioned: v1alpha1.ProvisionedCredential{Name: provisionedGitLab}},
				}}
			},
			reason: v1alpha1.ReasonNoProvisioner,
			says:   "it is the provisioned credential gitlab-token, and no Integration of Company",
		},
		{
			name: "a provisioned variable nothing mints that the seat lets go",
			mutate: func(in *Input) {
				in.Integrations = nil
				sb := seat(in, codeWorkSeat).Spec.Sandbox
				sb.Env = []v1alpha1.SeatEnvVar{{
					Name: envGitLabToken, ValueFrom: v1alpha1.SeatCredentialSource{Provisioned: v1alpha1.ProvisionedCredential{Name: provisionedGitLab}},
				}}
				sb.OptionalKeys = []string{envGitLabToken}
			},
		},
		{
			name: "an empty file of the Company's step",
			mutate: func(in *Input) {
				in.Company.Spec.Sandbox.Setup = []v1alpha1.SandboxSetupStep{step}
				in.Empty = []secretref.Key{optionalSecretOf(npmrc)}
			},
			reason: v1alpha1.ReasonSecretValueEmpty,
			says:   "gives it .npmrc at Company " + company + "'s spec.sandbox.setup[0].files[0].valueFrom and Secret",
		},
		{
			name: "an empty file of the seat's step",
			mutate: func(in *Input) {
				seat(in, codeWorkSeat).Spec.Sandbox.Setup = []v1alpha1.SandboxSetupStep{step}
				in.Empty = []secretref.Key{optionalSecretOf(npmrc)}
			},
			reason: v1alpha1.ReasonSecretValueEmpty,
			says:   "or list .npmrc in spec.sandbox.setup[0].optionalKeys",
		},
		{
			name: "an empty file the step lets go",
			mutate: func(in *Input) {
				optional := step
				optional.OptionalKeys = []string{fileNPMRC}
				seat(in, codeWorkSeat).Spec.Sandbox.Setup = []v1alpha1.SandboxSetupStep{optional}
				in.Empty = []secretref.Key{optionalSecretOf(npmrc)}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := codeWorkInput(t, &head, v1alpha1.CodingAgentClaudeCode)
			if c.mutate != nil {
				c.mutate(in)
			}
			w, ok := CodeWorkOf(in)[codeWorkSeat]
			if !ok {
				t.Fatal("the seat has no code work")
			}
			if w.Reason != c.reason || !strings.Contains(w.Message, c.says) {
				t.Errorf("the code work is %q: %s, want %q saying %q", w.Reason, w.Message, c.reason, c.says)
			}
			if c.reason != "" && w.Field != "spec.sandbox" {
				t.Errorf("the code work is reported at %s, want spec.sandbox", w.Field)
			}
		})
	}
}

// optionalSecretOf is ref's key in the test namespace.
func optionalSecretOf(ref v1alpha1.SecretKeyReference) secretref.Key {
	return optionalSecret(ref.Name, ref.Key)
}

// TestAHeaderIsSpelledAsItsUnitSpellsIt holds the render to Crewlet's refusal
// of a seat's value for an HTTP server under a header its unit writes for the
// server in another case, on the Seat, and to nothing else: a Stdio server's
// keys are variables an environment tells apart by case, and a seat no unit's
// block reaches spells its headers as it likes.
func TestAHeaderIsSpelledAsItsUnitSpellsIt(t *testing.T) {
	stdioBlock := func(owner *[]v1alpha1.MCPCredentials, name string) {
		*owner = append(*owner, v1alpha1.MCPCredentials{Name: optionalTavily, Env: []v1alpha1.MCPCredentialValue{{
			Name: name, ValueFrom: v1alpha1.SeatCredentialSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "tavily-project", Key: "id"}},
		}}})
	}
	cases := []struct {
		name    string
		mutate  func(in *Input)
		refused string
	}{
		{name: "a header spelled alike"},
		{
			name:    "a header spelled unlike",
			mutate:  func(in *Input) { seat(in, optionalSeat).Spec.MCP[0].Env[0].Name = headerUpper },
			refused: "Seat/coder spec.mcp[0].env[0].name",
		},
		{
			name: "a Stdio server's variable in another case",
			mutate: func(in *Input) {
				stdioBlock(&unit(in, unitEng).Spec.MCP, "TAVILY_PROJECT")
				stdioBlock(&seat(in, optionalSeat).Spec.MCP, "tavily_project")
			},
		},
		{
			name: "a seat outside the unit",
			mutate: func(in *Input) {
				seat(in, optionalSeat).Spec.UnitRef.Name = ""
				seat(in, optionalSeat).Spec.MCP[0].Env[0].Name = headerUpper
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input := optionalInput(t)
			if c.mutate != nil {
				c.mutate(input)
			}
			_, err := Company(input)
			refusals := RefErrors(err)
			refused := make([]string, 0, len(refusals))
			for _, r := range refusals {
				refused = append(refused, r.Kind+"/"+r.Name+" "+r.Field)
				if r.Reason != v1alpha1.ReasonConflict || r.Basis != EngineRefuses {
					t.Errorf("%s %s is refused on %v with %s", r.Kind, r.Name, r.Basis, r.Reason)
				}
			}
			if want := []string{c.refused}; c.refused == "" && len(refused) > 0 || c.refused != "" && !slices.Equal(refused, want) {
				t.Errorf("the render refuses %q, want %q", refused, c.refused)
			}
		})
	}
}
