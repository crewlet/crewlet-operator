package crd_test

import (
	"strings"
	"testing"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// charterCase is one charter the API is asked to admit: the mission, the
// vision and the policies of a Company, and the rule message it is refused
// with, empty for a charter it admits.
type charterCase struct {
	name     string
	mission  string
	vision   string
	policies []string
	want     string
}

// charterCases are the edges of every charter rule, each one character inside
// and one outside, counted in characters (an emoji is one, four bytes) on the
// value with the space around it trimmed, and every character that ends a
// line. TestCompanyCharterBounds holds the API to the messages, and
// TestCharterBoundsFollowTheEngine holds the same cases to the engine.
func charterCases() []charterCase {
	policies := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "Rule " + strings.Repeat("r", i+1) + "."
		}
		return out
	}
	cases := make([]charterCase, 0, 37)
	cases = append(cases,
		charterCase{name: "a written charter is accepted", mission: "Help friends split costs fairly.",
			vision:   "Splitting a bill is never the awkward part.\n\nIn any currency.",
			policies: []string{"Every amount is exact to the cent.", boardPolicy}},
		charterCase{name: "a mission of 1024 emoji is accepted", mission: strings.Repeat("🚀", 1024)},
		charterCase{name: "a mission of 1025 emoji is refused", mission: strings.Repeat("🚀", 1025), want: msgMissionLength},
		charterCase{name: "a mission at its bound with space around it is accepted",
			mission: "  " + strings.Repeat("m", 1024) + "\n"},
		charterCase{name: "a vision of 2048 CJK characters is accepted", vision: strings.Repeat("漢", 2048)},
		charterCase{name: "a vision of 2049 characters is refused", vision: strings.Repeat("v", 2049), want: msgVisionLength},
		charterCase{name: "a vision at its bound with space around it is accepted",
			vision: "\t" + strings.Repeat("v", 2048) + " "},
		charterCase{name: "25 policies are accepted", policies: policies(25)},
		charterCase{name: "26 policies are refused", policies: policies(26), want: msgPolicyCount},
		charterCase{name: "a policy of 1024 characters is accepted", policies: []string{strings.Repeat("p", 1024)}},
		charterCase{name: "a policy of 1025 characters is refused",
			policies: []string{strings.Repeat("p", 1025)}, want: msgPolicyLength},
		charterCase{name: "a policy at its bound with space around it is accepted",
			policies: []string{" " + strings.Repeat("p", 1024) + " "}},
		charterCase{name: "a blank policy is refused", policies: []string{"A rule.", whiteSpace}, want: msgPolicyBlank},
		charterCase{name: "a policy repeated is refused",
			policies: []string{"Say it once.", "Another.", "Say it once."}, want: msgPolicyRepeat},
		charterCase{name: "a policy with a trailing newline is accepted", policies: []string{"One rule.\n"}},
		charterCase{name: "a mission with a trailing newline is accepted", mission: "One line.\n"},
	)
	for name, lineBreak := range map[string]string{
		"a line feed": "\n", "a carriage return": "\r", "a vertical tab": "\v",
		"a form feed": "\f", "a next line": "\u0085", "a line separator": " ",
		"a paragraph separator": " ",
	} {
		cases = append(cases,
			charterCase{name: "a mission broken by " + name + " is refused",
				mission: "Split costs" + lineBreak + "fairly.", want: msgMissionLine},
			charterCase{name: "a policy broken by " + name + " is refused",
				policies: []string{"Every amount" + lineBreak + "is exact."}, want: msgPolicyLine},
			charterCase{name: "a vision broken by " + name + " is accepted",
				vision: "First" + lineBreak + "second."})
	}
	return cases
}

// boardPolicy is a policy as a company writes one.
const boardPolicy = "Work is tracked on the board."

// withCharter sets a Company's charter to c's.
func (c *charterCase) withCharter(company *v1.Company) {
	company.Spec.Mission, company.Spec.Vision, company.Spec.Policies = c.mission, c.vision, c.policies
}

// TestCompanyCharterBounds holds the API to Crewlet's bounds on a company's
// mission, vision and policies, each refused with its rule's message.
func TestCompanyCharterBounds(t *testing.T) {
	charter := charterCases()
	cases := make([]createCase[*v1.Company], 0, 1+len(charter))
	cases = append(cases, createCase[*v1.Company]{name: "a company with no charter is accepted"})
	for _, c := range charter {
		cases = append(cases, createCase[*v1.Company]{name: c.name, mutate: c.withCharter, want: c.want})
	}
	runCreateCases(t, company, cases)
}
