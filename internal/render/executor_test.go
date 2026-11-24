package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// The keys the providers the executor tests add are signed in under, and
// the variable a harness signed in through env reads its model provider's key
// from.
const (
	keyAPIKey     = "apiKey"
	keyBundle     = "bundle"
	envGatewayKey = "OPENROUTER_API_KEY"
)

// The models the executor tests' providers run, each in its harness's own
// grammar: OpenCode's names the provider it routes through.
const (
	modelSonnet   = "sonnet"
	modelOpenCode = "openrouter/anthropic/claude-sonnet-5"
	modelCopilot  = "claude-sonnet-4.5"
	modelHermes   = "anthropic/claude-sonnet-5"
	modelPi       = "openrouter/anthropic/claude-sonnet-5"
)

// The providers the executor tests add to the seat-pools case, beside its
// own ClaudeCode provider claude.
const (
	providerNoRunner = "no-runner"
	providerOpenCode = "opencode"
	providerCopilot  = "copilot"
	providerHermes   = "hermes"
	providerPi       = "pi"
	platformHeadName = "class-claude"
)

// noRunnerModel is the model a provider of noRunnerHarness runs: one every
// harness but OpenCode, which runs an executor, takes as written, as the CRD
// suite probes every harness with.
const noRunnerModel = "probe/probe-model"

// noRunnerKeyInEnv is the variable each harness whose profile names no key
// variable signs in by from env, under auth mode Subscription: its model
// provider's key. Every other harness signs in under auth mode APIKey.
var noRunnerKeyInEnv = map[v1alpha1.Harness]string{
	v1alpha1.HarnessHermes: envGatewayKey,
	v1alpha1.HarnessPi:     envGatewayKey,
}

// noRunnerHarness is the first harness the LLMProvider API offers, in its
// enum's order, that Crewlet runs no seat's executor on: one outside
// ExecutorHarnesses, which TestExecutorHarnessesFollowTheEngineSource holds to
// the engine's executor blocks, so the pick moves on as the engine gains a
// runner, and no case names a harness a runner may come to.
func noRunnerHarness(t *testing.T) v1alpha1.Harness {
	t.Helper()
	for _, harness := range harnessEnum(t) {
		if !slices.Contains(ExecutorHarnesses, harness) {
			return harness
		}
	}
	t.Fatalf("every harness the LLMProvider API offers runs a seat's executor (%v), so NoExecutorHarness and the "+
		"report of a member that never runs it cannot happen: remove them and the cases that reach them",
		ExecutorHarnesses)
	return ""
}

// harnessEnum is the harnesses the LLMProvider CRD's spec.harness offers, in
// its order.
func harnessEnum(t *testing.T) []v1alpha1.Harness {
	t.Helper()
	node := crdOf(t, "llmproviders")
	for _, step := range []any{"spec", "versions", 0, "schema", "openAPIV3Schema", keyProperties, "spec", keyProperties,
		"harness", "enum"} {
		switch at := step.(type) {
		case string:
			m, _ := node.(map[string]any)
			node = m[at]
		case int:
			list, _ := node.([]any)
			if len(list) <= at {
				t.Fatalf("the LLMProvider CRD has no spec.harness enum")
			}
			node = list[at]
		}
	}
	values, _ := node.([]any)
	if len(values) == 0 {
		t.Fatalf("the LLMProvider CRD has no spec.harness enum")
	}
	out := make([]v1alpha1.Harness, len(values))
	for i, v := range values {
		s, _ := v.(string)
		out[i] = v1alpha1.Harness(s)
	}
	return out
}

// asNoRunner moves p, signed in with auth mode APIKey, to noRunnerHarness,
// signed in as the API admits for it: on the same key, from env where its
// profile names no key variable.
func asNoRunner(t *testing.T, p *v1alpha1.LLMProvider) {
	t.Helper()
	harness := noRunnerHarness(t)
	p.Spec.Harness, p.Spec.Model = harness, noRunnerModel
	if name, ok := noRunnerKeyInEnv[harness]; ok {
		key := p.Spec.Auth.APIKeySecretRef
		p.Spec.Auth = v1alpha1.HarnessAuth{Mode: v1alpha1.HarnessAuthSubscription}
		p.Spec.Env = []v1alpha1.SecretEnvVar{{Name: name, ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: key}}}
	}
}

// noRunnerProvider is a provider of noRunnerHarness, which serves a seat's
// other phases and never its executor.
func noRunnerProvider(t *testing.T) v1alpha1.LLMProvider {
	t.Helper()
	p := v1alpha1.LLMProvider{
		Namespace: testNamespace, Name: providerNoRunner,
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: sampleCompany},
			Auth: v1alpha1.HarnessAuth{
				Mode: v1alpha1.HarnessAuthAPIKey, APIKeySecretRef: v1alpha1.SecretKeyReference{Name: providerNoRunner, Key: keyAPIKey},
			},
		},
	}
	asNoRunner(t, &p)
	return p
}

// openCodeProvider is a provider of OpenCode signed in by its model
// provider's key in env.
func openCodeProvider() v1alpha1.LLMProvider {
	return v1alpha1.LLMProvider{
		Namespace: testNamespace, Name: providerOpenCode,
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: sampleCompany},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{
				Harness: v1alpha1.HarnessOpenCode, Model: modelOpenCode,
			},
			Auth: v1alpha1.HarnessAuth{Mode: v1alpha1.HarnessAuthSubscription},
			Env: []v1alpha1.SecretEnvVar{{
				Name:      envGatewayKey,
				ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: openRouter, Key: keyAPIKey}},
			}},
		},
	}
}

// copilotProvider is a provider of Copilot on a GitHub token under auth mode
// APIKey, which Crewlet hands the harness as COPILOT_GITHUB_TOKEN.
func copilotProvider() v1alpha1.LLMProvider {
	return v1alpha1.LLMProvider{
		Namespace: testNamespace, Name: providerCopilot,
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef:          v1alpha1.CompanyReference{Name: sampleCompany},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{Harness: v1alpha1.HarnessCopilot, Model: modelCopilot},
			Auth: v1alpha1.HarnessAuth{
				Mode: v1alpha1.HarnessAuthAPIKey, APIKeySecretRef: v1alpha1.SecretKeyReference{Name: providerCopilot, Key: keyAPIKey},
			},
		},
	}
}

// hermesProvider is a provider of Hermes signed in by its model provider's
// key in env.
func hermesProvider() v1alpha1.LLMProvider {
	p := openCodeProvider()
	p.Name = providerHermes
	p.Spec.Harness, p.Spec.Model = v1alpha1.HarnessHermes, modelHermes
	return p
}

// piProvider is a provider of Pi signed in by its model provider's key in
// env.
func piProvider() v1alpha1.LLMProvider {
	p := openCodeProvider()
	p.Name = providerPi
	p.Spec.Harness, p.Spec.Model = v1alpha1.HarnessPi, modelPi
	return p
}

// bundled is the provider provider makes, signed in by a credential bundle
// alone, which the box of a seat's executor run on it is seeded with.
func bundled(provider func() v1alpha1.LLMProvider) v1alpha1.LLMProvider {
	p := provider()
	p.Spec.Env = nil
	p.Spec.Auth = v1alpha1.HarnessAuth{CredentialBundleSecretRef: v1alpha1.SecretKeyReference{Name: p.Name, Key: keyBundle}}
	return p
}

// chainOfNames is a chain of the Company's own LLMProviders.
func chainOfNames(names ...string) []v1alpha1.LLMProviderReference {
	out := make([]v1alpha1.LLMProviderReference, len(names))
	for i, name := range names {
		out[i] = v1alpha1.LLMProviderReference{Name: name}
	}
	return out
}

// TestExecutor holds the statement on every agent seat's executor to where
// Crewlet runs it: the first member of the chain a launch can run, in a box
// on the engine pod that holds the seat, a data pod's and a seat pod's alike,
// which reaches the seat's tools on that pod's own listener on loopback, with
// or without a public address and whatever its provider signs in with. It
// names the members a launch falls back to and those it never runs, and is
// stated with or without spec.sandbox, which only tunes the boxes.
//
// Mutations, each red: gate ExecutorOf on spec.sandbox again (the case without
// one reports nothing); name the fallbacks from the chain rather than its
// runners (the member that never runs it is named a fallback).
func TestExecutor(t *testing.T) {
	const (
		claude = "LLMProvider/claude ClaudeCode"
		// ownListener is where a box calls the seat's tools back, on a
		// data pod and a seat pod alike.
		ownListener = "on the pod's own listener, on loopback"
	)
	everySeat := func(member string) []string {
		return []string{"ana " + member, "bo " + member, "cy " + member, "dee " + member}
	}
	// claudeBut is every seat on the head of the order, claude, but each of
	// executors, "seat member", in its seat's place.
	claudeBut := func(executors ...string) []string {
		out := everySeat(claude)
		for _, v := range executors {
			seat, _, _ := strings.Cut(v, " ")
			for i := range out {
				if strings.HasPrefix(out[i], seat+" ") {
					out[i] = v
				}
			}
		}
		return out
	}
	noRunner := noRunnerProvider(t)
	for _, c := range []struct {
		name   string
		mutate func(*Input)
		// want is each seat with an executor, as "seat kind/member harness".
		want []string
		// says are phrases every report says; of the seat named in
		// saysOf alone when it is set.
		says   []string
		saysOf string
	}{
		{
			// Every seat names no chain and runs on claude, the only
			// provider.
			name: "the head of the order",
			want: everySeat(claude),
			says: []string{"as ClaudeCode on LLMProvider claude", "Company nimbus's first LLMProvider by name"},
		},
		{
			name: "on the data pods",
			want: everySeat(claude), saysOf: seatCy,
			says: []string{"in a box on the data pod that holds the seat", ownListener},
		},
		{
			// A seat pod serves its own boxes the bridge on loopback, as a
			// data pod does.
			name: "on a seat pod",
			want: everySeat(claude), saysOf: seatBo,
			says: []string{"in a box on the pod of seat pool general that holds the seat", ownListener},
		},
		{
			// spec.sandbox tunes the boxes; the operator places every run
			// whether or not it is set.
			name:   "with no spec.sandbox",
			mutate: func(in *Input) { in.Company.Spec.Sandbox = v1alpha1.SandboxSettings{} },
			want:   everySeat(claude),
		},
		{
			// The box is seeded with the login on the engine pod.
			name: "a bundled ClaudeCode login",
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, bundled(claudeCodeProvider))
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerClaudeCode)
			},
			want: claudeBut("cy LLMProvider/claude-code ClaudeCode"), saysOf: seatCy,
			says: []string{"as ClaudeCode on LLMProvider claude-code, in a box on the data pod"},
		},
		{
			// Hermes runs the executor, so a launch takes it rather than
			// passing it over, and its login seeds the box as any other.
			name: "a bundled Hermes login",
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, bundled(hermesProvider))
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerHermes)
			},
			want: claudeBut("cy LLMProvider/hermes Hermes"), saysOf: seatCy,
			says: []string{"as Hermes on LLMProvider hermes"},
		},
		{
			name: "a bundled Pi login",
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, bundled(piProvider))
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerPi)
			},
			want: claudeBut("cy LLMProvider/pi Pi"), saysOf: seatCy,
			says: []string{"as Pi on LLMProvider pi"},
		},
		{
			// Copilot is a runner a launch takes past a member that never
			// runs it, which the report names among the harnesses that run
			// the executor.
			name: "a member that never runs it ahead of a Copilot",
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, noRunner, copilotProvider())
				seatNamed(in, seatBo).Spec.LLM.Default = chainOfNames(providerNoRunner, providerCopilot)
			},
			want: claudeBut("bo LLMProvider/copilot Copilot"), saysOf: seatBo,
			says: []string{
				"as Copilot on LLMProvider copilot",
				"LLMProvider no-runner (" + string(noRunner.Spec.Harness) + ") never runs it, as Crewlet runs a " +
					"seat's executor only on " + executorHarnessWords() + ": it serves the seat's other phases",
			},
		},
		{
			// A launch passes over a harness that cannot run the executor,
			// and is never said to fall back to it.
			name: "a member that never runs it ahead of a ClaudeCode",
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, noRunner)
				seatNamed(in, seatBo).Spec.LLM.Default = chainOfNames(providerNoRunner, "claude")
			},
			want: claudeBut(), saysOf: seatBo,
			says: []string{"LLMProvider no-runner (" + string(noRunner.Spec.Harness) + ") never runs it"},
		},
		{
			name: "a fallback",
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, noRunner, openCodeProvider())
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames("claude", providerNoRunner, providerOpenCode)
			},
			want: claudeBut(), saysOf: seatCy,
			says: []string{
				". While LLMProvider claude sits out its spent plan on the node, a launch falls back to " +
					"LLMProvider opencode as OpenCode. LLMProvider no-runner",
			},
		},
		{
			// Refused on its own (NoExecutorHarness): there is no executor to
			// state.
			name: "a seat whose chain no member of runs it",
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, noRunner)
				seatNamed(in, seatBo).Spec.LLM.Default = chainOfNames(providerNoRunner)
			},
			want: slices.DeleteFunc(everySeat(claude), func(v string) bool { return strings.HasPrefix(v, seatBo+" ") }),
		},
		{
			name: "a platform provider at the head of the order",
			mutate: func(in *Input) {
				in.Company.Spec.LLM.UsePlatformProviders = new(true)
				in.Class.Spec.PlatformLLMProviders = []v1alpha1.PlatformLLMProvider{{
					Name: platformHeadName, Harness: v1alpha1.HarnessClaudeCode, Model: "claude-sonnet-5",
					Auth: v1alpha1.PlatformLLMProviderAuth{
						Mode: v1alpha1.HarnessAuthAPIKey,
						APIKeySecretRef: v1alpha1.NamespacedSecretKeyReference{
							Namespace: "crewlet-system", Name: platformHeadName, Key: keyAPIKey,
						},
					},
				}}
				in.Company.Spec.LLM.Order = []v1alpha1.LLMProviderReference{
					{Kind: v1alpha1.LLMProviderKindPlatform, Name: platformHeadName},
				}
			},
			want: everySeat("PlatformLLMProvider/" + platformHeadName + " ClaudeCode"),
			says: []string{"the platform model provider class-claude", "the head of Company nimbus's spec.llm.order"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, caseSeatPools).input(t)
			if c.mutate != nil {
				c.mutate(in)
			}
			executors := ExecutorOf(in)
			got := make([]string, 0, len(executors))
			for _, seat := range slices.Sorted(maps.Keys(executors)) {
				e := executors[seat]
				kind := v1alpha1.LLMProviderKindLLMProvider
				if e.Platform {
					kind = v1alpha1.LLMProviderKindPlatform
				}
				got = append(got, fmt.Sprintf("%s %s/%s %s", seat, kind, e.Member, e.Harness))
				if c.saysOf != "" && seat != c.saysOf {
					continue
				}
				for _, phrase := range c.says {
					if !strings.Contains(e.Message, phrase) {
						t.Errorf("the report on %s does not say %q: %s", seat, phrase, e.Message)
					}
				}
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("reported %v, want %v", got, c.want)
			}
		})
	}
}

// ownProvider is the Company's own LLMProvider of that name.
func ownProvider(in *Input, name string) *v1alpha1.LLMProvider {
	for i := range in.LLMProviders {
		if in.LLMProviders[i].Name == name {
			return &in.LLMProviders[i]
		}
	}
	panic("no LLMProvider " + name)
}

// TestContractDirectCell holds the sandbox catalogue every document writes to
// the volume the operator keeps a run's boxes on and to what the engine takes:
// Crewlet's local backend with its boxes under SandboxDir, where every engine
// pod mounts its sandboxes volume, and nothing else of it, the rest being the
// container cell's; default_run_in direct; and the Company's spec.sandbox
// tuning, with or without it, in a company with an agent seat and in one with
// none. The engine takes each. And each is handed to the engine again without
// its local backend, and without its default_run_in, which the engine must
// refuse: each is what places a run in a box on the pod, not a key the engine
// would make up for, so a case it took either way would hold nothing.
//
// Mutation: write no default_run_in, and the engine refuses every document.
func TestContractDirectCell(t *testing.T) {
	tierA, err := Bootstrap(bootstrapCases()[0].input(t))
	if err != nil {
		t.Fatal(err)
	}
	config := write(t, "crewlet.yaml", tierA.YAML)
	const (
		catalogue        = keyProviders + ".sandbox"
		catalogueDefault = catalogue + ".default_run_in"
		entryRunIn       = keyProviders + ".llm.claude.run_in"
	)
	for _, c := range []struct {
		name   string
		mutate func(*Input)
		// want is the catalogue the document writes beside its local
		// backend and its default_run_in.
		want map[string]any
		// withoutDefaultAt is where the engine refuses the document
		// without its default_run_in: the entry a seat's executor launches
		// on, or the catalogue's default where none does.
		withoutDefaultAt string
		// alone says the company has no agent seat, so the engine gives
		// none of the advisories on one.
		alone bool
	}{
		{name: "no spec.sandbox", mutate: func(*Input) {}, withoutDefaultAt: entryRunIn},
		{
			name: "spec.sandbox's tuning",
			mutate: func(in *Input) {
				in.Company.Spec.Sandbox = v1alpha1.SandboxSettings{
					HostGraceSeconds: 1200, DefaultPauseTTLSeconds: new(int32(0)), CommitAttribution: new(false),
				}
			},
			want:             map[string]any{"host_grace_seconds": 1200, "default_pause_ttl_seconds": 0, "commit_attribution": false},
			withoutDefaultAt: entryRunIn,
		},
		{
			name:             "no agent seat",
			mutate:           func(in *Input) { in.Seats = nil },
			withoutDefaultAt: catalogueDefault, alone: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			// One ClaudeCode provider, claude, on a key, and one agent seat
			// with no chain of its own, whose executor runs on it.
			const base = "minimal-agent"
			in := companyCaseNamed(t, base).input(t)
			c.mutate(in)
			doc, err := Company(in)
			if err != nil {
				t.Fatalf("the render refuses the document: %v", err)
			}
			var tree struct {
				Providers struct {
					Sandbox map[string]any `json:"sandbox"`
				} `json:"providers"`
			}
			if err := yaml.Unmarshal(doc.YAML, &tree); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"local": map[string]any{"state_dir": SandboxDir}, "default_run_in": "direct"}
			maps.Copy(want, c.want)
			got, err := json.Marshal(tree.Providers.Sandbox)
			if err != nil {
				t.Fatal(err)
			}
			if wanted, err := json.Marshal(want); err != nil || !bytes.Equal(got, wanted) {
				t.Errorf("the document's providers.sandbox is %s, want %s (%v): its boxes on the sandboxes volume, in "+
					"the direct cell, and nothing of the container cell", got, wanted, err)
			}

			env := bootstrapEnv(tierA)
			for _, k := range doc.Refs {
				env[secretsync.StoreName(k)] = dummyCredential(k)
			}
			advisories := companyCaseNamed(t, base).advisories
			if c.alone {
				advisories = []advisory{advisoryNoHumanSeat}
			}
			validate(t, env, advisories, "-config", config, "-company", write(t, "company.yaml", doc.YAML))

			for key, at := range map[string]string{"local": catalogue, "default_run_in": c.withoutDefaultAt} {
				var without map[string]any
				if err := yaml.Unmarshal(doc.YAML, &without); err != nil {
					t.Fatal(err)
				}
				delete(without[keyProviders].(map[string]any)["sandbox"].(map[string]any), key)
				data, err := yaml.Marshal(without)
				if err != nil {
					t.Fatal(err)
				}
				got, stdout, stderr, _ := verdict(t, env, "-config", config, "-company", write(t, "without-"+key+".yaml", data))
				refused := slices.ContainsFunc(got.Problems, func(raw json.RawMessage) bool {
					var p struct{ Path string }
					return json.Unmarshal(raw, &p) == nil && p.Path == at
				})
				if got.Valid || !refused {
					t.Errorf("crewlet validate took the document without providers.sandbox.%s, or refused it elsewhere "+
						"than at %s:\n%s\n%s", key, at, stdout, stderr)
				}
			}
		})
	}
}
