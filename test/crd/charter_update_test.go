package crd_test

import (
	"strings"
	"testing"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	celvalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// TestCompanyCharterIsJudgedOnItsChange: through the API server, an update
// that leaves the charter alone, or edits it inside its bounds, is admitted,
// and one that edits a part out of them is refused naming the rule.
func TestCompanyCharterIsJudgedOnItsChange(t *testing.T) {
	written := func(c *v1.Company) {
		c.Spec.Mission = "Help friends split costs fairly."
		c.Spec.Policies = []string{"Every amount is exact.", boardPolicy}
	}
	runUpdateCases(t, company, []updateCase[*v1.Company]{
		{name: "an unrelated field changes", before: written,
			mutate: func(c *v1.Company) { c.Spec.TimeZone = "Europe/Berlin" }},
		{name: "the policies are reordered and one removed", before: written,
			mutate: func(c *v1.Company) { c.Spec.Policies = []string{boardPolicy} }},
		{name: "the mission is edited onto two lines", before: written,
			mutate: func(c *v1.Company) { c.Spec.Mission = "Split costs\nfairly." }, want: msgMissionLine},
		{name: "a policy is added past its bound", before: written,
			mutate: func(c *v1.Company) {
				c.Spec.Policies = append(c.Spec.Policies, strings.Repeat("p", 1025))
			}, want: msgPolicyLength},
	})
}

// charterValidator runs the CEL rules of one charter field of the Company the
// suite installed, with the validator the API server runs, from an old value
// to a new one; a nil old is a create. It is how an update is judged from an
// old value these rules would refuse today: a Company written before them,
// which no create through the API can make now.
func charterValidator(t *testing.T, name string) func(value, old any) field.ErrorList {
	t.Helper()
	var node *apiextensionsv1.JSONSchemaProps
	for _, c := range installedCRDs {
		if c.Name != "companies.crewlet.ai" {
			continue
		}
		for _, v := range c.Spec.Versions {
			if v.Name != "v1alpha1" {
				continue
			}
			if child, ok := v.Schema.OpenAPIV3Schema.Properties["spec"].Properties[name]; ok {
				node = &child
			}
		}
	}
	if node == nil {
		t.Fatalf("the installed Company CRD has no v1alpha1 spec.%s", name)
	}
	var internal apiextensions.JSONSchemaProps
	err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(node, &internal, nil)
	if err != nil {
		t.Fatal(err)
	}
	structural, err := structuralschema.NewStructural(&internal)
	if err != nil {
		t.Fatalf("spec.%s is not a structural schema: %v", name, err)
	}
	validator := celvalidation.NewValidator(structural, false, celconfig.PerCallLimit)
	if validator == nil {
		t.Fatalf("spec.%s carries no CEL rule", name)
	}
	return func(value, old any) field.ErrorList {
		errs, _ := validator.Validate(t.Context(), field.NewPath("spec", name), structural, value, old,
			celconfig.RuntimeCELCostBudget)
		return errs
	}
}

// strs is a policies value as the API server hands a list to its validator.
func strs(values ...string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// TestAnOldCharterFaultNeverLocksACompanyOut: a Company written before the
// charter's bounds, with a mission on two lines or past its bound, a vision
// past its bound and policies past theirs, keeps applying. An update that
// leaves a faulty mission or vision as it was (the space around it aside),
// leaves a faulty policy's text as it was, reorders or removes policies, or
// adds a policy inside the bounds is admitted; one that edits a faulty part
// and leaves it at fault, adds to a count past the bound or adds a repeat is
// refused. This is the engine's own rule for what a write changes (Crewlet's
// config.Company.ValidateCharterChange), but for the policies' exact-text
// comparison the field's doc states.
func TestAnOldCharterFaultNeverLocksACompanyOut(t *testing.T) {
	mission, vision, policies := charterValidator(t, "mission"), charterValidator(t, "vision"),
		charterValidator(t, "policies")
	twoLines := "Split costs.\nFairly."
	longMission := strings.Repeat("m", 1100)
	longVision := strings.Repeat("v", 2049)
	long := strings.Repeat("x", 1025)
	stored := strs("A rule.", long, "Twice.", "Twice.")
	thirty := make([]any, 30)
	for i := range thirty {
		thirty[i] = "Rule " + strings.Repeat("r", i+1) + "."
	}
	for _, c := range []struct {
		name     string
		validate func(value, old any) field.ErrorList
		value    any
		old      any
		want     string
	}{
		{"a stored two-line mission left alone", mission, twoLines + "\n", twoLines, ""},
		{"a stored two-line mission edited, still two lines", mission, "Split costs\nfairly.", twoLines, msgMissionLine},
		{"a stored two-line mission put on one line", mission, "Split costs fairly.", twoLines, ""},
		{"a new two-line mission", mission, twoLines, nil, msgMissionLine},
		{"a stored long mission left alone", mission, longMission, longMission, ""},
		{"a stored long mission edited, still long", mission, longMission + "!", longMission, msgMissionLength},
		{"a stored long vision left alone", vision, longVision, longVision, ""},
		{"a stored long vision edited, still long", vision, longVision + "w", longVision, msgVisionLength},
		{"stored faulty policies left alone", policies, stored, stored, ""},
		{"stored faulty policies reordered", policies, strs("Twice.", long, "Twice.", "A rule."), stored, ""},
		{"a stored faulty policy removed", policies, strs("A rule.", "Twice.", "Twice."), stored, ""},
		{"a policy added beside stored faulty ones", policies,
			strs("A rule.", long, "Twice.", "Twice.", "A new rule."), stored, ""},
		{"a stored long policy edited, still long", policies,
			strs("A rule.", long+"y", "Twice.", "Twice."), stored, msgPolicyLength},
		{"a third copy of a stored repeat", policies,
			strs("A rule.", long, "Twice.", "Twice.", "Twice."), stored, msgPolicyRepeat},
		{"a repeat of a policy that was not repeated", policies,
			strs("A rule.", long, "Twice.", "Twice.", "A rule."), stored, msgPolicyRepeat},
		{"a policy removed from 30 stored", policies, thirty[1:], thirty, ""},
		{"a policy added to 30 stored", policies, append(append([]any{}, thirty...), "One more."), thirty, msgPolicyCount},
		{"a new long policy", policies, strs(long), nil, msgPolicyLength},
	} {
		t.Run(c.name, func(t *testing.T) {
			errs := c.validate(c.value, c.old)
			switch {
			case c.want == "" && len(errs) > 0:
				t.Errorf("refused: %v", errs)
			case c.want != "" && !strings.Contains(errs.ToAggregate().Error(), c.want):
				t.Errorf("= %v, want a refusal naming %q", errs, c.want)
			}
		})
	}
}
