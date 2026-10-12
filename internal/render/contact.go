package render

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// A Human seat's spec.contact: the accounts it renders to Crewlet's role
// contact, how two seats' claims of one are compared, and what is reported
// of an Atlassian account the company's Jira or Confluence cannot name the
// person by.

// contactAccount is one field of a Seat's spec.contact: the account it
// names, the key of Crewlet's role contact it is written under, and whether
// Crewlet compares it without case.
type contactAccount struct {
	field, key string
	// fold is set where Crewlet lowercases the value before it routes on it
	// or compares it with another seat's: the accounts of a platform that
	// compares them without case. Every other value Crewlet takes exactly
	// as written, trimmed, so two that differ in case are two accounts.
	fold  bool
	value func(*v1alpha1.SeatContact) string
}

// claim is the account as Crewlet compares two seats' claims of it, or ""
// for none.
func (a contactAccount) claim(c *v1alpha1.SeatContact) string {
	v := strings.TrimSpace(a.value(c))
	if a.fold {
		v = strings.ToLower(v)
	}
	return v
}

// contactAtlassian is the field of the Atlassian account, which atlassianAccounts
// reports on.
const contactAtlassian = "atlassianAccount"

// contactAccounts is every field of SeatContact, in the order of Crewlet's
// own table of them. One table writes the contact and finds the seats that
// claim one account, so neither can miss a field the other has.
var contactAccounts = []contactAccount{
	{field: "slackUserID", key: "slack_user_id", value: func(c *v1alpha1.SeatContact) string { return c.SlackUserID }},
	{field: "mattermostUsername", key: "mattermost_user_id", value: func(c *v1alpha1.SeatContact) string {
		return c.MattermostUsername
	}},
	{field: contactAtlassian, key: "atlassian_account_id", value: func(c *v1alpha1.SeatContact) string {
		return c.AtlassianAccount
	}},
	{field: "githubLogin", key: "github_login", fold: true, value: func(c *v1alpha1.SeatContact) string { return c.GitHubLogin }},
	{field: "gitlabUsername", key: "gitlab_username", fold: true, value: func(c *v1alpha1.SeatContact) string {
		return c.GitLabUsername
	}},
}

// contact is Crewlet's role contact for c, keyed as contactAccounts says, or
// nil when c names no account.
func contact(c *v1alpha1.SeatContact) map[string]string {
	var doc map[string]string
	for _, a := range contactAccounts {
		if v := a.value(c); v != "" {
			if doc == nil {
				doc = map[string]string{}
			}
			doc[a.key] = v
		}
	}
	return doc
}

// cloudAccountID is the form of an Atlassian Cloud account id, the
// AccountId of Atlassian's user management API: letters, digits, '_', '|',
// ':' and '-', at most 128 characters. Nothing else names a person on
// Cloud.
var cloudAccountID = regexp.MustCompile(`^[A-Za-z0-9_|:-]{1,128}$`)

// atlassianAccounts reports each Seat whose spec.contact.atlassianAccount
// cannot be an account id while the company reaches Jira or Confluence on
// Atlassian Cloud. Crewlet registers the one value as the person's identity
// on both products, and routes an event by the identity its webhook names
// the actor by: the account id on Cloud, the username on Data Center. An
// email address, the value people reach for, or a Data Center username
// matches nothing on Cloud, so nothing the person does there reaches the
// seat as theirs and an agent's mention of them names nobody.
//
// It is reported, never held: Crewlet applies any value, and only the
// person's seat goes unrecognized (ADR-0005). The API takes any value
// without whitespace, because which deployment the value is for is a fact
// about the company's Integrations rather than the Seat.
func (r *renderer) atlassianAccounts() {
	cloud := r.atlassianCloud()
	if len(cloud) == 0 {
		return
	}
	for _, s := range sortedByName(r.in.Seats) {
		v := strings.TrimSpace(s.Spec.Contact.AtlassianAccount)
		if v == "" || cloudAccountID.MatchString(v) {
			continue
		}
		r.errs.add(Reported, KindSeat, s.Name, "spec.contact."+contactAtlassian, v1alpha1.ReasonNotAnAccountID, fmt.Sprintf(
			"is %q, which is no Atlassian account id, and Company %s reaches %s on Atlassian Cloud, which names a person "+
				"by account id alone: nothing the person does there reaches this seat as theirs, and an agent's mention "+
				"of them names nobody. Give the person's account id, such as 557058:f58131cb-b67d-43c7-b30d-6b58d40bd077, "+
				"the accountId Jira's GET /rest/api/3/myself answers when called as them",
			v, r.in.Company.Name, strings.Join(cloud, " and ")))
	}
}

// atlassianCloud names each product the company reaches on Atlassian Cloud,
// with the Integration that connects it, such as "Jira (Integration jira)".
func (r *renderer) atlassianCloud() []string {
	var out []string
	if in := r.vendorBlock(VendorJira); in != nil && jiraOnCloud(&in.Spec.Jira) {
		out = append(out, "Jira (Integration "+in.Name+")")
	}
	if in := r.vendorBlock(VendorConfluence); in != nil && confluenceOnCloud(&in.Spec.Confluence) {
		out = append(out, "Confluence (Integration "+in.Name+")")
	}
	return out
}

// vendorBlock is the Integration that writes vendor's block of the
// document: the one that sorts first by name, as integrations keeps it.
func (r *renderer) vendorBlock(vendor string) *v1alpha1.Integration {
	for _, in := range sortedByName(r.in.Integrations) {
		if Vendor(in) == vendor {
			return in
		}
	}
	return nil
}

// jiraCloudHosts are the domains Crewlet's jira.DeploymentOf reads a Jira
// address under as Atlassian Cloud (its cloudHosts). The API gateway a
// cloud_id is read through, api.atlassian.com, is one of them.
var jiraCloudHosts = []string{".atlassian.net", ".atlassian.com", ".jira.com"}

// jiraOnCloud is the deployment Crewlet's jira.DeploymentOf derives from
// the address the engine reads Jira at: for a url, Cloud when its host is
// under one of jiraCloudHosts and Data Center otherwise. Without a url (the
// API refuses one beside a cloudID) the address is on Cloud: the API
// gateway for a cloudID, or with neither the site the company's Atlassian
// Integration discovers, which only Cloud has (without one, Crewlet refuses
// the document).
func jiraOnCloud(j *v1alpha1.JiraIntegration) bool {
	return j.URL == "" || underHost(j.URL, jiraCloudHosts)
}

// confluenceCloudHosts are the domains Crewlet's confluence.isCloudSite
// reads a Confluence address under as Atlassian Cloud, and
// confluenceGateway the path of Atlassian's API gateway for Confluence,
// which its confluence.DeploymentOf reads as Cloud wherever it appears.
var confluenceCloudHosts = []string{".atlassian.net", ".jira.com"}

const confluenceGateway = "api.atlassian.com/ex/confluence"

// confluenceOnCloud is the deployment Crewlet's confluence.DeploymentOf
// derives from the address the engine reads Confluence at, as jiraOnCloud
// is Jira's, by Confluence's own list of hosts and its gateway's path.
func confluenceOnCloud(c *v1alpha1.ConfluenceIntegration) bool {
	return c.URL == "" || underHost(c.URL, confluenceCloudHosts) ||
		strings.Contains(strings.TrimSpace(c.URL), confluenceGateway)
}

// underHost reports whether the host of the address raw, without case, ends
// in one of suffixes. An address that does not parse is under none.
func underHost(raw string, suffixes []string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, suffix := range suffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}
