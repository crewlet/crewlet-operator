package render

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// janeAccount is the account the tests claim for the sample company's
// founder, on whichever surface.
const janeAccount = "jane.founder"

// contactFields are SeatContact's fields by their JSON names.
func contactFields(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeFor[v1alpha1.SeatContact]()
	fields := make([]string, 0, typ.NumField())
	for f := range typ.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || f.Type.Kind() != reflect.String {
			t.Fatalf("SeatContact.%s is not a string field with a JSON name; contactAccounts has no row for it", f.Name)
		}
		fields = append(fields, name)
	}
	return fields
}

// setContact sets the field of c whose JSON name is field, which must be
// one: the fields are contactAccounts', which
// TestContactAccountsAreSeatContact holds to SeatContact.
func setContact(c *v1alpha1.SeatContact, field, value string) {
	v := reflect.ValueOf(c).Elem()
	for i := range v.NumField() {
		if name, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("json"), ","); name == field {
			v.Field(i).SetString(value)
			return
		}
	}
	panic("SeatContact has no field " + field)
}

// TestContactAccountsAreSeatContact holds contactAccounts to the API: a row
// for every field of SeatContact and for no other, each reading its own
// field and writing it, alone, under its key.
func TestContactAccountsAreSeatContact(t *testing.T) {
	rows := make([]string, 0, len(contactAccounts))
	for _, a := range contactAccounts {
		rows = append(rows, a.field)
	}
	fields := contactFields(t)
	if !slices.Equal(slices.Sorted(slices.Values(rows)), slices.Sorted(slices.Values(fields))) {
		t.Fatalf("contactAccounts has rows for %v, and SeatContact has the fields %v", rows, fields)
	}
	for _, a := range contactAccounts {
		var c v1alpha1.SeatContact
		setContact(&c, a.field, janeAccount)
		if got := a.value(&c); got != janeAccount {
			t.Errorf("%s: the row reads %q from a contact that sets only %s", a.field, got, a.field)
		}
		if got, want := contact(&c), map[string]string{a.key: janeAccount}; !maps.Equal(got, want) {
			t.Errorf("%s: the contact is written as %v, want %v", a.field, got, want)
		}
	}
	if got := contact(&v1alpha1.SeatContact{}); got != nil {
		t.Errorf("a contact with no account is written as %v, want none", got)
	}
}

// TestContactAccountsNameTheirCrewletKey holds each field's doc comment, as
// the generated CRD carries it to every reader of the API, to the key the
// render writes the field under: the rule that every typed field names the
// one Crewlet field it maps onto, for the fields whose Crewlet name says
// something other than the field's own.
func TestContactAccountsNameTheirCrewletKey(t *testing.T) {
	path := filepath.Join("..", "..", "config", "crd", "bases", "crewlet.ai_seats.yaml")
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var properties map[string]apiextensionsv1.JSONSchemaProps
	for _, v := range crd.Spec.Versions {
		if v.Name == v1alpha1.GroupVersion.Version {
			properties = v.Schema.OpenAPIV3Schema.Properties["spec"].Properties["contact"].Properties
		}
	}
	for _, a := range contactAccounts {
		description := strings.Join(strings.Fields(properties[a.field].Description), " ")
		if want := "Crewlet's `" + a.key + "`"; !strings.Contains(description, want) {
			t.Errorf("spec.contact.%s: the description does not say %s: %q", a.field, want, description)
		}
	}
}

// TestContactKeysAreTheEngines holds the keys the render writes a contact
// under to the engine's schema of a role's contact, in both directions: a
// key Crewlet adds fails here until SeatContact has a field for it, or a
// reason not to beside this test, and a key it drops fails here rather than
// refusing every Human seat that sets the field.
func TestContactKeysAreTheEngines(t *testing.T) {
	schema := engineSchema(t, tierCompany)
	defs, _ := schema["$defs"].(map[string]any)
	role, _ := defs["Role"].(map[string]any)
	roleProperties, _ := role["properties"].(map[string]any)
	field, _ := roleProperties["contact"].(map[string]any)
	ref, _ := field["$ref"].(string)
	def, ok := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
	if !ok {
		t.Fatalf("the engine's schema has no definition of a role's contact (Role.contact is %v)", field)
	}
	properties, _ := def["properties"].(map[string]any)
	engineKeys := slices.Sorted(maps.Keys(properties))

	keys := make([]string, 0, len(contactAccounts))
	for _, a := range contactAccounts {
		keys = append(keys, a.key)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, engineKeys) {
		t.Errorf("the engine's role contact has the keys %v; SeatContact writes %v", engineKeys, keys)
	}
}

// TestContractContactAccountsCompareAsTheEngine holds the render's
// comparison of two seats' contact accounts to the engine's, field by field:
// two values that differ only in case are one account to the render exactly
// where the engine refuses a document naming them on two seats.
func TestContractContactAccountsCompareAsTheEngine(t *testing.T) {
	client := enginetest.Start(t).Client()
	const value, twinName = janeAccount, "Jane Two"
	other := strings.ToUpper(value)
	for _, a := range contactAccounts {
		t.Run(a.field, func(t *testing.T) {
			in := companyCaseNamed(t, sampleCompany).input(t)
			jane := seat(in, seatJane)
			jane.Spec.Contact = v1alpha1.SeatContact{}
			setContact(&jane.Spec.Contact, a.field, value)
			base, err := Company(in)
			if err != nil {
				t.Fatalf("the company with one claim is refused: %v", err)
			}

			twin := jane.DeepCopy()
			twin.Name = seatJane + "-2"
			twin.Spec.DisplayName = twinName
			twin.Spec.Manages = nil
			twin.Spec.Contact = v1alpha1.SeatContact{}
			setContact(&twin.Spec.Contact, a.field, other)
			in.Seats = append(in.Seats, *twin)
			_, err = Company(in)
			refusals := RefErrors(err)
			renderRefuses := slices.ContainsFunc(refusals, func(r *RefError) bool {
				return r.Name == twin.Name && r.Field == "spec.contact."+a.field && r.Reason == v1alpha1.ReasonConflict
			})
			if err != nil && !renderRefuses {
				t.Fatalf("the render refuses the company for something else: %v", err)
			}

			var doc map[string]any
			if err := yaml.Unmarshal(base.YAML, &doc); err != nil {
				t.Fatal(err)
			}
			role := roleIn(doc, seatJane)
			if role == nil {
				t.Fatalf("the document has no role %s", seatJane)
			}
			copied := maps.Clone(role)
			delete(copied, "manages")
			copied["handle"], copied[keyName] = twin.Name, twinName
			copied["contact"] = map[string]any{a.key: other}
			doc["roles"] = append(doc["roles"].([]any), copied)
			written, err := yaml.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ApplyConfig(t.Context(), engine.ApplyRequest{Document: written, DryRun: true})
			verr, refused := errors.AsType[*engine.ValidationError](err)
			if err != nil && !refused {
				t.Fatalf("the engine's dry run answered %v", err)
			}
			if refused && !slices.ContainsFunc(verr.Problems, func(p engine.Problem) bool {
				return strings.Contains(p.Message, a.key)
			}) {
				t.Fatalf("the engine refuses the document for something else: %v", verr)
			}
			if renderRefuses != refused {
				t.Errorf("%s %q and %q on two seats: the render refuses the company: %v; the engine refuses the "+
					"document: %v", a.key, value, other, renderRefuses, refused)
			}
		})
	}
}

// The addresses the Atlassian tests name a product at, besides those of
// Atlassian's own Clouds: a site's id, and a Data Center instance of each
// product.
const (
	atlassianSiteID = "a436116f-02ce-4520-8fbb-7301462a1674"
	jiraDataCenter  = "https://jira.helix.example"
	wikiDataCenter  = "https://wiki.helix.example"
)

// atlassianAddress is an address a Jira or Confluence Integration of the
// company case caseAtlassian may name its product at, and whether Crewlet
// reads the product there on Atlassian Cloud in a company that declares no
// deployment, as every company the operator renders. The render's report on
// a person's Atlassian account is held to it
// (TestAtlassianAccountIsReportedWhereCloudNamesPeopleByID), and so is the
// engine's own reading of the document the operator renders
// (TestContractAtlassianDeploymentIsTheEngines), so the two agree through it.
type atlassianAddress struct {
	name string
	// vendor is VendorJira or VendorConfluence, and url and cloudID are its
	// Integration's: with neither, the company's Atlassian Integration
	// discovers the site.
	vendor, url, cloudID string
	cloud                bool
}

// atlassianAddresses are the addresses the tests hold the render and the
// engine to: every way Crewlet's rule (atlassianhost.Of) reads a host as
// Atlassian Cloud's own, and every near miss it reads as Data Center.
var atlassianAddresses = []atlassianAddress{
	{name: "Jira on a Data Center instance", vendor: VendorJira, url: jiraDataCenter},
	{name: "Confluence on a Data Center instance", vendor: VendorConfluence, url: wikiDataCenter},
	{name: "Jira on the site the organization discovers", vendor: VendorJira, cloud: true},
	{name: "Confluence on the site the organization discovers", vendor: VendorConfluence, cloud: true},
	{name: "Jira by cloudID", vendor: VendorJira, cloudID: atlassianSiteID, cloud: true},
	{name: "Confluence by cloudID", vendor: VendorConfluence, cloudID: atlassianSiteID, cloud: true},
	{name: "Jira on a Cloud site", vendor: VendorJira, url: "https://helix.atlassian.net", cloud: true},
	{name: "Jira on a Cloud site written in capitals", vendor: VendorJira, url: "https://HELIX.Atlassian.NET/", cloud: true},
	{name: "Jira on a Cloud site named absolutely", vendor: VendorJira, url: "https://helix.atlassian.net./", cloud: true},
	{name: "Jira on a jira.com site", vendor: VendorJira, url: "https://helix.jira.com", cloud: true},
	{name: "Jira on a Government Cloud site", vendor: VendorJira, url: "https://helix.atlassian-us-gov-mod.net", cloud: true},
	{
		name: "Jira at the API gateway", vendor: VendorJira, cloud: true,
		url: "https://api.atlassian.com/ex/jira/" + atlassianSiteID,
	},
	{name: "Confluence on a Cloud site", vendor: VendorConfluence, url: "https://helix.atlassian.net/wiki", cloud: true},
	{name: "Confluence on a jira.com site", vendor: VendorConfluence, url: "https://helix.jira.com/wiki", cloud: true},
	{
		name: "Confluence at the API gateway", vendor: VendorConfluence, cloud: true,
		url: "https://api.atlassian.com/ex/confluence/" + atlassianSiteID,
	},
	{
		name: "Confluence at Government Cloud's gateway", vendor: VendorConfluence, cloud: true,
		url: "https://api.atlassian-us-gov-mod.com/ex/confluence/" + atlassianSiteID,
	},
	// The gateway is Cloud whatever path follows it, another product's
	// included: what it does not serve is a 404, not a Data Center.
	{
		name: "Confluence at the gateway's Jira path", vendor: VendorConfluence, cloud: true,
		url: "https://api.atlassian.com/ex/jira/" + atlassianSiteID,
	},
	// No customer's site is served under atlassian.com: Atlassian's own
	// Jira there is Server, and only the gateway's own host is Cloud.
	{name: "Jira under atlassian.com", vendor: VendorJira, url: "https://jira.atlassian.com"},
	{name: "Confluence under atlassian.com", vendor: VendorConfluence, url: "https://wiki.atlassian.com"},
	{name: "Jira under the gateway's host", vendor: VendorJira, url: "https://eu.api.atlassian.com/ex/jira/" + atlassianSiteID},
	{name: "Jira under Government Cloud's own domain", vendor: VendorJira, url: "https://jira.atlassian-us-gov-mod.com"},
	{name: "Jira at a host ending in a site domain's letters", vendor: VendorJira, url: "https://helixatlassian.net"},
	// The host decides, never the path: the gateway's path on a Data
	// Center instance's host is that instance's.
	{
		name: "Confluence at the gateway's path on another host", vendor: VendorConfluence,
		url: wikiDataCenter + "/api.atlassian.com/ex/confluence/" + atlassianSiteID,
	},
	// An Isolated Cloud site is at a base domain of its own, which only a
	// declared deployment places, and the operator declares none.
	{name: "Jira at a base domain of its own", vendor: VendorJira, url: "https://helix.isolated.example"},
	{name: "Confluence at a base domain of its own", vendor: VendorConfluence, url: "https://helix.isolated.example/wiki"},
}

// atlassianIntegration names the Integration of caseAtlassian that connects
// each product.
var atlassianIntegration = map[string]string{VendorJira: caseAtlassianJira, VendorConfluence: caseAtlassianWiki}

// placeAtlassian connects a's product at a's address.
func placeAtlassian(in *Input, a atlassianAddress) {
	spec := &integration(in, atlassianIntegration[a.vendor]).Spec
	switch a.vendor {
	case VendorJira:
		spec.Jira.URL, spec.Jira.CloudID = a.url, a.cloudID
	case VendorConfluence:
		spec.Confluence.URL, spec.Confluence.CloudID = a.url, a.cloudID
	default:
		panic("no Atlassian product is connected by " + a.vendor)
	}
}

// TestAtlassianAccountIsReportedWhereCloudNamesPeopleByID holds the render's
// report on a person's Atlassian account to the deployment each product is
// on, as Crewlet derives it from the address it reads the product at, by
// one rule for both (atlassianhost.Of) in a company that declares no
// deployment: a value outside the form of an account id is reported, naming
// each product on Cloud, and nothing is reported where both are on Data
// Center, whose usernames take almost any form, or where neither is
// connected.
func TestAtlassianAccountIsReportedWhereCloudNamesPeopleByID(t *testing.T) {
	const (
		email       = "lena@helix.example"
		username    = "lena.ortiz"
		jiraCloud   = "Jira (Integration " + caseAtlassianJira + ")"
		wikiCloud   = "Confluence (Integration " + caseAtlassianWiki + ")"
		legacyCloud = "5b10ac8d82e05b22cc7d4ef5"
	)
	onCloud := map[string]string{VendorJira: jiraCloud, VendorConfluence: wikiCloud}
	// onDataCenter puts both products on Data Center instances; an address
	// case then moves one to an address of its own.
	onDataCenter := func(in *Input) {
		integration(in, caseAtlassianJira).Spec.Jira.URL = jiraDataCenter
		integration(in, caseAtlassianWiki).Spec.Confluence.URL = wikiDataCenter
	}
	type reportCase struct {
		name, account string
		// mutate moves the company's Jira and Confluence, and want is the
		// products the report names on Cloud, none when there is no report.
		mutate func(*Input)
		want   []string
	}
	accounts := []reportCase{
		{name: "an account id on sites the organization discovers", account: lenaAtlassianCloudID},
		{name: "a legacy account id on Cloud", account: legacyCloud},
		{name: "an account id of 128 characters on Cloud", account: strings.Repeat("a", 128)},
		{name: "an email address on sites the organization discovers", account: email, want: []string{jiraCloud, wikiCloud}},
		{name: "a Data Center username on Cloud", account: username, want: []string{jiraCloud, wikiCloud}},
		{name: "a value past 128 characters on Cloud", account: strings.Repeat("a", 129), want: []string{jiraCloud, wikiCloud}},
		{name: "an address for a username on Data Center", account: email, mutate: onDataCenter},
		// Only the Integration that sorts first by name writes the block a
		// company has one of per vendor; a second one is a Conflict.
		{name: "a username with a second Jira Integration on Cloud", account: username,
			mutate: func(in *Input) {
				onDataCenter(in)
				second := integration(in, caseAtlassianJira).DeepCopy()
				second.Name, second.Spec.Jira.URL = "jira-cloud", "https://helix.atlassian.net"
				in.Integrations = append(in.Integrations, *second)
			}},
		{name: "an email address with neither product connected", account: email,
			mutate: func(in *Input) {
				in.Integrations = slices.DeleteFunc(in.Integrations, func(i v1alpha1.Integration) bool {
					return i.Name == caseAtlassianJira || i.Name == caseAtlassianWiki
				})
			}},
	}
	addresses := make([]reportCase, 0, len(atlassianAddresses))
	for _, a := range atlassianAddresses {
		c := reportCase{name: "a username with " + a.name, account: username,
			mutate: func(in *Input) { onDataCenter(in); placeAtlassian(in, a) }}
		if a.cloud {
			c.want = []string{onCloud[a.vendor]}
		}
		addresses = append(addresses, c)
	}
	for _, c := range slices.Concat(accounts, addresses) {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, caseAtlassian).input(t)
			if c.mutate != nil {
				c.mutate(in)
			}
			seat(in, seatLena).Spec.Contact.AtlassianAccount = c.account
			var reported []*RefError
			for _, e := range ReportedOf(in) {
				if e.Reason == v1alpha1.ReasonNotAnAccountID {
					reported = append(reported, e)
				}
			}
			if len(c.want) == 0 {
				if len(reported) != 0 {
					t.Fatalf("%q is reported: %v", c.account, reported)
				}
				return
			}
			if len(reported) != 1 {
				t.Fatalf("%q is reported %d times, want once: %v", c.account, len(reported), reported)
			}
			e := reported[0]
			if e.Kind != KindSeat || e.Name != seatLena || e.Field != "spec.contact."+contactAtlassian || e.Basis != Reported {
				t.Errorf("the report is %v on %s %s %s, want Reported on Seat %s spec.contact.%s",
					e.Basis, e.Kind, e.Name, e.Field, seatLena, contactAtlassian)
			}
			for _, product := range []string{jiraCloud, wikiCloud} {
				if named := strings.Contains(e.Message, product); named != slices.Contains(c.want, product) {
					t.Errorf("the report names %s: %v, want %v: %s", product, named, !named, e.Message)
				}
			}
			if !strings.Contains(e.Message, strconv.Quote(c.account)) {
				t.Errorf("the report does not quote the value: %s", e.Message)
			}
		})
	}
}

// TestContractAtlassianDeploymentIsTheEngines holds atlassianAddresses, to
// which the render's report is held, to the engine's own reading of each
// address in the document the operator renders, at the one place the engine
// shows it without connecting to the product: its validation requires the
// webhook_secret of a Jira or Confluence on Data Center alone, and Crewlet's
// atlassianDeployment decides which by the one rule (atlassianhost.Of) and
// the company's declaration, the two every client the engine builds is
// handed. So the product under test is connected with no webhookSecretRef,
// and the engine refuses the document for that product's webhook_secret
// exactly where it reads the address as Data Center. It holds what no
// fingerprint in mirrored can: that the document the operator renders
// declares no deployment, and reaches the rule with each address as the
// Integration wrote it.
func TestContractAtlassianDeploymentIsTheEngines(t *testing.T) {
	client := enginetest.Start(t).Client()
	for _, a := range atlassianAddresses {
		t.Run(a.name, func(t *testing.T) {
			in := companyCaseNamed(t, caseAtlassian).input(t)
			placeAtlassian(in, a)
			spec := &integration(in, atlassianIntegration[a.vendor]).Spec
			spec.Jira.WebhookSecretRef, spec.Confluence.WebhookSecretRef = v1alpha1.SecretKeyReference{}, v1alpha1.SecretKeyReference{}
			doc, err := Company(in)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ApplyConfig(t.Context(), engine.ApplyRequest{Document: doc.YAML, DryRun: true})
			verr, refused := errors.AsType[*engine.ValidationError](err)
			if err != nil && !refused {
				t.Fatalf("the engine's dry run answered %v", err)
			}
			secret := "integrations." + a.vendor + ".webhook_secret"
			if refused {
				for _, p := range verr.Problems {
					if p.Path != secret {
						t.Fatalf("the engine refuses the document at %s, not %s: %s", p.Path, secret, p.Message)
					}
				}
			}
			if dataCenter := refused; dataCenter == a.cloud {
				t.Errorf("the engine reads the address on Data Center: %v, and atlassianAddresses, to which the "+
					"render's report is held, on Cloud: %v", dataCenter, a.cloud)
			}
		})
	}
}

// TestAtlassianDeploymentFollowsTheEngineSource holds the Clouds the render
// reads a Jira or Confluence address on as Atlassian Cloud's own to
// Crewlet's atlassianhost.environments, which
// TestMirroredEngineFunctionsAreUnchanged does not: it holds the functions
// that read the table, not the table. It holds the table's type too, whose
// every field a literal may leave out: a field Crewlet adds to an
// environment is one more thing that may place a host, which the render
// copies nothing of until somebody reads it.
func TestAtlassianDeploymentFollowsTheEngineSource(t *testing.T) {
	source := openSource(t, enginetest.Source(t))
	src, err := source.ReadFile(atlassianHostGo)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := structFields(t, atlassianHostGo, src, "environment"), []string{"gateway", "sites"}; !slices.Equal(got, want) {
		t.Fatalf("an environment of the engine's has the fields %v, the render's %v", got, want)
	}
	got := engineEnvironments(t, src)
	if !slices.EqualFunc(got, atlassianEnvironments, func(a, b atlassianEnvironment) bool {
		return a.gateway == b.gateway && slices.Equal(a.sites, b.sites)
	}) {
		t.Errorf("the engine reads an address on %+v as Atlassian Cloud, the render on %+v", got, atlassianEnvironments)
	}
}

// engineEnvironments is the environments table declared in src, the source
// of Crewlet's atlassianhost: each element a literal naming its fields.
func engineEnvironments(t *testing.T, src []byte) []atlassianEnvironment {
	t.Helper()
	const name = "environments"
	list, ok := declared(t, atlassianHostGo, src, token.VAR, name).(*ast.CompositeLit)
	if !ok {
		t.Fatalf("%s in %s is no literal", name, atlassianHostGo)
	}
	out := make([]atlassianEnvironment, 0, len(list.Elts))
	for _, elt := range list.Elts {
		lit, ok := elt.(*ast.CompositeLit)
		if !ok {
			t.Fatalf("an element of %s in %s is no literal", name, atlassianHostGo)
		}
		var env atlassianEnvironment
		for _, field := range lit.Elts {
			kv, ok := field.(*ast.KeyValueExpr)
			if !ok {
				t.Fatalf("an element of %s in %s does not name its fields", name, atlassianHostGo)
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				t.Fatalf("an element of %s in %s does not name its fields", name, atlassianHostGo)
			}
			switch key.Name {
			case "gateway":
				env.gateway = stringLiteral(t, atlassianHostGo, name, kv.Value)
			case "sites":
				env.sites = stringLiterals(t, atlassianHostGo, name, kv.Value)
			default:
				t.Fatalf("an element of %s in %s sets %s, which the render has no copy of", name, atlassianHostGo, key.Name)
			}
		}
		out = append(out, env)
	}
	return out
}

// structFields is the names of the fields of the struct type name declares
// in the Go source src of file, in their order.
func structFields(t *testing.T, file string, src []byte, name string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typ, ok := spec.(*ast.TypeSpec)
			if !ok || typ.Name.Name != name {
				continue
			}
			st, ok := typ.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("%s in %s is no struct", name, file)
			}
			var fields []string
			for _, f := range st.Fields.List {
				if len(f.Names) == 0 {
					t.Fatalf("%s in %s embeds a field", name, file)
				}
				for _, n := range f.Names {
					fields = append(fields, n.Name)
				}
			}
			return fields
		}
	}
	t.Fatalf("%s declares no type %s", file, name)
	return nil
}
