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

// The fields of a value the first setup step of a Company's or a Seat's
// spec.sandbox gives a seat's executor runs, its first variable and its
// first file, and of the first variable of a Seat's spec.sandbox.env.
const (
	firstStepEnv  = "spec.sandbox.setup[0].env[0].valueFrom"
	firstStepFile = "spec.sandbox.setup[0].files[0].valueFrom"
	seatEnv       = "spec.sandbox.env[0].valueFrom"
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
// every Secret key a tool server or an executor run is started with: where it is
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
		value(optionalSecret("npm", optionalToken), company, firstStepEnv, "NPM_TOKEN", "", false,
			company, firstStep),
		value(optionalSecret("npm", "npmrc"), company, firstStepFile, fileNPMRC, "", true,
			company, firstStep),
		value(optionalSecret("tavily", keyAPIKey), tavily, "spec.env[0].valueFrom", envTavilyKey, optionalTavily, false,
			tavily, fieldOptionalKeys),
		value(optionalSecret("npm", "registry"), tavily, "spec.env[1].valueFrom", envRegistry, optionalTavily, true,
			tavily, fieldOptionalKeys),
		unitHeader,
		value(optionalSecret("coder-github", headerLower), coder, "spec.mcp[0].env[0].valueFrom", headerAuthorization,
			sampleGitHub, true, github, fieldOptionalKeys),
		value(optionalSecret("coder-github", optionalToken), coder, seatEnv, envGitHubSignIn, "", false,
			coder, seatSandbox),
		value(optionalSecret("coder-db", "url"), coder, "spec.sandbox.env[1].valueFrom", envDatabaseURL, "", true, coder, seatSandbox),
		value(optionalSecret("coder-db", "user"), coder, firstStepEnv, envPGUser, "", false,
			coder, firstStep),
		value(optionalSecret("coder-db", "pgpass"), coder, firstStepFile, filePGPass, "", true,
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
// engine starts each tool server and executor run with: a value nothing
// starts with, because no instance of its server runs, no agent seat's
// executor runs, or another value replaces it wherever it would be read, is
// not started, whatever the Secret holds. Every agent seat's executor runs in
// a box, its own spec.sandbox or none, so each is given the Company's steps.
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
			name: "a per-seat server's own value an executor's box is given",
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
			object: ObjectRef{Kind: KindCompany, Name: optionalInput(t).Company.Name}, field: firstStepEnv,
		},
		{
			name: "a Company step's file, which no seat replaces",
			mutate: func(in *Input) {
				seat(in, optionalSeat).Spec.Sandbox.Env[0].Name = "NPM_TOKEN"
			},
			object: ObjectRef{Kind: KindCompany, Name: optionalInput(t).Company.Name}, field: firstStepFile, want: true,
		},
		{
			name:   "a Company step's variable a seat with no spec.sandbox of its own runs",
			mutate: func(in *Input) { seat(in, optionalSeat).Spec.Sandbox = v1alpha1.SeatSandbox{} },
			object: ObjectRef{Kind: KindCompany, Name: optionalInput(t).Company.Name}, field: firstStepEnv,
			want: true,
		},
		{
			name: "a Company step's variable with no agent seat to run it",
			mutate: func(in *Input) {
				in.Seats = slices.DeleteFunc(in.Seats, func(s v1alpha1.Seat) bool { return s.Spec.Kind != v1alpha1.SeatKindHuman })
			},
			object: ObjectRef{Kind: KindCompany, Name: optionalInput(t).Company.Name}, field: firstStepEnv,
		},
		{
			name:   "a seat's env its executor runs with",
			object: ObjectRef{Kind: KindSeat, Name: optionalSeat}, field: seatEnv, want: true,
		},
		// An agent seat whose executor never launches starts nothing with
		// its run's values, so an empty one holds nothing up.
		{
			// A valid company: no model, so no executor, and no box to
			// configure.
			name: "a seat's env in a company with no model and no spec.sandbox",
			mutate: func(in *Input) {
				in.LLMProviders = nil
				in.Company.Spec.Sandbox = v1alpha1.SandboxSettings{}
			},
			object: ObjectRef{Kind: KindSeat, Name: optionalSeat}, field: seatEnv,
		},
		{
			// Crewlet refuses the document (SandboxRequired), so it starts
			// nothing on it.
			name:   "a seat's env in a company with a model and no spec.sandbox",
			mutate: func(in *Input) { in.Company.Spec.Sandbox = v1alpha1.SandboxSettings{} },
			object: ObjectRef{Kind: KindSeat, Name: optionalSeat}, field: seatEnv,
		},
		{
			name:   "a seat's env whose executor chain resolves to nothing",
			mutate: func(in *Input) { in.LLMProviders = nil },
			object: ObjectRef{Kind: KindSeat, Name: optionalSeat}, field: seatEnv,
		},
		{
			name: "a seat's env whose executor chain holds no CLI that runs it",
			mutate: func(in *Input) {
				in.LLMProviders[0].Spec.Agent, in.LLMProviders[0].Spec.Model = v1alpha1.CLIAgentCodex, modelCodex
			},
			object: ObjectRef{Kind: KindSeat, Name: optionalSeat}, field: seatEnv,
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
// it is written: a tool server is not started unless the key is optional,
// when it starts without it, and an executor run starts without it either
// way, logged as a gap of the run unless the key is optional.
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
		"spec.sandbox.env[2].valueFrom": "the seat's executor runs start without it, and the engine logs it as a gap",
		"spec.sandbox.env[3].valueFrom": "the seat's executor runs start without it, quietly",
	} {
		if got, ok := reported[field]; !ok || !strings.Contains(got, says) {
			t.Errorf("%s is reported %q, want it to say %q", field, got, says)
		}
	}
}

// executorLoginInput is the opencode-executor case with seat fixer's executor
// on head, a new provider placed in the Company's E2B sandbox, and a
// spec.sandbox of its own.
func executorLoginInput(t *testing.T, head *v1alpha1.LLMProvider) *Input {
	t.Helper()
	in := companyCaseNamed(t, "opencode-executor").input(t)
	head.Spec.RunIn = v1alpha1.SandboxPlacementE2B
	in.LLMProviders = append(in.LLMProviders, *head)
	fixer := seat(in, executorSeat)
	fixer.Spec.LLM.Default = chainOfNames(head.Name)
	fixer.Spec.Sandbox = v1alpha1.SeatSandbox{}
	return in
}

// executorSeat is the seat of the opencode-executor case the executor
// login tests break.
const executorSeat = "fixer"

// TestExecutorGoesWithoutASignInThatHoldsNothing holds the verdict on an
// executor run in E2B, on a ClaudeCode provider whose credential bundle
// cannot follow it there, to the run's environment as Crewlet lays it: a
// variable that would sign ClaudeCode in counts for nothing where the value
// that wins resolves to nothing, optional or not, since the run starts
// without it; a required one that is missing counts, since the document is
// held and the running revision keeps the value it had.
func TestExecutorGoesWithoutASignInThatHoldsNothing(t *testing.T) {
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
		{name: "an optional sign-in whose value is there", optional: true, says: "signed in by " + envClaudeSignIn},
		{
			name: "an optional sign-in whose value is missing", optional: true, unset: []secretref.Key{optionalSecretOf(signIn)},
			reason: v1alpha1.ReasonSandboxLoginNotPortable, says: "gives " + envClaudeSignIn + ", which every run goes without",
		},
		{
			name: "an optional sign-in whose value is empty", optional: true, empty: []secretref.Key{optionalSecretOf(signIn)},
			reason: v1alpha1.ReasonSandboxLoginNotPortable, says: "which every run goes without",
		},
		{name: "a required sign-in whose value is missing", unset: []secretref.Key{optionalSecretOf(signIn)}},
		{
			// The engine reads an empty value as nothing, and starts the
			// run without it.
			name: "a required sign-in whose value is empty", empty: []secretref.Key{optionalSecretOf(signIn)},
			reason: v1alpha1.ReasonSandboxLoginNotPortable, says: "which every run goes without",
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
			in := executorLoginInput(t, new(bundled(cliProvider)))
			in.Unset, in.Empty = c.unset, c.empty
			fixer := seat(in, executorSeat)
			fixer.Spec.Sandbox.Env = append(fixer.Spec.Sandbox.Env, v1alpha1.SeatEnvVar{
				Name: envClaudeSignIn, ValueFrom: v1alpha1.SeatCredentialSource{SecretKeyRef: signIn},
			})
			if c.optional {
				fixer.Spec.Sandbox.OptionalKeys = append(fixer.Spec.Sandbox.OptionalKeys, envClaudeSignIn)
			}
			if c.step {
				step := v1alpha1.SandboxSetupStep{Name: "sign-in", Env: []v1alpha1.SecretEnvVar{{
					Name: envClaudeSignIn, ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: stepSignIn},
				}}}
				if c.stepOptional {
					step.OptionalKeys = []string{envClaudeSignIn}
				}
				fixer.Spec.Sandbox.Setup = append(fixer.Spec.Sandbox.Setup, step)
			}
			e, ok := ExecutorOf(in)[executorSeat]
			if !ok {
				t.Fatal("the seat has no executor")
			}
			if e.Reason != c.reason {
				t.Fatalf("the executor's reason is %q (%s), want %q", e.Reason, e.Message, c.reason)
			}
			if !strings.Contains(e.Message, c.says) {
				t.Errorf("the message is %q, want it to say %q", e.Message, c.says)
			}
		})
	}
}

// TestExecutorStartsWithoutAValueThatHoldsNothing holds the verdict on a
// seat's executor, and what the render records of the values its runs are
// given, to Crewlet's rule that a run starts without any value that resolves
// to nothing: a provisioned variable no Integration mints, or an empty file
// of a step, the Company's or the seat's, is no reason the seat's turns
// cannot run, whatever optionalKeys says, and each is a value the engine
// starts the run with, which the controller reports when it holds nothing
// and is not optional.
func TestExecutorStartsWithoutAValueThatHoldsNothing(t *testing.T) {
	npmrc := v1alpha1.SecretKeyReference{Name: "npm", Key: "npmrc"}
	step := v1alpha1.SandboxSetupStep{Name: "npm", Files: []v1alpha1.SandboxFile{{
		Path: fileNPMRC, ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: npmrc},
	}}}
	provisioned := v1alpha1.SeatEnvVar{
		Name: envGitLabToken, ValueFrom: v1alpha1.SeatCredentialSource{Provisioned: v1alpha1.ProvisionedCredential{Name: provisionedGitLab}},
	}
	cases := []struct {
		name   string
		mutate func(in *Input)
		// started is the field of a value the engine starts the run with,
		// and its object's kind.
		started, kind string
	}{
		{
			name: "a provisioned variable nothing mints",
			mutate: func(in *Input) {
				in.Integrations = nil
				seat(in, executorSeat).Spec.Sandbox.Env = []v1alpha1.SeatEnvVar{provisioned}
			},
		},
		{
			name: "an empty file of the Company's step",
			mutate: func(in *Input) {
				in.Company.Spec.Sandbox.Setup = []v1alpha1.SandboxSetupStep{step}
				in.Empty = []secretref.Key{optionalSecretOf(npmrc)}
			},
			started: firstStepFile, kind: KindCompany,
		},
		{
			name: "an empty file of the seat's step",
			mutate: func(in *Input) {
				seat(in, executorSeat).Spec.Sandbox.Setup = []v1alpha1.SandboxSetupStep{step}
				in.Empty = []secretref.Key{optionalSecretOf(npmrc)}
			},
			started: firstStepFile, kind: KindSeat,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := executorLoginInput(t, new(cliProvider()))
			c.mutate(in)
			e, ok := ExecutorOf(in)[executorSeat]
			if !ok || e.Reason != "" {
				t.Fatalf("the seat's executor is %q (%s), want one that runs", e.Reason, e.Message)
			}
			if c.started == "" {
				return
			}
			doc, err := Company(in)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(doc.StartValues, func(v StartValue) bool {
				return v.Started && v.Field == c.started && v.Object.Kind == c.kind
			}) {
				t.Errorf("the value at %s %s is not one the engine starts the run with: %+v", c.kind, c.started, doc.StartValues)
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
