package render

import (
	"errors"
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

// contactNotOffered are the keys of Crewlet's role contact no field of
// SeatContact writes, each with why. SeatContact's doc comment says the same
// to whoever reads the API.
var contactNotOffered = map[string]string{
	"crewlet_operator_id": "it binds one of the engine's API tokens to the person, and the engines the operator " +
		"runs hold two, neither of them one person's: the operator's own and the one every consumer of the " +
		"company is handed",
}

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
// under, and the ones it offers no field for, to the engine's schema of a
// role's contact, in both directions: a key Crewlet adds fails here until
// SeatContact has a field for it or contactNotOffered says why not, and a
// key it drops fails here rather than refusing every Human seat that sets
// the field.
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

	var keys []string
	for _, a := range contactAccounts {
		if _, ok := contactNotOffered[a.key]; ok {
			t.Errorf("%s is written by spec.contact.%s and listed as not offered", a.key, a.field)
		}
		keys = append(keys, a.key)
	}
	keys = slices.Concat(keys, slices.Collect(maps.Keys(contactNotOffered)))
	slices.Sort(keys)
	if !slices.Equal(keys, engineKeys) {
		t.Errorf("the engine's role contact has the keys %v; SeatContact writes or declines %v", engineKeys, keys)
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

// TestAtlassianAccountIsReportedWhereCloudNamesPeopleByID holds the render's
// report on a person's Atlassian account to the deployment each product is
// on, as Crewlet derives it from the address it reads the product at
// (jira.DeploymentOf, confluence.DeploymentOf): a value outside the form of
// an account id is reported, naming each product on Cloud, and nothing is
// reported where both are on Data Center, whose usernames take almost any
// form, or where neither is connected.
func TestAtlassianAccountIsReportedWhereCloudNamesPeopleByID(t *testing.T) {
	const (
		email       = "lena@helix.example"
		username    = "lena.ortiz"
		siteID      = "a436116f-02ce-4520-8fbb-7301462a1674"
		jiraCloud   = "Jira (Integration " + caseAtlassianJira + ")"
		wikiCloud   = "Confluence (Integration " + caseAtlassianWiki + ")"
		jiraDC      = "https://jira.helix.example"
		wikiDC      = "https://wiki.helix.example"
		legacyCloud = "5b10ac8d82e05b22cc7d4ef5"
	)
	jira := func(in *Input) *v1alpha1.JiraIntegration { return &integration(in, caseAtlassianJira).Spec.Jira }
	wiki := func(in *Input) *v1alpha1.ConfluenceIntegration {
		return &integration(in, caseAtlassianWiki).Spec.Confluence
	}
	// onDataCenter puts both products on Data Center instances; each case
	// below then moves one back to Cloud by an address of its own.
	onDataCenter := func(in *Input) {
		jira(in).URL, wiki(in).URL = jiraDC, wikiDC
	}
	for _, c := range []struct {
		name, account string
		// mutate moves the company's Jira and Confluence, and want is the
		// products the report names on Cloud, none when there is no report.
		mutate func(*Input)
		want   []string
	}{
		{name: "an account id on sites the organization discovers", account: lenaAtlassianCloudID},
		{name: "a legacy account id on Cloud", account: legacyCloud},
		{name: "an account id of 128 characters on Cloud", account: strings.Repeat("a", 128)},
		{name: "an email address on sites the organization discovers", account: email, want: []string{jiraCloud, wikiCloud}},
		{name: "a Data Center username on Cloud", account: username, want: []string{jiraCloud, wikiCloud}},
		{name: "a value past 128 characters on Cloud", account: strings.Repeat("a", 129), want: []string{jiraCloud, wikiCloud}},
		{name: "a username on Data Center", account: username, mutate: onDataCenter},
		{name: "an address for a username on Data Center", account: email, mutate: onDataCenter},
		{name: "a username with Jira on a Cloud site", account: username, want: []string{jiraCloud},
			mutate: func(in *Input) { onDataCenter(in); jira(in).URL = "https://helix.atlassian.net" }},
		{name: "a username with Jira on a Cloud site written in capitals", account: username, want: []string{jiraCloud},
			mutate: func(in *Input) { onDataCenter(in); jira(in).URL = "https://HELIX.Atlassian.NET/" }},
		{name: "a username with Jira on a jira.com site", account: username, want: []string{jiraCloud},
			mutate: func(in *Input) { onDataCenter(in); jira(in).URL = "https://helix.jira.com" }},
		{name: "a username with Jira at the API gateway", account: username, want: []string{jiraCloud},
			mutate: func(in *Input) { onDataCenter(in); jira(in).URL = "https://api.atlassian.com/ex/jira/" + siteID }},
		{name: "a username with Jira by cloudID", account: username, want: []string{jiraCloud},
			mutate: func(in *Input) { onDataCenter(in); jira(in).URL, jira(in).CloudID = "", siteID }},
		{name: "a username with Confluence on a Cloud site", account: username, want: []string{wikiCloud},
			mutate: func(in *Input) { onDataCenter(in); wiki(in).URL = "https://helix.atlassian.net/wiki" }},
		{name: "a username with Confluence at the API gateway", account: username, want: []string{wikiCloud},
			mutate: func(in *Input) {
				onDataCenter(in)
				wiki(in).URL = "https://api.atlassian.com/ex/confluence/" + siteID
			}},
		{name: "a username with Confluence by cloudID", account: username, want: []string{wikiCloud},
			mutate: func(in *Input) { onDataCenter(in); wiki(in).URL, wiki(in).CloudID = "", siteID }},
		// Crewlet's Confluence knows fewer Cloud hosts than its Jira: an
		// instance under atlassian.com is Data Center to it.
		{name: "a username with Confluence under atlassian.com", account: username,
			mutate: func(in *Input) { onDataCenter(in); wiki(in).URL = "https://wiki.atlassian.com" }},
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
	} {
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

// TestAtlassianDeploymentFollowsTheEngineSource holds the hosts the render
// reads a Jira address under as Atlassian Cloud to Crewlet's jira.cloudHosts,
// which TestMirroredEngineFunctionsAreUnchanged does not: it holds the
// functions that read the list, not the list.
func TestAtlassianDeploymentFollowsTheEngineSource(t *testing.T) {
	source := openSource(t, enginetest.Source(t))
	src, err := source.ReadFile(jiraClient)
	if err != nil {
		t.Fatal(err)
	}
	if got := stringSliceVar(t, jiraClient, src, "cloudHosts"); !slices.Equal(got, jiraCloudHosts) {
		t.Errorf("the engine reads Jira under %v as Atlassian Cloud, the render under %v", got, jiraCloudHosts)
	}
}
