package crd_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

const (
	// probeNamespace is the namespace of the company the engine's verdicts
	// are read on, which the store names it is given are derived from.
	probeNamespace = "probe"
	// keyOptionalKeys is Crewlet's optional_keys, where its refusals are.
	keyOptionalKeys = "optional_keys"
	// headerLower is the Authorization header in another case than the
	// server writes it.
	headerLower = "authorization"
)

// optionalStoreName is the store name the operator writes a Secret key of the
// probe company's namespace as: the variable a value's reference names, which
// Crewlet refuses in place of the key it is written under.
func optionalStoreName(name, key string) string {
	return secretsync.StoreName(secretref.Key{Namespace: probeNamespace, Name: name, Key: key})
}

// TestSetupStepOptionalKeysFollowTheEngine holds the API's verdict on a setup
// step's optionalKeys to Crewlet's, both ways: the API refuses an entry
// exactly when crewlet validate refuses the step it renders to. Every value a
// step's env or files hold is read from a Secret, so an entry naming one of
// them names a reference, and nothing is left to Crewlet alone; and a
// variable's name in place of its key, the one shape the API cannot name
// itself, is refused by both, since no variable the operator writes is a key
// a step holds.
func TestSetupStepOptionalKeysFollowTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	shapes := [][]string{
		{envNPMToken}, {fileNPMRC}, {envNPMToken, fileNPMRC},
		{"NPM_CONFIG_REGISTRY"}, {"~/.npmrc"}, {"npm_token"}, {envNPMToken, "PROXY"},
		{optionalStoreName("company-sandbox", "token")},
	}
	steps := make([]v1.SandboxSetupStep, len(shapes))
	for i, keys := range shapes {
		steps[i] = v1.SandboxSetupStep{
			Name:         fmt.Sprintf("s%02d", i),
			Files:        []v1.SandboxFile{{Path: fileNPMRC, ValueFrom: fromSecret("company-sandbox", "npmrc")}},
			Env:          []v1.SecretEnvVar{{Name: envNPMToken, ValueFrom: fromSecret("company-sandbox", "token")}},
			OptionalKeys: keys,
		}
	}
	refused := engineRefusesSetupSteps(t, steps, v1.SandboxPlacementE2B, func(_ int, rest []engine.Segment) bool {
		return len(rest) >= 1 && rest[0].Key == keyOptionalKeys
	})
	apiRefused := 0
	for i, keys := range shapes {
		c := company(namespace, fmt.Sprintf("o%02d", i))
		c.Spec.Sandbox = v1.SandboxSettings{
			DefaultRunIn: v1.SandboxPlacementE2B,
			E2B:          v1.E2BSandbox{APIKeySecretRef: secretKey("e2b", apiKeyKey)},
			Setup:        []v1.SandboxSetupStep{steps[i]},
		}
		apiAdmits := admitted(t, k8sClient, c)
		if !apiAdmits {
			apiRefused++
		}
		switch {
		case apiAdmits && refused[i]:
			t.Errorf("optionalKeys %v: the API admits it, and crewlet validate refuses it, so the Company is applied "+
				"nowhere", keys)
		case !apiAdmits && !refused[i]:
			t.Errorf("optionalKeys %v: the API refuses it, and crewlet validate takes it", keys)
		}
	}
	if apiRefused == 0 || apiRefused == len(shapes) {
		t.Errorf("the API refuses %d of %d shapes: its rule is tried in one direction alone", apiRefused, len(shapes))
	}
}

// TestSeatSandboxOptionalKeysFollowTheEngine holds the API's verdict on a
// Seat's spec.sandbox.optionalKeys to Crewlet's, both ways, as
// TestSetupStepOptionalKeysFollowTheEngine holds a step's: a variable of env,
// read from a Secret or provisioned, is taken, and anything else, a
// variable's name in place of its key included, refused.
func TestSeatSandboxOptionalKeysFollowTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	shapes := [][]string{
		{envDatabaseURL}, {gitlabToken}, {envDatabaseURL, gitlabToken},
		{"PROXY_URL"}, {"database_url"}, {optionalStoreName("coder-db", "url")},
	}
	sandbox := func(keys []string) v1.SeatSandbox {
		return v1.SeatSandbox{
			Env: []v1.SeatEnvVar{
				{Name: envDatabaseURL, ValueFrom: seatFromSecret("coder-db", "url")},
				{
					Name:      gitlabToken,
					ValueFrom: v1.SeatCredentialSource{Provisioned: v1.ProvisionedCredential{Name: "gitlab-token"}},
				},
			},
			OptionalKeys: keys,
		}
	}
	apiRefused := 0
	for i, keys := range shapes {
		var crewletRefuses bool
		for _, p := range validateProblems(t, sandboxCompany(nil, new(sandbox(keys)), v1.SandboxPlacementE2B)) {
			at := render.PathOf(p.Segments).String()
			if !slices.ContainsFunc(p.Segments, func(s engine.Segment) bool { return s.Key == keyOptionalKeys }) {
				t.Fatalf("optionalKeys %v: crewlet validate refuses the document elsewhere (%s): %s", keys, at, p.Message)
			}
			crewletRefuses = true
		}
		s := seat(namespace, fmt.Sprintf("o%02d", i))
		s.Spec.Sandbox = sandbox(keys)
		apiAdmits := admitted(t, k8sClient, s)
		if !apiAdmits {
			apiRefused++
		}
		switch {
		case apiAdmits && crewletRefuses:
			t.Errorf("optionalKeys %v: the API admits it, and crewlet validate refuses it, so the company is applied "+
				"nowhere", keys)
		case !apiAdmits && !crewletRefuses:
			t.Errorf("optionalKeys %v: the API refuses it, and crewlet validate takes it", keys)
		}
	}
	if apiRefused == 0 || apiRefused == len(shapes) {
		t.Errorf("the API refuses %d of %d shapes: its rule is tried in one direction alone", apiRefused, len(shapes))
	}
}

// TestMCPServerOptionalKeysFollowTheEngine holds the operator's verdict on an
// MCPServer's optionalKeys, the API's and the render's together, to Crewlet's,
// both ways: the operator refuses an entry exactly when crewlet validate
// refuses the server's optional_keys in the document it renders. The API
// holds a key to the grammar a Seat's or a Unit's spec.mcp writes and a header
// to one spelling; the render, which sees the Seats and the Units, refuses an
// entry no value read from a Secret is written under, as Crewlet does, a
// variable's name in place of a key included.
func TestMCPServerOptionalKeysFollowTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	const registry, project, team = "NPM_CONFIG_REGISTRY", "TAVILY_PROJECT", "X-Team"
	type shape struct {
		name string
		http bool
		own  []v1.MCPEnvVar
		seat []v1.MCPCredentialValue
		keys []string
	}
	fromSecretOwn := func(name string) v1.MCPEnvVar {
		return v1.MCPEnvVar{Name: name, ValueFrom: fromSecret("tools", strings.ToLower(name))}
	}
	seatSecret := func(name string) v1.MCPCredentialValue {
		return v1.MCPCredentialValue{Name: name, ValueFrom: seatFromSecret("coder-tools", strings.ToLower(name))}
	}
	shapes := []shape{
		{
			name: "a variable the server reads from a Secret",
			own:  []v1.MCPEnvVar{fromSecretOwn(registry)}, keys: []string{registry},
		},
		{
			name: "a variable the server writes as a literal alone",
			own:  []v1.MCPEnvVar{literal(project)}, keys: []string{project},
		},
		{
			name: "a variable the server writes as a literal and a seat from a Secret",
			own:  []v1.MCPEnvVar{literal(project)}, seat: []v1.MCPCredentialValue{seatSecret(project)}, keys: []string{project},
		},
		{
			name: "a variable a seat gives outside a variable's grammar",
			seat: []v1.MCPCredentialValue{seatSecret("npm-config-proxy")}, keys: []string{"npm-config-proxy"},
		},
		{
			name: "a variable in another case",
			own:  []v1.MCPEnvVar{fromSecretOwn(registry)}, keys: []string{strings.ToLower(registry)},
		},
		{
			name: "a variable nothing writes",
			own:  []v1.MCPEnvVar{fromSecretOwn(registry)}, keys: []string{"NPM_CONFIG_USERCONFIG"},
		},
		{
			name: "a variable's store name in place of its key", own: []v1.MCPEnvVar{fromSecretOwn(registry)},
			keys: []string{optionalStoreName("tools", strings.ToLower(registry))},
		},
		{
			name: "a header in another case", http: true,
			own: []v1.MCPEnvVar{fromSecretOwn(headerAuthorization)}, keys: []string{headerLower},
		},
		{
			name: "a header the server writes as a literal alone", http: true,
			own: []v1.MCPEnvVar{literal(team)}, keys: []string{team},
		},
		{
			name: "a header the server writes as a literal and a seat from a Secret in another case", http: true,
			own: []v1.MCPEnvVar{literal(team)}, seat: []v1.MCPCredentialValue{seatSecret("x-team")}, keys: []string{"X-TEAM"},
		},
		{
			name: "one header listed in two cases", http: true, own: []v1.MCPEnvVar{fromSecretOwn(headerAuthorization)},
			keys: []string{headerAuthorization, headerLower},
		},
	}
	in := sandboxCompany(nil, nil, v1.SandboxPlacementE2B)
	coder := &in.Seats[slices.IndexFunc(in.Seats, func(s v1.Seat) bool { return s.Name == "coder" })]
	servers := make([]v1.MCPServer, len(shapes))
	apiAdmits := make([]bool, len(shapes))
	for i, sh := range shapes {
		s := mcpServer(namespace, fmt.Sprintf("s%02d", i))
		if sh.http {
			httpServer(s)
			for _, e := range sh.own {
				s.Spec.Headers = append(s.Spec.Headers, v1.MCPHeader(e))
			}
		} else {
			s.Spec.Env = sh.own
		}
		s.Spec.OptionalKeys = sh.keys
		apiAdmits[i] = admitted(t, k8sClient, s.DeepCopy())
		s.Namespace = probeNamespace
		servers[i] = *s
		if len(sh.seat) > 0 {
			coder.Spec.MCP = append(coder.Spec.MCP, v1.MCPCredentials{Name: s.Name, Env: sh.seat})
		}
	}
	in.MCPServers = servers

	// The operator's verdict: the API's, and the render's on the company.
	_, err := render.Company(in)
	renderRefuses := map[string]bool{}
	for _, r := range render.RefErrors(err) {
		if r.Kind != render.KindMCPServer || r.Reason != v1.ReasonOptionalKeyUnused {
			t.Fatalf("the render refuses the company elsewhere: %v", r)
		}
		renderRefuses[r.Name] = true
	}
	// Crewlet's, on the document the operator renders, with each server's
	// optional_keys as listed.
	for i := range in.MCPServers {
		in.MCPServers[i].Spec.OptionalKeys = nil
	}
	doc, err := render.Company(in)
	if err != nil {
		t.Fatalf("rendering the company without its optional keys: %v", err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(doc.YAML, &tree); err != nil {
		t.Fatal(err)
	}
	at := map[string]int{}
	for j, server := range tree["mcp_servers"].([]any) {
		entry := server.(map[string]any)
		name := entry["name"].(string)
		at[name] = j
		listed := shapes[slices.IndexFunc(servers, func(s v1.MCPServer) bool { return s.Name == name })].keys
		if len(listed) > 0 {
			entry[keyOptionalKeys] = listed
		}
	}
	edited, err := yaml.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	crewletRefuses := map[int]bool{}
	for _, p := range validateDocument(t, doc, edited) {
		s := p.Segments
		if len(s) < 3 || s[0].Key != "mcp_servers" || !s[1].IsIndex || s[2].Key != keyOptionalKeys {
			t.Fatalf("crewlet validate refuses the document elsewhere (%s): %s", render.PathOf(s), p.Message)
		}
		crewletRefuses[s[1].Index] = true
	}

	refused := 0
	for i, sh := range shapes {
		name := servers[i].Name
		operator := !apiAdmits[i] || renderRefuses[name]
		if operator {
			refused++
		}
		switch crewlet := crewletRefuses[at[name]]; {
		case !operator && crewlet:
			t.Errorf("%s: the operator takes optionalKeys %v, and crewlet validate refuses it, so the company is "+
				"applied nowhere", sh.name, sh.keys)
		case operator && !crewlet:
			t.Errorf("%s: the operator refuses optionalKeys %v (API %t, render %t), and crewlet validate takes it",
				sh.name, sh.keys, !apiAdmits[i], renderRefuses[name])
		}
	}
	if refused == 0 || refused == len(shapes) {
		t.Errorf("the operator refuses %d of %d shapes: its rules are tried in one direction alone", refused, len(shapes))
	}
}
