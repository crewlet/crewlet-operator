package render

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// companyCase is one company document the tests render, pin in a golden
// file and hand to the engine's validator. Its objects are
// testdata/company/<name>.input.yaml, except the nimbus case's, which are the
// samples in config/samples.
type companyCase struct {
	name string
	// exposed gives the company a public base URL, as a class that routes
	// webhooks does.
	exposed bool
	// advisories are the advisories Crewlet's validator gives the
	// document, about choices of the tenant's that no render avoids: a
	// chart of agents alone, which has no human seat, and a code access
	// tier nothing applies.
	advisories []advisory
}

// advisoryCodeAccessGrantsNothing is Crewlet's advisory on a seat whose
// role.code.access nothing applies (config.roleCodeWarnings): the company
// hosts no code of its own, which the operator never asks of it, and either
// has no GitHub integration or the seat has no GitHub identity of its own,
// the only places a tier caps what the seat may do. The render writes the
// tier the Seat states all the same, as Crewlet runs it.
var advisoryCodeAccessGrantsNothing = advisory{path: "roles[0].code.access", says: "grants nothing"}

func companyCases() []companyCase {
	agentsAlone := []advisory{advisoryNoHumanSeat}
	return []companyCase{
		// The example company every page of the docs uses.
		{name: sampleCompany, exposed: true},
		{name: "minimal", advisories: agentsAlone},
		{name: "minimal-agent", advisories: agentsAlone},
		{name: "org-chart"},
		{name: "slack", exposed: true, advisories: agentsAlone},
		{name: "slack-phrases", exposed: true, advisories: agentsAlone},
		{name: "github-enterprise", exposed: true, advisories: agentsAlone},
		{name: "github-minimal", exposed: true, advisories: agentsAlone},
		{name: "platform-providers", advisories: agentsAlone},
		{name: "platform-providers-off", advisories: agentsAlone},
		{name: "secret-positions", exposed: true, advisories: agentsAlone},
		{name: "cli-agent", exposed: true, advisories: agentsAlone},
		{name: "opencode-agent", exposed: true, advisories: agentsAlone},
		{name: "code-access", advisories: []advisory{advisoryNoHumanSeat, advisoryCodeAccessGrantsNothing}},
		{name: caseAtlassian, exposed: true},
		{name: caseSeatPools},
		{name: caseOptionalKeys},
	}
}

// companyCaseNamed is the case of that name.
func companyCaseNamed(t *testing.T, name string) companyCase {
	t.Helper()
	for _, c := range companyCases() {
		if c.name == name {
			return c
		}
	}
	t.Fatalf("no company case %s", name)
	return companyCase{}
}

func (c companyCase) input(t *testing.T) *Input {
	t.Helper()
	files := []string{filepath.Join("testdata", "company", c.name+".input.yaml")}
	if c.name == sampleCompany {
		samples, err := filepath.Glob(filepath.Join("..", "..", "config", "samples", "v1alpha1_*.yaml"))
		if err != nil || len(samples) == 0 {
			t.Fatalf("the samples in config/samples: %v", err)
		}
		files = samples
	}
	in := load(t, files...).input
	if c.exposed {
		in.PublicBaseURL = "https://" + c.name + ".hooks.example.com"
	}
	return &in
}

// TestCompanyGolden pins every case's document. A difference is either a
// regression or an intended change, which -update records for review.
func TestCompanyGolden(t *testing.T) {
	for _, c := range companyCases() {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Company(c.input(t))
			if err != nil {
				t.Fatal(err)
			}
			golden(t, filepath.Join("testdata", "company", c.name+".yaml"), doc.YAML)
		})
	}
}

var pointer = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)\}`)

// TestCompanyRefsAreExactlyThePointers holds Refs to the document: every
// ${NAME} in the document is the store name of a listed key, and every
// listed key is pointed at. A credential the renderer wrote without
// recording it would never be pushed into the engine's store, and a key
// recorded but never written would be pushed for nothing.
func TestCompanyRefsAreExactlyThePointers(t *testing.T) {
	for _, c := range companyCases() {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Company(c.input(t))
			if err != nil {
				t.Fatal(err)
			}
			var pointed []string
			for _, m := range pointer.FindAllStringSubmatch(string(doc.YAML), -1) {
				pointed = append(pointed, m[1])
			}
			slices.Sort(pointed)
			pointed = slices.Compact(pointed)

			listed := make([]string, 0, len(doc.Refs)+len(doc.Provisioned))
			for _, k := range doc.Refs {
				listed = append(listed, secretsync.StoreName(k))
			}
			listed = append(listed, doc.Provisioned...)
			slices.Sort(listed)
			if !slices.Equal(pointed, listed) {
				t.Errorf("the document points at %v, Refs and Provisioned list %v", pointed, listed)
			}
			if !slices.IsSorted(doc.Provisioned) || len(slices.Compact(slices.Clone(doc.Provisioned))) != len(doc.Provisioned) {
				t.Errorf("Provisioned is not sorted and free of duplicates: %v", doc.Provisioned)
			}
			for _, name := range doc.Provisioned {
				if secretsync.Owns(name) {
					t.Errorf("the provisioned credential %s has a name the secret sync owns, and would delete", name)
				}
			}
			if !slices.IsSortedFunc(doc.Refs, secretref.Compare) || len(slices.Compact(slices.Clone(doc.Refs))) != len(doc.Refs) {
				t.Errorf("Refs is not sorted and free of duplicates: %v", doc.Refs)
			}
		})
	}
}

// TestCompanyEveryCredentialPosition names each credential position the API
// covers and the Secret key (namespace/name[key]) the secret-positions case
// fills it from, so a position that loses its reference, or renders
// anything else, fails by name rather than as a golden diff.
func TestCompanyEveryCredentialPosition(t *testing.T) {
	doc, err := Company(companyCaseNamed(t, "secret-positions").input(t))
	if err != nil {
		t.Fatal(err)
	}
	positions := []struct{ path, key string }{
		{"providers.llm.anthropic.auth.api_key", "tenant-nimbus/anthropic[apiKey]"},
		{"providers.llm.platform-claude.auth.api_key", "crewlet-system/platform-llm[anthropic]"},
		{"providers.llm.platform-claude.env.ANTHROPIC_BASE_URL", "crewlet-system/platform-llm[gateway]"},
		{"providers.llm.claude-subscription.auth.token", "tenant-nimbus/claude-cli[oauthToken]"},
		{"providers.llm.codex-subscription.auth.credential_bundle", "tenant-nimbus/codex-cli[bundle]"},
		{"providers.llm.claude-subscription.env.CLAUDE_CODE_USE_PROXY", "tenant-nimbus/claude-cli[proxy]"},
		{"providers.embeddings.api_key", "tenant-nimbus/embeddings[apiKey]"},
		{"providers.sandbox.e2b.api_key", "tenant-nimbus/e2b[apiKey]"},
		{"providers.sandbox.setup[0].files..git-credentials", "tenant-nimbus/company-sandbox[gitCredentials]"},
		{"providers.sandbox.setup[0].env.NPM_TOKEN", "tenant-nimbus/company-sandbox[npmToken]"},
		{"mcp_servers[2].env.LINEAR_API_KEY", "tenant-nimbus/linear[apiKey]"},
		{"mcp_servers[0].headers.X-Org", "tenant-nimbus/github-mcp[org]"},
		{"units[0].mcp_env.linear.LINEAR_TEAM_TOKEN", "tenant-nimbus/engineering-linear[token]"},
		{"units[0].children[0].mcp_env.linear.LINEAR_TEAM_TOKEN", "tenant-nimbus/platform-linear[token]"},
		{"units[0].roles[0].mcp_env.github.Authorization", "tenant-nimbus/coder-github[authorization]"},
		{"units[0].roles[0].sandbox.env.GITHUB_TOKEN", "tenant-nimbus/coder-github[token]"},
		{"units[0].roles[0].sandbox.setup[0].files..npmrc", "tenant-nimbus/coder-sandbox[npmrc]"},
		{"units[0].roles[0].sandbox.setup[0].env.REGISTRY_TOKEN", "tenant-nimbus/coder-sandbox[registryToken]"},
		{"units[0].roles[0].integrations.slack.bot_token", "tenant-nimbus/coder-slack[botToken]"},
		{"units[0].roles[0].integrations.slack.signing_secret", "tenant-nimbus/coder-slack[signingSecret]"},
		{"units[0].roles[0].integrations.github.private_key", "tenant-nimbus/coder-github-app[privateKey]"},
		{"units[0].roles[0].integrations.github.webhook_secret", "tenant-nimbus/coder-github-app[webhookSecret]"},
		{githubWebhookSecret, "tenant-nimbus/github-org[webhookSecret]"},
		{"integrations.github.token", "tenant-nimbus/github-org[token]"},
		{"units[0].roles[0].integrations.mattermost.bot_token", "tenant-nimbus/coder-mattermost[botToken]"},
		{"integrations.gitlab.signing_secret", "tenant-nimbus/gitlab[signingSecret]"},
		{"integrations.gitlab.token", "tenant-nimbus/gitlab[readToken]"},
		{"integrations.gitlab.provisioning.admin_token", "tenant-nimbus/gitlab-admin[token]"},
		{"integrations.mattermost.provisioning.admin_token", "tenant-nimbus/mattermost-admin[token]"},
		{"integrations.jira.token", "tenant-nimbus/jira[token]"},
		{"integrations.jira.webhook_secret", "tenant-nimbus/jira[webhookSecret]"},
		{"integrations.confluence.token", "tenant-nimbus/confluence[token]"},
		{"integrations.confluence.webhook_secret", "tenant-nimbus/confluence[webhookSecret]"},
		{"integrations.confluence.webhook_token", "tenant-nimbus/confluence[webhookToken]"},
		{"integrations.datadog.webhook_token", "tenant-nimbus/datadog[webhookToken]"},
		{"integrations.datadog.provisioning.api_key", "tenant-nimbus/datadog[apiKey]"},
		{"integrations.datadog.provisioning.app_key", "tenant-nimbus/datadog[appKey]"},
		{"integrations.atlassian.api_key", "tenant-nimbus/atlassian[apiKey]"},
	}
	// The seats' provisioned credentials, named as the engine's
	// provisioning finds them: one value wherever a seat names it.
	provisioned := []struct{ path, handle, name string }{
		{"units[0].roles[0].mcp_env.gitlab.GITLAB_TOKEN", "coder", "gitlab-token"},
		{"units[0].roles[0].sandbox.env.GITLAB_TOKEN", "coder", "gitlab-token"},
		{"roles[0].integrations.mattermost.bot_token", "support", "mattermost-bot-token"},
	}
	byKey := map[string]secretref.Key{}
	for _, k := range doc.Refs {
		byKey[k.String()] = k
	}
	leaves := flatten(t, doc.YAML)
	for _, p := range positions {
		k, ok := byKey[p.key]
		if !ok {
			t.Errorf("%s: %s is not among the document's Refs", p.path, p.key)
			continue
		}
		if want := "${" + secretsync.StoreName(k) + "}"; leaves[p.path] != want {
			t.Errorf("%s = %v, want %s", p.path, leaves[p.path], want)
		}
	}
	if len(doc.Refs) != len(positions) {
		t.Errorf("the document references %d keys, the test names %d positions: name the new one here", len(doc.Refs), len(positions))
	}
	names := map[string]bool{}
	for _, p := range provisioned {
		name := secretsync.ProvisionedName(p.handle, p.name)
		names[name] = true
		if want := "${" + name + "}"; leaves[p.path] != want {
			t.Errorf("%s = %v, want %s", p.path, leaves[p.path], want)
		}
	}
	if len(doc.Provisioned) != len(names) {
		t.Errorf("the document lists %d provisioned credentials, the test names %d: name the new one here", len(doc.Provisioned), len(names))
	}

	// Every position credentialPositions says the API covers is filled
	// here, and every reference the document holds is at one of them.
	var referenced []Path
	visitLeaves(t, doc.YAML, func(at Path, v any) {
		if value, _ := v.(string); !pointer.MatchString(value) {
			return
		}
		referenced = append(referenced, at)
		if position, ok := positionOf(tierCompany, at); !ok || position.Unsupported != "" {
			t.Errorf("%s holds a reference, and credentialPositions lists no covered position there", at)
		}
	})
	for _, position := range credentialPositions {
		if position.Tier != tierCompany || position.Unsupported != "" {
			continue
		}
		if !slices.ContainsFunc(referenced, func(p Path) bool { return matchesPosition(position.Path, p) }) {
			t.Errorf("credentialPositions covers %s, and the secret-positions case fills it nowhere: add it there", position.Path)
		}
	}
}

// flatten decodes a document into its scalar leaves, keyed by their paths
// as Crewlet prints them.
func flatten(t *testing.T, document []byte) map[string]any {
	t.Helper()
	var tree any
	if err := yaml.Unmarshal(document, &tree); err != nil {
		t.Fatal(err)
	}
	leaves := map[string]any{}
	var walk func(at Path, v any)
	walk = func(at Path, v any) {
		switch v := v.(type) {
		case map[string]any:
			for key, child := range v {
				walk(append(slices.Clone(at), key), child)
			}
		case []any:
			for i, child := range v {
				walk(append(slices.Clone(at), i), child)
			}
		default:
			leaves[at.String()] = v
		}
	}
	walk(nil, tree)
	return leaves
}

// TestCompanyIsDeterministic renders each case from its objects listed in
// many orders: the document, its hash, its refs and its sources must not
// change, or every reconcile would see a change that is not one and apply a
// new revision.
func TestCompanyIsDeterministic(t *testing.T) {
	for _, c := range companyCases() {
		t.Run(c.name, func(t *testing.T) {
			want, err := Company(c.input(t))
			if err != nil {
				t.Fatal(err)
			}
			for order := range 8 {
				in := c.input(t)
				reorder(in.Units, order)
				reorder(in.Seats, order)
				reorder(in.LLMProviders, order)
				reorder(in.MCPServers, order)
				reorder(in.Integrations, order)
				reorder(in.Company.Spec.EngineConfig, order)
				got, err := Company(in)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got.YAML, want.YAML) || got.Hash != want.Hash {
					t.Fatalf("order %d: the document changed with the order of its objects:\n%s", order, got.YAML)
				}
				if !slices.Equal(got.Refs, want.Refs) {
					t.Fatalf("order %d: refs %v, want %v", order, got.Refs, want.Refs)
				}
				if fmt.Sprint(got.Sources) != fmt.Sprint(want.Sources) {
					t.Fatalf("order %d: sources %v, want %v", order, got.Sources, want.Sources)
				}
			}
		})
	}
}

// reorder puts s in the order-th of a set of orders that between them move
// every element to every position: rotations, each also reversed.
func reorder[T any](s []T, order int) {
	if len(s) == 0 {
		return
	}
	k := (order / 2) % len(s)
	slices.Reverse(s[:k])
	slices.Reverse(s[k:])
	slices.Reverse(s)
	if order%2 == 1 {
		slices.Reverse(s)
	}
}

// TestCompanyHashIsOfTheYAML pins what Hash is: the hex SHA-256 of the
// bytes, which is what a controller compares and records as appliedHash.
func TestCompanyHashIsOfTheYAML(t *testing.T) {
	doc, err := Company(companyCaseNamed(t, "minimal").input(t))
	if err != nil {
		t.Fatal(err)
	}
	// The SHA-256 of "name: Tiny\n", the whole minimal document.
	if want := "07cfca96aa4b4f2676edeed094ae837a83d856e8691991903e79655e5f5c2fa9"; doc.Hash != want || string(doc.YAML) != "name: Tiny\n" {
		t.Fatalf("hash %s of %q, want %s", doc.Hash, doc.YAML, want)
	}
}

// TestCompanyLeavesItsInputAlone holds the renderer to a pure function: the
// controller renders from objects in its cache, which it must not modify.
func TestCompanyLeavesItsInputAlone(t *testing.T) {
	in := companyCaseNamed(t, sampleCompany).input(t)
	before := make([]string, 0, len(in.Seats))
	for _, s := range in.Seats {
		before = append(before, s.Name)
	}
	if _, err := Company(in); err != nil {
		t.Fatal(err)
	}
	for i, s := range in.Seats {
		if s.Name != before[i] {
			t.Fatalf("the Seats were reordered: %v, then %s at %d", before, s.Name, i)
		}
	}
}

// TestEngineConfigCannotSetAKeyAFieldRenders holds the three lists of the
// top-level keys the API's fields and objects render to one another, each
// both ways: the keys of the company document (companyDoc), the keys Diff
// compares as the API's own (typedKeys), and the keys the CRD's rule on a
// Company's engineConfig refuses, read off the rule (engineConfigReserved).
// The render merges every engineConfig entry into the document as a
// top-level key, trusting the API to refuse each key a field renders: a key
// the rule let through would be replaced by an entry of its name, and a key
// the rule refused that no field renders would be one nothing could set. A
// key typedKeys left out would be compared as a tuning block, so an edit made
// to it through the engine would go unfound; one it listed that no field
// renders would be compared exactly, so the defaults the engine serves for
// it would read as drift.
func TestEngineConfigCannotSetAKeyAFieldRenders(t *testing.T) {
	doc := reflect.TypeFor[companyDoc]()
	rendered := make([]string, 0, doc.NumField())
	for f := range doc.Fields() {
		key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		rendered = append(rendered, key)
	}
	for _, key := range lacking(typedKeys, rendered) {
		t.Errorf("a field of the API renders %s, and typedKeys does not list it: Diff would compare it as a "+
			"tuning block, so an edit made to it through the engine would go unfound", key)
	}
	for _, key := range lacking(rendered, typedKeys) {
		t.Errorf("typedKeys lists %s, which no field of the API renders: Diff would compare it exactly, so the "+
			"defaults the engine serves for it would read as drift", key)
	}
	reserved := engineConfigReserved(t)
	for _, key := range lacking(reserved, rendered) {
		t.Errorf("a field of the API renders %s, and the CRD's rule lets engineConfig set it: an entry of that "+
			"name would replace what the field renders; add it to the rule on Company.spec.engineConfig", key)
	}
	for _, key := range lacking(rendered, reserved) {
		t.Errorf("the CRD's rule refuses %s in engineConfig, and no field of the API renders it: nothing could "+
			"set it; remove it from the rule on Company.spec.engineConfig", key)
	}
	// The keys read off the rule are the ones the API server refuses, with
	// the rule's message.
	validate := engineConfigValidator(t)
	for _, key := range reserved {
		errs := validate(map[string]any{keyName: key, "value": map[string]any{}})
		if !slices.ContainsFunc(errs, func(e *field.Error) bool { return strings.HasPrefix(e.Detail, engineConfigRefusal) }) {
			t.Errorf("the CRD's rule names %s, and its validator lets engineConfig set it (%v)", key, errs)
		}
	}
}

// reservedKeysRule is the rule on Company.spec.engineConfig that refuses the
// keys a field renders, as controller-gen writes it into the CRD, its list of
// quoted keys the first group. The rule that refuses code, a key no field
// renders, has another shape and is not read.
var reservedKeysRule = regexp.MustCompile(
	`^self\.all\(e, !\(e\.name in \[('[a-z][a-z0-9_]*'(?:, '[a-z][a-z0-9_]*')*)\]\)\)$`)

// engineConfigReserved is the keys the CRD's rule on Company.spec.engineConfig
// refuses as ones a field renders, read off the rule. It must be there
// exactly once, in the shape reservedKeysRule reads, or the test fails
// rather than read nothing.
func engineConfigReserved(t *testing.T) []string {
	t.Helper()
	var lists []string
	for _, r := range engineConfigSchema(t).XValidations {
		if m := reservedKeysRule.FindStringSubmatch(r.Rule); m != nil {
			lists = append(lists, m[1])
		}
	}
	if len(lists) != 1 {
		t.Fatalf("want exactly one rule of the shape %s on Company.spec.engineConfig, found %q", reservedKeysRule, lists)
	}
	keys := strings.Split(lists[0], ", ")
	for i, quoted := range keys {
		keys[i] = strings.Trim(quoted, "'")
	}
	return keys
}

// lacking is every key of want that keys does not hold.
func lacking(keys, want []string) []string {
	var out []string
	for _, key := range want {
		if !slices.Contains(keys, key) {
			out = append(out, key)
		}
	}
	return out
}
