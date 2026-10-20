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
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
	corev1 "k8s.io/api/core/v1"
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
// address, brackets around anything else, and a ']' after an IPv6 zone,
// which the API server's own URL parser takes or refuses as Go was built
// and is run: run with looseURLParser it takes the second ':', and as it is
// built it reads the ']' into the zone), and their
// neighbours it takes (an octet of each branch of the rule's pattern, IPv6
// addresses in each form, a control character or a NEL in a path, and a NEL
// in the public address's host, which no browser reads). internal/render's
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
	"http://[fe80::1%25eth0]]/", "http://[fe80::1%25eth0]h]/p", "http://[fe80::1%25eth0]fe80::1]",
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

// zoneBracket is why the API refuses as a publicBaseURL an IPv6 address
// whose zone holds a ']', which Crewlet takes there.
const zoneBracket = "a ']' after an IPv6 zone: Go's URL parser, the engine's as well as the API server's, ends the " +
	"bracketed address at the last ']' and reads the rest into the zone, so the engine starts on a host no vendor " +
	"or sandbox can reach; the API takes an IPv6 address in brackets whose zone holds no ']'"

// publicRefusesMore are the shapes the API refuses as a publicBaseURL alone
// although Crewlet takes them there, and why: as a dashboardBaseURL Crewlet
// refuses every IPv6 zone.
var publicRefusesMore = map[string]string{
	"http://[fe80::1%25eth0]]/":       zoneBracket,
	"http://[fe80::1%25eth0]h]/p":     zoneBracket,
	"http://[fe80::1%25eth0]fe80::1]": zoneBracket,
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
// too, but for apiRefusesMore and publicRefusesMore. A Company's two
// addresses are offered to both API servers, the one whose URL parser takes
// a second ':' after a host too (looseURLParser): a host and port the engine
// refuses are refused by the CRD's own rules on either, whatever isURL() and
// url() take. A class's API host name template that only a Company's
// namespace or name makes a number of is admitted, and that Company's
// address is refused by the render as by the engine, which holds the pods
// back (RuntimeReady=False AddressRefused).
func TestAddressShapesFollowTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	createNamespace(t, looseClient, namespace)
	expectLooseParser(t, namespace)
	for i, shape := range baseURLShapes {
		t.Run(fmt.Sprintf("%.40q", shape), func(t *testing.T) {
			dashboard := company(namespace, fmt.Sprintf("d%03d", i))
			dashboard.Spec.Exposure.DashboardBaseURL = shape
			// The operator drops a trailing '/' before the engine sees it.
			holdAddress(t, shape, shape, admissions(t, dashboard), render.FieldExternalURL, strings.TrimRight(shape, "/"))

			https := strings.Replace(shape, "http://", "https://", 1)
			public := company(namespace, fmt.Sprintf("p%03d", i))
			public.Spec.Exposure.PublicBaseURL = https
			holdAddress(t, shape, https, admissions(t, public), render.FieldPublicURL, strings.TrimRight(https, "/"))
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
			ok := admitted(t, k8sClient, class)
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

// artifactsBaseURLShapes are artifactsBaseURLs either side of each line Crewlet
// draws for an artifact origin, beside a dashboard at takenExternal.
var artifactsBaseURLShapes = []string{
	"https://{artifact}.artifacts.example.net", "https://artifacts.example.com", "https://artifacts.example.com/",
	"https://artifacts.example.com:8443", "http://localhost:8082", "http://{artifact}.localhost:8082",
	"http://127.0.0.1:8082", "http://127.0.0.2", "https://{artifact}--acme.example.net",
	"https://{artifact}-" + strings.Repeat("a", 36) + ".example.net", "https://1.2.3.4",
	"http://artifacts.example.com", "http://10.0.0.1:8082", "ftp://artifacts.example.com",
	"https://{artifact}{artifact}.example.net", "https://x{artifact}.example.net", "https://{artifact}",
	"https://{artifact}:8443", "https://{artifact}_x.example.net", "https://{artifact}X.example.net",
	"https://{artifact}-" + strings.Repeat("a", 37) + ".example.net",
	"https://artifacts.example.com/pages", "https://artifacts.example.com?x=1", "https://artifacts.example.com#x",
	"https://artifacts.example.com:0", "https://artifacts.example.com:65536",
	"http://[::1]:8082", "https://Artifacts.example.com", spacedArtifactsURL, "https://artifacts.example.com.",
	"https://u@artifacts.example.com", "https://127.1", "https://artifacts.123",
}

// spacedArtifactsURL is an artifact origin with white space before it.
const spacedArtifactsURL = " https://artifacts.example.com"

// artifactsAPIRefusesMore are the shapes the API refuses although Crewlet
// takes them, and why.
var artifactsAPIRefusesMore = map[string]string{
	"http://[::1]:8082": "an IPv6 loopback address: the API takes plain http on localhost, a name under it and " +
		"127.x.x.x, which a port-forwarding browser reaches",
	"https://Artifacts.example.com":  "a host in upper case, which a browser lowers before it compares an origin",
	spacedArtifactsURL:               "white space around the address, which the engine trims",
	"https://artifacts.example.com.": "a trailing dot, which names the host a browser treats as another site",
	"https://u@artifacts.example.com": "a user: a credential is never written into an address, and an artifact " +
		"origin is reached anonymously",
	"https://127.1": "a host whose last label is a number and that is not four decimal octets, which a browser " +
		"rewrites before it sends it",
	"https://artifacts.123": "a host whose last label is a number, which a browser reads as an IPv4 address",
}

// artifactsHeldBack are the shapes the API admits and Crewlet refuses beside
// the dashboard, which only the render can see and refuses.
var artifactsHeldBack = []string{takenExternal}

// TestArtifactsBaseURLFollowsTheEngine holds the API's rules on
// artifactsBaseURL to crewlet validate.
func TestArtifactsBaseURLFollowsTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	for i, shape := range append(slices.Clone(artifactsBaseURLShapes), artifactsHeldBack...) {
		t.Run(fmt.Sprintf("%.40q", shape), func(t *testing.T) {
			c := company(namespace, fmt.Sprintf("a%03d", i))
			c.Spec.Exposure.ArtifactsBaseURL = shape
			ok := admitted(t, k8sClient, c)
			// The operator drops a trailing '/' before the engine sees it.
			address := strings.TrimRight(shape, "/")
			engine := engineRefuses(t, render.FieldArtifactsURL, address)
			why, more := artifactsAPIRefusesMore[shape]
			heldBack := slices.Contains(artifactsHeldBack, shape)
			switch {
			case more && (ok || engine):
				t.Errorf("%q is listed as refused by the API alone (%s): the API admits it: %v, crewlet validate "+
					"refuses it: %v", shape, why, ok, engine)
			case more:
			case heldBack && (!ok || !engine || !renderRefuses(t, render.FieldArtifactsURL, address)):
				t.Errorf("%q is listed as held back by the render: the API admits it: %v, crewlet validate refuses "+
					"it: %v", shape, ok, engine)
			case heldBack:
			case ok && engine:
				t.Errorf("the API admits artifactsBaseURL %q, and crewlet validate refuses it, so the data pod "+
					"would crash-loop", shape)
			case ok:
				doc, err := render.Bootstrap(inputWith(render.FieldArtifactsURL, address))
				if err != nil {
					t.Fatalf("the API admits artifactsBaseURL %q, and the render refuses it: %v", shape, err)
				}
				if problems, out := validate(t, doc.YAML); len(problems) > 0 {
					t.Errorf("the API admits artifactsBaseURL %q, and crewlet validate refuses the crewlet.yaml "+
						"the render writes for it:\n%s", shape, out)
				}
			case !engine:
				t.Errorf("the API refuses artifactsBaseURL %q, and crewlet validate takes it", shape)
			}
		})
	}
}

// admitted is whether the API server c talks to admits obj, which fails the
// test when it refuses it for another reason than validation.
func admitted(t *testing.T, c client.Client, obj client.Object) bool {
	t.Helper()
	err := c.Create(ctx, obj)
	if err != nil && !apierrors.IsInvalid(err) {
		t.Fatalf("creating %s: %v", obj.GetName(), err)
	}
	return err == nil
}

// admission is whether one of the API servers admits an object.
type admission struct {
	server   string
	admitted bool
}

// admissions is what each API server answers to obj: the one every test
// asks, and the one whose URL parser is Go's looser one.
func admissions(t *testing.T, obj client.Object) []admission {
	t.Helper()
	return []admission{
		{"the API server", admitted(t, k8sClient, obj.DeepCopyObject().(client.Object))},
		{"the API server run with GODEBUG=" + looseURLParser, admitted(t, looseClient, obj.DeepCopyObject().(client.Object))},
	}
}

// createNamespace creates the namespace name through c, for the API server
// that is not k8sClient's.
func createNamespace(t *testing.T, c client.Client, name string) {
	t.Helper()
	if err := c.Create(ctx, &corev1.Namespace{Name: name}); err != nil {
		t.Fatalf("creating namespace %s: %v", name, err)
	}
}

// expectLooseParser fails the test unless the second API server's URL parser
// takes what the first one's refuses, http://h.example:80:80: without that,
// the second answers as the first does and holds nothing more.
func expectLooseParser(t *testing.T, namespace string) {
	t.Helper()
	const shape, noURL, host = "http://h.example:80:80", "dashboardBaseURL must be an absolute URL",
		"dashboardBaseURL's host must be a name"
	errs := make([]error, 2)
	for i, c := range []client.Client{k8sClient, looseClient} {
		obj := company(namespace, "loose-parser")
		obj.Spec.Exposure.DashboardBaseURL = shape
		errs[i] = c.Create(ctx, obj)
		if !apierrors.IsInvalid(errs[i]) || !strings.Contains(errs[i].Error(), host) {
			t.Fatalf("dashboardBaseURL %q is not refused by its host rule: %v", shape, errs[i])
		}
	}
	if !strings.Contains(errs[0].Error(), noURL) {
		t.Fatalf("the API server takes dashboardBaseURL %q for a URL, which only the one run with GODEBUG=%s "+
			"should: %v", shape, looseURLParser, errs[0])
	}
	if strings.Contains(errs[1].Error(), noURL) {
		t.Fatalf("the API server run with GODEBUG=%s refuses dashboardBaseURL %q as no URL, so the setting "+
			"did not reach it, and it tries nothing the other does not: %v", looseURLParser, shape, errs[1])
	}
}

// holdAddress holds each API server's admission of value, admitted or not, to
// the render's and the engine's on address, what the operator writes for it
// at field. shape is the entry of baseURLShapes value was made from. An
// address an API server admits is rendered as the operator renders it, and
// the engine is handed that crewlet.yaml as it is written, so that what the
// YAML does to the address between the two is held too.
func holdAddress(t *testing.T, shape, value string, admissions []admission, field, address string) {
	t.Helper()
	engine := engineRefuses(t, field, address)
	why, more := apiRefusesMore[shape]
	if public, ok := publicRefusesMore[shape]; ok && field == render.FieldPublicURL {
		why, more = public, true
	}
	if more && engine {
		t.Errorf("%q is listed as refused by the API alone (%s), and crewlet validate refuses %s %q",
			shape, why, field, address)
	}
	rendered := false
	for _, v := range admissions {
		switch {
		case more:
			if v.admitted {
				t.Errorf("%q is listed as refused by the API alone (%s), and %s admits %q", shape, why, v.server, value)
			}
		case v.admitted && engine:
			t.Errorf("%s admits %q, and crewlet validate refuses %s %q, so the data pod would crash-loop",
				v.server, value, field, address)
		case v.admitted && !rendered:
			rendered = true
			doc, err := render.Bootstrap(inputWith(field, address))
			if err != nil {
				t.Errorf("%s admits %q, and the render refuses %s %q, which crewlet validate takes: %v",
					v.server, value, field, address, err)
				continue
			}
			if problems, out := validate(t, doc.YAML); len(problems) > 0 {
				t.Errorf("%s admits %q, and crewlet validate refuses the crewlet.yaml the render writes for %s %q, "+
					"so the data pod would crash-loop:\n%s", v.server, value, field, address, out)
			}
		case !v.admitted && !engine:
			t.Errorf("%s refuses %q, and crewlet validate takes %s %q", v.server, value, field, address)
		}
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
		VolumeSize:     resource.MustParse("16Gi"),
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
	case render.FieldArtifactsURL:
		in.ArtifactsURL = address
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
	case render.FieldArtifactsURL:
		api["artifacts"] = map[string]any{"port": render.ArtifactsPort, "external_url": address}
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
