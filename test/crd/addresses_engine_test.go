package crd_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// baseURLShapes are the addresses TestAddressShapesFollowTheEngine offers as
// a Company's spec.exposure.dashboardBaseURL, and with https:// for http://
// as its publicBaseURL: those Crewlet refuses at start (a host a browser
// rewrites, IPv4 short forms, decimal, hexadecimal and octal, a trailing
// dot, a name whose last label is a number; a host outside ASCII, as it is
// or percent-encoded; an IPv6 zone; a port outside 1 to 65535, one for each
// branch of the rule's pattern; a second ':' or a bracket outside an IPv6
// address, and brackets around anything else, which the API server of
// Kubernetes 1.34 and 1.35 takes as a URL), and their neighbours it takes
// (an octet of each branch of the rule's pattern, IPv6 addresses in each
// form, a control character or a NEL in a path, and a NEL in the public
// address's host, which no browser reads). internal/render's
// TestContractAddressShapes holds the render to the engine at these and
// more, in every place an address goes.
var baseURLShapes = []string{
	"http://127.1:8080", "http://127.1", "http://2130706433", "http://0x7f.0.0.1", "http://0177.0.0.1",
	"http://192.168.010.1", "http://1.2.3.4.", "http://01.2.3.4", "http://1.2.3.04", "http://256.1.1.1", "http://1.2.3",
	"http://123.", "http://1.2.3.4", "http://0.0.0.0", "http://127.0.0.1:8080", "http://10.0.0.1",
	"http://10.20.30.40:8080", "http://10.0.0.200", "http://10.0.0.255", "http://200.249.250.255",
	"http://255.255.255.255", "http://crewlet.123", "http://crewlet.123.", "http://crewlet.123..", "http://crewlet.0x1f",
	"http://crewlet.0X1F", "http://crewlet.0x", "http://0x.", "http://.1", "http://crewlet123", "http://123crewlet",
	"http://crewlet.1x", "http://1e1.example", "http://example.com.", "http://" + strings.Repeat("a", 2030) + ".1",
	"https://bücher.example", "https://xn--bcher-kva.example", "http://b%C3%BCcher.example", "http://a%C3.example",
	"http://a%25b.example", "http://[::1]", "http://[::1]:8080", "http://[fe80::1]", "http://[::ffff:127.0.0.1]",
	"http://[2001:db8:0:0:0:0:0:1]", "http://[1::]", "http://[::1.2.3.4]", "http://[fe80::1%25eth0]:8080",
	"http://[fe80::1%25]", "http://h.example:80:80", "http://h.example::80", "http://h.example:80:", "http://::1:8080",
	"http://h.example:65536:80", "http://127.1:8080:8080", "http://x[::1]:8080", "http://h.example[::1]:8080",
	"http://[1.2.3.4]", "http://[v1.x]", "http://[::1]]", "http://[12345::1]", "http://[1::2::3]",
	"http://[::ffff:127.1]", "http://[foo.example]",
	"http://h.example:0", "http://h.example:00000", "http://h.example:1", "http://h.example:080",
	"http://h.example:0000000000000000080", "http://h.example:65535", "http://h.example:065535", "http://h.example:65536",
	"http://h.example:65540", "http://h.example:65599", "http://h.example:65600", "http://h.example:65999",
	"http://h.example:66000", "http://h.example:69999", "http://h.example:70000", "http://h.example:065536",
	"http://h.example:0099999", "http://h.example:99999", "http://h.example:123456789012345678901234567890",
	"http://h.example:" + strings.Repeat("9", 2000), "http://h.example:",
	"http://h.example/", "http://h.example/nimbus/path", "http://h.example/ü", "http://h.example:8080/a%2Fb",
	"http://h.example/a\u0080b", "http://h.example/a\u0085b", "http://h.ex\u0085ample", "http://\u0085h.example",
	"http://h.ex\u0080ample", "http://localhost:8080", "http://LOCALHOST:8080",
	"http://h.example/?", "http://h.example#", "http://h.example/#", "http://u:p@h.example", "http://u@h.example",
	"http://@h.example", " http://h.example", "http://h.example ",
}

// apiRefusesMore are the shapes the API refuses although Crewlet would take
// them, and why it does on purpose. The test holds each to the engine too, so
// a shape the engine comes to refuse leaves the list.
var apiRefusesMore = map[string]string{
	"http://h.example/?": "an empty query: the API refuses a '?' anywhere, since no link the engine builds on the " +
		"address may carry one",
	"http://h.example#": "an empty fragment: the API refuses a '#' anywhere, since no link the engine builds on the " +
		"address may carry one",
	"http://h.example/#": "an empty fragment: the API refuses a '#' anywhere, since no link the engine builds on the " +
		"address may carry one",
	"http://u:p@h.example": "a user and password: a credential is never written into an address",
	"http://u@h.example":   "a user: a credential is never written into an address",
	"http://@h.example":    "an empty user: a credential is never written into an address",
	" http://h.example":    "white space around the address, which the engine trims and the API refuses as no URL",
	"http://h.example ":    "white space around the address, which the engine trims and the API refuses as no URL",
}

// apiHostShapes are the host name templates TestAddressShapesFollowTheEngine
// offers as an EngineClass's spec.exposure.api.hostname, whose expansion is
// where a browser reaches a Company's engine and an origin it accepts.
var apiHostShapes = []string{
	"{{uid}}.123", "{{uid}}.0x1f", "{{uid}}.0x", "{{uid}}.10.0.0.1", "{{uid}}.4",
	"{{uid}}.123x", "{{uid}}.x123", "{{uid}}.0xg", "{{uid}}.api.example.com",
	"{{name}}.{{namespace}}", "{{name}}.{{namespace}}.example.com", "{{uid}}.{{namespace}}",
	"0x{{name}}.{{namespace}}.x", "{{uid}}.1{{namespace}}", "{{uid}}.0x{{name}}",
}

// TestAddressShapesFollowTheEngine holds the API's verdict on the addresses a
// Company and its class give a data pod's engine (Crewlet's api.external_url,
// api.public.external_url and api.auth.allowed_origins) to the engine every
// suite runs, both ways where that is honest: every shape the API admits
// renders to a crewlet.yaml `crewlet validate` takes, so no Company the API
// admits crash-loops its data pod; and every shape it refuses Crewlet refuses
// too, but for apiRefusesMore. A class's API host name template that only a
// Company's namespace or name makes a number of is admitted, and that
// Company's address is refused by the render as by the engine, which holds the
// pods back (RuntimeReady=False AddressRefused).
func TestAddressShapesFollowTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	for i, shape := range baseURLShapes {
		t.Run(fmt.Sprintf("%.40q", shape), func(t *testing.T) {
			dashboard := company(namespace, fmt.Sprintf("d%03d", i))
			dashboard.Spec.Exposure.DashboardBaseURL = shape
			// The operator drops a trailing '/' before the engine sees it.
			holdAddress(t, shape, shape, admitted(t, dashboard), render.FieldExternalURL, strings.TrimRight(shape, "/"))

			https := strings.Replace(shape, "http://", "https://", 1)
			public := company(namespace, fmt.Sprintf("p%03d", i))
			public.Spec.Exposure.PublicBaseURL = https
			holdAddress(t, shape, https, admitted(t, public), render.FieldPublicURL, strings.TrimRight(https, "/"))
		})
	}
	// heldBack counts the templates the API admits whose address the engine
	// refuses for one Company and takes for another: those only the render
	// can refuse.
	heldBack := 0
	for i, template := range apiHostShapes {
		t.Run(template, func(t *testing.T) {
			class := engineClass("", fmt.Sprintf("%s-h%02d", strings.ToLower(namespacePrefix(namespace)), i))
			class.Spec.Exposure.API = v1.APIExposure{Mode: v1.APIExposureGateway, Gateway: gatewayExposure(), Hostname: template}
			ok := admitted(t, class)
			// A tenant's namespace and name, and one whose namespace is a
			// number.
			for _, ns := range []string{"tenant-a", "4711"} {
				c := company(ns, companyName)
				c.UID = types.UID("6f1d4c2e-0b7a-4d6e-9c3f-2a5b8e7d1c04")
				routed := "https://" + render.ExpandTemplate(template, c)
				engine := engineRefuses(t, render.FieldExternalURL, routed)
				if engine != engineRefuses(t, render.FieldAllowedOrigins, routed) {
					t.Fatalf("crewlet validate takes %s as one of api.external_url and api.auth.allowed_origins "+
						"and not the other", routed)
				}
				switch {
				case !ok && !engine:
					t.Errorf("the API refuses api.hostname %q, and crewlet validate takes %s", template, routed)
				case !ok:
				case renderRefuses(t, render.FieldExternalURL, routed) != engine ||
					renderRefuses(t, render.FieldAllowedOrigins, routed) != engine:
					t.Errorf("the render's verdict on %s, api.hostname %q in namespace %s, is not crewlet validate's (refused: %v)",
						routed, template, ns, engine)
				case engine && ns == "tenant-a":
					t.Errorf("the API admits api.hostname %q, and crewlet validate refuses %s", template, routed)
				case engine:
					heldBack++
				}
			}
		})
	}
	if heldBack == 0 {
		t.Error("no template the API admits makes an address the engine refuses for a namespace that is a number: " +
			"the render's refusal of a derived address is tried nowhere")
	}
}

// admitted is whether the API server admits obj, which fails the test when it
// refuses it for another reason than validation.
func admitted(t *testing.T, obj client.Object) bool {
	t.Helper()
	err := k8sClient.Create(ctx, obj)
	if err != nil && !apierrors.IsInvalid(err) {
		t.Fatalf("creating %s: %v", obj.GetName(), err)
	}
	return err == nil
}

// holdAddress holds the API's verdict on value, admitted or not, to the
// render's and the engine's on address, what the operator writes for it at
// field. shape is the entry of baseURLShapes value was made from. An address
// the API admits is rendered as the operator renders it, and the engine is
// handed that crewlet.yaml as it is written, so that what the YAML does to
// the address between the two is held too.
func holdAddress(t *testing.T, shape, value string, admitted bool, field, address string) {
	t.Helper()
	engine := engineRefuses(t, field, address)
	why, more := apiRefusesMore[shape]
	switch {
	case more:
		if admitted || engine {
			t.Errorf("apiRefusesMore lists %q (%s), and the API admits %q: %v, crewlet validate refuses it: %v",
				shape, why, value, admitted, engine)
		}
	case admitted && engine:
		t.Errorf("the API admits %q, and crewlet validate refuses %s %q, so the data pod would crash-loop",
			value, field, address)
	case admitted:
		doc, err := render.Bootstrap(inputWith(field, address))
		if err != nil {
			t.Errorf("the API admits %q, and the render refuses %s %q, which crewlet validate takes: %v",
				value, field, address, err)
			return
		}
		if problems, out := validate(t, doc.YAML); len(problems) > 0 {
			t.Errorf("the API admits %q, and crewlet validate refuses the crewlet.yaml the render writes for %s %q, "+
				"so the data pod would crash-loop:\n%s", value, field, address, out)
		}
	case !engine:
		t.Errorf("the API refuses %q, and crewlet validate takes %s %q", value, field, address)
	}
}

// bootstrapInput is a data pod's Tier A for a Company of namespace tenant-a,
// on the given addresses.
func bootstrapInput(external, public string, origins []string) *render.BootstrapInput {
	c := company("tenant-a", companyName)
	c.UID = types.UID("6f1d4c2e-0b7a-4d6e-9c3f-2a5b8e7d1c04")
	return &render.BootstrapInput{
		Company: c,
		Class:   engineClass("", "basic"),
		Keyring: render.Keyring{
			ActiveKeyID: "k1", Keys: map[string]string{"k1": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))},
		},
		Tokens:         render.APITokens{Operator: strings.Repeat("o", 43), Access: strings.Repeat("a", 43)},
		VolumeSize:     resource.MustParse("10Gi"),
		ExternalURL:    external,
		PublicURL:      public,
		AllowedOrigins: origins,
	}
}

// The addresses the engine takes, which every Tier A here starts from.
const (
	takenExternal = "http://localhost:8080"
	takenPublic   = "http://nimbus-engine.tenant-a.svc:8081"
)

// inputWith is a data pod's Tier A input on the addresses the engine takes,
// but for address at field.
func inputWith(field, address string) *render.BootstrapInput {
	in := bootstrapInput(takenExternal, takenPublic, nil)
	switch field {
	case render.FieldExternalURL:
		in.ExternalURL = address
	case render.FieldPublicURL:
		in.PublicURL = address
	case render.FieldAllowedOrigins:
		in.AllowedOrigins = []string{address}
	}
	return in
}

// renderRefuses is whether render.Bootstrap refuses address at field with an
// AddressError, and fails the test when it refuses it for anything else.
func renderRefuses(t *testing.T, field, address string) bool {
	t.Helper()
	_, err := render.Bootstrap(inputWith(field, address))
	refused, ok := errors.AsType[*render.AddressError](err)
	switch {
	case err == nil:
		return false
	case !ok || refused.Field != field:
		t.Fatalf("render.Bootstrap refused %s %q with %v", field, address, err)
	}
	return true
}

// engineRefuses is whether `crewlet validate` refuses a data pod's Tier A
// whose field holds address, and fails the test when it refuses it for
// anything else. The Tier A is written by yamlFlow rather than by the render,
// so that the engine is asked about the address itself, which the render may
// refuse.
func engineRefuses(t *testing.T, field, address string) bool {
	t.Helper()
	doc, err := render.Bootstrap(bootstrapInput(takenExternal, takenPublic, nil))
	if err != nil {
		t.Fatal(err)
	}
	var tierA map[string]any
	if err := yaml.Unmarshal(doc.YAML, &tierA); err != nil {
		t.Fatal(err)
	}
	api := tierA["api"].(map[string]any)
	path := field
	switch field {
	case render.FieldExternalURL:
		api["external_url"] = address
	case render.FieldPublicURL:
		api["public"].(map[string]any)["external_url"] = address
	case render.FieldAllowedOrigins:
		api["auth"].(map[string]any)["allowed_origins"] = []string{address}
		path += "[0]"
	}
	problems, out := validate(t, []byte(yamlFlow(tierA)))
	for _, p := range problems {
		if p != path {
			t.Fatalf("crewlet validate refused the Tier A at %s, not only at %s:\n%s", p, path, out)
		}
	}
	return len(problems) > 0
}

// validate is the paths at which `crewlet validate` refuses a data pod's
// crewlet.yaml, none when it takes it, and what it printed.
func validate(t *testing.T, crewletYAML []byte) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "crewlet.yaml")
	if err := os.WriteFile(file, crewletYAML, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), enginetest.Binary(t), "validate", "-json", "-tier", "bootstrap", file)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, render.EnvNodeID + "=nimbus-data-0"}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil && !errors.As(err, new(*exec.ExitError)) {
		t.Fatalf("running crewlet validate: %v", err)
	}
	var got struct {
		Valid    bool `json:"valid"`
		Problems []struct {
			Path string `json:"path"`
		} `json:"problems"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("crewlet validate printed no verdict (%v):\n%s\n%s", err, stdout.String(), stderr.String())
	}
	paths := make([]string, 0, len(got.Problems))
	for _, p := range got.Problems {
		paths = append(paths, p.Path)
	}
	if !got.Valid && len(paths) == 0 {
		t.Fatalf("crewlet validate refused the Tier A at no path:\n%s\n%s", stdout.String(), stderr.String())
	}
	return paths, stdout.String() + stderr.String()
}
