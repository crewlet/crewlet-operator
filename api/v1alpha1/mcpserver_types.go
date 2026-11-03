package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// MCPTransport is how the engine reaches a tool server. It maps onto
// Crewlet's mcp_servers[].transport (Stdio is `stdio`, HTTP is `http`).
// +kubebuilder:validation:Enum=Stdio;HTTP
type MCPTransport string

// The MCP transports.
const (
	// MCPTransportStdio launches the server as a process next to the engine
	// and speaks to it over its standard input and output. The command must
	// exist in the engine image.
	MCPTransportStdio MCPTransport = "Stdio"
	// MCPTransportHTTP connects to a remote server over Streamable HTTP.
	MCPTransportHTTP MCPTransport = "HTTP"
)

// MCPToolAnnotations corrects the behavioural hints one tool advertises. The
// engine decides from them what a tool may be used for, such as whether a
// delegated worker may call it. Each field maps onto Crewlet's
// tool_annotations entry; unset keeps what the server says.
type MCPToolAnnotations struct {
	// tool is the tool's name as the server publishes it, without any
	// prefix.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	Tool string `json:"tool,omitempty"`

	// readOnly marks a tool that changes nothing. Crewlet's `read_only`.
	// +optional
	ReadOnly *bool `json:"readOnly,omitempty"`

	// destructive marks a tool whose effect cannot be undone. Crewlet's
	// `destructive`.
	// +optional
	Destructive *bool `json:"destructive,omitempty"`

	// idempotent marks a tool that can be repeated safely. Crewlet's
	// `idempotent`.
	// +optional
	Idempotent *bool `json:"idempotent,omitempty"`

	// openWorld marks a tool that reaches outside the company. Crewlet's
	// `open_world`.
	// +optional
	OpenWorld *bool `json:"openWorld,omitempty"`
}

// MCPToolLevel is the level a tool of a server runs at. It maps onto Crewlet's
// tool levels (Allow is `allow`, Approve `approve`, Blocked `blocked`): Allow
// offers the tool to every agent the server reaches and runs each call, Approve
// offers it and holds each call until a person allows or denies it, and Blocked
// never offers it to an agent, which is then neither listed to nor callable by
// any of them.
// +kubebuilder:validation:Enum=Allow;Approve;Blocked
type MCPToolLevel string

// The tool levels.
const (
	MCPToolLevelAllow   MCPToolLevel = "Allow"
	MCPToolLevelApprove MCPToolLevel = "Approve"
	MCPToolLevelBlocked MCPToolLevel = "Blocked"
)

// MCPToolLevelEntry sets the level of one tool of a server. It maps onto one
// key of Crewlet's tools.levels.
type MCPToolLevelEntry struct {
	// tool is the tool's name as the server lists it, before toolPrefix:
	// the name an operator reads off the server's listing, which stays the
	// same when the prefix changes. A name holds no space and no control
	// character, as no tool name does; a well-formed name the server does
	// not list blocks nothing, and Crewlet names it on the server's page.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[^\p{Z}\p{Cc}]+$`
	Tool string `json:"tool,omitempty"`

	// level is the level the tool runs at.
	// +required
	Level MCPToolLevel `json:"level,omitempty"`
}

// MCPTools sets the level each tool of a server runs at. It maps onto
// Crewlet's tools. A tool neither default nor levels sets starts at the level
// its hints give it (toolAnnotations applied): Allow for a tool the server
// marks read-only, Approve for every other, a tool with no hints included.
//
// Crewlet never files a blocked tool, so no agent is offered, lists or calls
// it, on the engine or from an executor's sandbox through the engine's bridge,
// and it runs a call to a tool at Approve only once a person allows it: an
// executor's run parks on the person's answer, and every other phase is
// refused the call. A sandbox that lists the server in
// spec.sandbox.mcpServerRefs would reach it whole, past the engine, so a Seat
// may list only a server whose every tool runs at Allow (default Allow and no
// other level): the operator reports any other listing on the Seat and holds
// the document, as Crewlet refuses it.
// +kubebuilder:validation:MinProperties=1
type MCPTools struct {
	// default is the level of every tool levels does not name, a tool the
	// server adds later included. Unset, each such tool starts at the level
	// its hints give it: Allow for a tool the server marks read-only, Approve
	// for every other. Crewlet's `tools.default`.
	// +optional
	Default MCPToolLevel `json:"default,omitempty"`

	// levels sets one tool's level each, by the server's own name for it.
	// Crewlet's `tools.levels`. At most 256, the most tools Crewlet lists of
	// one server.
	// +optional
	// +listType=map
	// +listMapKey=tool
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=256
	Levels []MCPToolLevelEntry `json:"levels,omitempty"`
}

// MCPEnvVar is one variable of a Stdio tool server's environment, which every
// instance of the server starts with: a credential, read from a Secret, or a
// setting that is no credential, written as a literal value, such as the
// address of the service the server reaches or a switch that turns one of its
// tools on.
//
// Crewlet treats a server's whole environment as credentials, masking every
// value on every read, so it cannot tell the two apart; a name can. A name
// says it holds a credential when one of its parts, cut at every character
// that is not a letter or a digit and read without case, is KEY, KEYS,
// APIKEY, ACCESSKEY, SECRETKEY, TOKEN, SECRET, PASSWORD, PASSWD, CREDENTIAL,
// CREDENTIALS, AUTH, OAUTH or PAT, which is how Crewlet itself reads a
// credential's name (harnessprofile.IsCredentialName), or when it is one of
// the HTTP headers that carry one, Authorization, Proxy-Authorization and
// Cookie; such a name takes valueFrom, never a literal. So GITLAB_TOKEN,
// TAVILY_API_KEY and X-Api-Key come from Secrets, and JIRA_URL or
// SLACK_MCP_ADD_MESSAGE_TOOL may be written out. The rule keeps a credential
// from being typed where a setting goes; a credential under a name that says
// nothing (a URL with a password in it, say) is still yours to keep in a
// Secret.
// +kubebuilder:validation:XValidation:rule="has(self.value) != has(self.valueFrom)",message="exactly one of value and valueFrom must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.value) || !(self.name.matches('(?i)(^|[^A-Za-z0-9])(KEY|KEYS|APIKEY|ACCESSKEY|SECRETKEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|CREDENTIALS|AUTH|OAUTH|PAT)([^A-Za-z0-9]|$)') || self.name.lowerAscii() in ['authorization', 'proxy-authorization', 'cookie'])",message="value may not be given to a name that says it holds a credential, which comes from a Secret through valueFrom: one of its parts, cut at every character that is not a letter or a digit, is, in any case, KEY, KEYS, APIKEY, ACCESSKEY, SECRETKEY, TOKEN, SECRET, PASSWORD, PASSWD, CREDENTIAL, CREDENTIALS, AUTH, OAUTH or PAT, as Crewlet reads a credential's name, or it is the Authorization, Proxy-Authorization or Cookie header",fieldPath=".value"
type MCPEnvVar struct {
	// name is the variable's name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[A-Za-z_][A-Za-z0-9_]*$`
	Name string `json:"name,omitempty"`

	// value is a literal value, for a setting that is no credential. It
	// lands in the company document as written, where anyone who can read
	// this object reads it too. An empty string is a valid value, which is
	// why the field is a pointer.
	// +optional
	// +kubebuilder:validation:MaxLength=4096
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="value may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	Value *string `json:"value,omitempty"`

	// valueFrom reads a credential from a Secret key.
	// +optional
	ValueFrom SecretValueSource `json:"valueFrom,omitzero"`
}

// MCPHeader is one header of every request to an HTTP tool server, which every
// instance of the server sends: a credential, read from a Secret, such as the
// Authorization header, or a setting that is no credential, written as a
// literal value, by the rule MCPEnvVar states.
// +kubebuilder:validation:XValidation:rule="has(self.value) != has(self.valueFrom)",message="exactly one of value and valueFrom must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.value) || !(self.name.matches('(?i)(^|[^A-Za-z0-9])(KEY|KEYS|APIKEY|ACCESSKEY|SECRETKEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|CREDENTIALS|AUTH|OAUTH|PAT)([^A-Za-z0-9]|$)') || self.name.lowerAscii() in ['authorization', 'proxy-authorization', 'cookie'])",message="value may not be given to a name that says it holds a credential, which comes from a Secret through valueFrom: one of its parts, cut at every character that is not a letter or a digit, is, in any case, KEY, KEYS, APIKEY, ACCESSKEY, SECRETKEY, TOKEN, SECRET, PASSWORD, PASSWD, CREDENTIAL, CREDENTIALS, AUTH, OAUTH or PAT, as Crewlet reads a credential's name, or it is the Authorization, Proxy-Authorization or Cookie header",fieldPath=".value"
type MCPHeader struct {
	// name is the header's name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9!#$%&'*+.^_|~-]+$`
	Name string `json:"name,omitempty"`

	// value is a literal value, for a setting that is no credential. It
	// lands in the company document as written, where anyone who can read
	// this object reads it too. An empty string is a valid value, which is
	// why the field is a pointer.
	// +optional
	// +kubebuilder:validation:MaxLength=4096
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="value may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	Value *string `json:"value,omitempty"`

	// valueFrom reads a credential from a Secret key, which holds the whole
	// header value, scheme included (for example "Bearer <token>").
	// +optional
	ValueFrom SecretValueSource `json:"valueFrom,omitzero"`
}

// SeatCredentialKind is an integration whose per-seat credential a tool
// server can read. It maps onto Crewlet's seat_credentials[].kind (Atlassian
// is `atlassian`, Cloudflare `cloudflare`, Datadog `datadog`, Expo `expo`,
// GitHub `github`, GitLab `gitlab`, Mattermost `mattermost`, Slack `slack`).
// +kubebuilder:validation:Enum=Atlassian;Cloudflare;Datadog;Expo;GitHub;GitLab;Mattermost;Slack
type SeatCredentialKind string

// The seat credential kinds.
const (
	SeatCredentialAtlassian  SeatCredentialKind = "Atlassian"
	SeatCredentialCloudflare SeatCredentialKind = "Cloudflare"
	SeatCredentialDatadog    SeatCredentialKind = "Datadog"
	SeatCredentialExpo       SeatCredentialKind = "Expo"
	SeatCredentialGitHub     SeatCredentialKind = "GitHub"
	SeatCredentialGitLab     SeatCredentialKind = "GitLab"
	SeatCredentialMattermost SeatCredentialKind = "Mattermost"
	SeatCredentialSlack      SeatCredentialKind = "Slack"
)

// SeatCredentialField is which of an Atlassian account's two seat credentials
// a server reads. It maps onto Crewlet's seat_credentials[].field (Token is
// `TOKEN`, Email `EMAIL`).
// +kubebuilder:validation:Enum=Token;Email
type SeatCredentialField string

// The Atlassian seat credential fields.
const (
	SeatCredentialToken SeatCredentialField = "Token"
	SeatCredentialEmail SeatCredentialField = "Email"
)

// MCPSeatCredential is one per-seat credential a tool server reads. It maps
// onto one entry of Crewlet's seat_credentials. The value is the engine's to
// give each seat's instance; nothing here names a credential.
// +kubebuilder:validation:XValidation:rule="!has(self.field) || self.kind == 'Atlassian'",message="field applies to Atlassian, whose account holds a Token and an Email; every other integration's seat credential is one",fieldPath=".field"
type MCPSeatCredential struct {
	// kind is the integration whose seat credential the server reads.
	// Crewlet's `kind`.
	// +required
	Kind SeatCredentialKind `json:"kind,omitempty"`

	// field is which of an Atlassian account's credentials, its Token or
	// its Email. Unset is the integration's credential itself, an
	// Atlassian account's Token. Crewlet's `field`.
	// +optional
	Field SeatCredentialField `json:"field,omitempty"`

	// key is the variable (Stdio) or header (HTTP) the server reads it
	// under, which the server's transport decides. Crewlet's `key`.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9!#$%&'*+.^_|~-]+$`
	Key string `json:"key,omitempty"`

	// prefix is text the value follows, such as "Bearer " for an
	// Authorization header. Crewlet's `prefix`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="prefix may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	Prefix string `json:"prefix,omitempty"`
}

// MCPServerAccess is which agent seats a tool server reaches. It maps onto
// Crewlet's mcp_servers[].access. A seat is reached when seatRefs names it,
// or when a Unit in unitRefs holds it at any depth and exceptSeatRefs does not
// name it; seatRefs wins over exceptSeatRefs, which is why the two may not
// name one Seat. A Human seat runs no tools and is reached by nothing, so
// naming one is refused.
// +kubebuilder:validation:XValidation:rule="!has(self.seatRefs) || !has(self.exceptSeatRefs) || !self.exceptSeatRefs.exists(e, self.seatRefs.exists(s, s.name == e.name))",message="a Seat is both granted in seatRefs and left out in exceptSeatRefs: keep it in one of the two",fieldPath=".exceptSeatRefs"
type MCPServerAccess struct {
	// unitRefs name Units whose agent seats get the server: every agent
	// seat in each Unit and in every Unit beneath it, at any depth, so a
	// seat that joins the Unit later gets it too. Crewlet's `access.units`,
	// which names a Unit by its metadata.name, the Unit's key.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=64
	UnitRefs []LocalObjectReference `json:"unitRefs,omitempty"`

	// seatRefs name agent Seats that get the server, each by itself.
	// Crewlet's `access.seats`, which names a Seat by its handle.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=128
	SeatRefs []LocalObjectReference `json:"seatRefs,omitempty"`

	// exceptSeatRefs name agent Seats left out although a Unit in unitRefs
	// holds them. Crewlet's `access.except`, which names a Seat by its
	// handle.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=64
	ExceptSeatRefs []LocalObjectReference `json:"exceptSeatRefs,omitempty"`
}

// MCPOAuthGrant is how the engine is granted access to a tool server it signs
// the company in to with OAuth. It maps onto Crewlet's
// mcp_servers[].oauth.grant (AuthorizationCode is `authorization_code`,
// ClientCredentials `client_credentials`).
// +kubebuilder:validation:Enum=AuthorizationCode;ClientCredentials
type MCPOAuthGrant string

// The grants the engine is given a tool server's access by.
const (
	// MCPOAuthGrantAuthorizationCode is a person's sign-in: an administrator
	// signs the company in once from the server's page in the engine's
	// dashboard, and the engine renews the grant (OAuth's authorization code
	// grant). A server whose oauth gives no grant is signed in to this way.
	MCPOAuthGrantAuthorizationCode MCPOAuthGrant = "AuthorizationCode"
	// MCPOAuthGrantClientCredentials is a service identity: a client the
	// company registered at the vendor, which the engine signs in as with
	// its id and secret whenever it needs an access token, with no person
	// behind it (OAuth's client credentials grant).
	MCPOAuthGrantClientCredentials MCPOAuthGrant = "ClientCredentials" //nolint:gosec // G101: a grant's name, which holds no credential.
)

// MCPOAuthClient is how the engine registers at a tool server's authorization
// server for a person's sign-in. It maps onto Crewlet's
// mcp_servers[].oauth.client (CIMD is `cimd`, DCR `dcr`, Own `own`).
// +kubebuilder:validation:Enum=CIMD;DCR;Own
type MCPOAuthClient string

// The ways the engine registers at an authorization server.
const (
	// MCPOAuthClientCIMD signs in as the client the engine describes in the
	// client metadata document it publishes at its own address, which is
	// the client's id.
	MCPOAuthClientCIMD MCPOAuthClient = "CIMD"
	// MCPOAuthClientDCR registers a client at the authorization server when a
	// sign-in starts (dynamic client registration), which issues the id.
	MCPOAuthClientDCR MCPOAuthClient = "DCR"
	// MCPOAuthClientOwn signs in as a client an administrator registered at
	// the vendor beforehand, whose id clientID gives.
	MCPOAuthClientOwn MCPOAuthClient = "Own"
)

// MCPOAuth signs the company in to an HTTP tool server with OAuth: one grant
// for the whole company, on every pod that runs the server. grant says which:
// a person's sign-in, made once from the server's page in the engine's
// dashboard and renewed by the engine (AuthorizationCode, the default), or a
// service identity, the company's own client, which the engine signs in as
// with its id and secret whenever it needs an access token
// (ClientCredentials). It maps onto Crewlet's mcp_servers[].oauth.
//
// The grant itself is not here, nor anywhere the operator writes: what a
// person's sign-in is granted (its refresh token, and the client a
// registration issues) is sealed in the engine's own store and never read
// back out, and a service identity's access tokens are held in the memory of
// the pod they were issued to alone. This says only how the engine signs in.
//
// The rules below read grant as AuthorizationCode where it is unset, as
// Crewlet does, and hold each field to the grant: client for a person's
// sign-in alone, clientID and clientSecret for the company's own client,
// whichever grant signs in as it. No field is required by every grant, so an
// empty block is refused as one: it would say nothing of how to sign in.
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:XValidation:rule="(has(self.grant) && self.grant == 'ClientCredentials') || has(self.client)",message="client is required for a person's sign-in (grant AuthorizationCode, the default): set it to CIMD, DCR or Own to say how the engine registers at the authorization server, or set grant to ClientCredentials to sign in as a service identity",fieldPath=".client"
// +kubebuilder:validation:XValidation:rule="!has(self.grant) || self.grant != 'ClientCredentials' || !has(self.client)",message="client is not allowed with grant ClientCredentials: a service identity signs in as the company's own client, the one clientID names, and registers nothing. Remove client",fieldPath=".client"
// +kubebuilder:validation:XValidation:rule="!((has(self.grant) && self.grant == 'ClientCredentials') || (has(self.client) && self.client == 'Own')) || has(self.clientID)",message="clientID is required with grant ClientCredentials and with client Own: give the id the company's own client was registered under at the vendor",fieldPath=".clientID"
// +kubebuilder:validation:XValidation:rule="(has(self.grant) && self.grant == 'ClientCredentials') || !has(self.client) || self.client == 'Own' || !has(self.clientID)",message="clientID applies to client Own and to grant ClientCredentials, the company's own client: with client CIMD the id is the address of the engine's client metadata document, with DCR the one the registration issues. Remove clientID",fieldPath=".clientID"
// +kubebuilder:validation:XValidation:rule="!has(self.grant) || self.grant != 'ClientCredentials' || has(self.clientSecret)",message="clientSecret is required with grant ClientCredentials: the engine signs in as the company's own client with its id and secret. Read the client's secret from a Secret key",fieldPath=".clientSecret"
// +kubebuilder:validation:XValidation:rule="(has(self.grant) && self.grant == 'ClientCredentials') || !has(self.client) || self.client == 'Own' || !has(self.clientSecret)",message="clientSecret applies to client Own, a client registered at the vendor as confidential, and to grant ClientCredentials: with client CIMD or DCR the engine signs in as a public client, with no secret. Remove clientSecret",fieldPath=".clientSecret"
type MCPOAuth struct {
	// grant is how the engine is granted access to the server.
	// AuthorizationCode, a person's sign-in: an administrator signs the
	// company in once from the server's page in the engine's dashboard,
	// registered as client says, and the engine renews the grant.
	// ClientCredentials, a service identity: a client the company
	// registered at the vendor that signs in as itself, with no person
	// behind it. The engine signs in as it with clientID and clientSecret
	// whenever it needs an access token, so both are required, and client
	// is not allowed. Unset, it is a person's sign-in. Crewlet's `grant`.
	// +optional
	Grant MCPOAuthGrant `json:"grant,omitempty"`

	// client is how the engine registers at the server's authorization
	// server for a person's sign-in: CIMD, by the client metadata document
	// it publishes; DCR, by registering a client when a sign-in starts; or
	// Own, as a client an administrator registered at the vendor
	// beforehand. The engine's check of a server's address says which the
	// server supports. Required unless grant is ClientCredentials, and not
	// allowed with it: a service identity always signs in as the company's
	// own client, so nothing is registered. Crewlet's `client`.
	// +optional
	Client MCPOAuthClient `json:"client,omitempty"`

	// clientID is the id the company's own client was registered under at
	// the vendor: required with client Own and with grant
	// ClientCredentials, and not allowed with client CIMD or DCR, where the
	// id is the engine's or the registration's. Printable ASCII and spaces,
	// as RFC 6749 spells a client id, with no space at either end, and at
	// most 256 characters: Crewlet sets no bound, and the API server needs
	// one to cost the rules over it. Crewlet's `client_id`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:XValidation:rule="self.matches('^[!-~]([ -~]*[!-~])?$')",message="clientID is printable ASCII characters and spaces, as RFC 6749 spells a client id, with no space at either end"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="clientID may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	ClientID string `json:"clientID,omitempty"`

	// clientSecret reads the secret of the company's own client from a
	// Secret key: required with grant ClientCredentials, which signs in
	// with it; with client Own, given for a client registered at the vendor
	// as confidential and left unset for a public one; and not allowed with
	// client CIMD or DCR. Crewlet signs in with it, at every renewal of a
	// person's sign-in and at every access token a service identity is
	// issued, so it starts the server only once the value is there: a
	// Secret key that holds an empty value holds no document back, and the
	// MCPServer reports ResolvedRefs=False with SecretValueEmpty until it
	// holds one, while a Secret or key that does not exist holds the
	// document, as every reference does. Crewlet's `client_secret`, a
	// credential position.
	// +optional
	ClientSecret SecretValueSource `json:"clientSecret,omitzero"`

	// scopes are the scopes the sign-in asks for, in any order. Unset, a
	// person's sign-in asks for the ones the server's own metadata lists, or
	// none where it lists none, and a service identity asks for none, which
	// its authorization server answers with the client's own default scopes.
	// Each is one scope token as RFC 6749 spells it: printable ASCII
	// characters with no space, double quote or backslash, at most 256 of
	// them, a bound the API server needs to cost its rules and Crewlet does
	// not set. At most 50 scopes, as Crewlet bounds them. Crewlet's
	// `scopes`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=50
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=256
	// +kubebuilder:validation:XValidation:rule="self.all(s, s.matches('^[!#-\\\\[\\\\]-~]+$'))",message="each scope is one scope token as RFC 6749 spells it: printable ASCII characters with no space, double quote or backslash"
	// +kubebuilder:validation:XValidation:rule="self.all(s, !s.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]'))",message="scopes may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	Scopes []string `json:"scopes,omitempty"`
}

// MCPServerSpec is one tool server a company's agents call. It renders to one
// entry of Crewlet's mcp_servers, named by the MCPServer's metadata.name;
// Seats and Units give it per-seat values by that name.
// +kubebuilder:validation:XValidation:rule="self.transport == 'Stdio' ? has(self.command) : !has(self.command)",message="command is required with transport Stdio and not allowed with HTTP"
// +kubebuilder:validation:XValidation:rule="self.transport == 'Stdio' || (!has(self.args) && !has(self.env))",message="args and env apply to transport Stdio only"
// +kubebuilder:validation:XValidation:rule="self.transport == 'HTTP' ? has(self.url) : !has(self.url)",message="url is required with transport HTTP and not allowed with Stdio"
// +kubebuilder:validation:XValidation:rule="self.transport == 'HTTP' || !has(self.headers)",message="headers apply to transport HTTP only"
// +kubebuilder:validation:XValidation:rule="self.transport != 'HTTP' || !has(self.optionalKeys) || self.optionalKeys.all(a, self.optionalKeys.exists_one(b, b.lowerAscii() == a.lowerAscii()))",message="optionalKeys names each header once: HTTP header names are case-insensitive, so its names must differ in more than case",fieldPath=".optionalKeys"
// +kubebuilder:validation:XValidation:rule="!has(self.seatCredentials) || (has(self.shared) && !self.shared)",message="seatCredentials applies to a server with shared false: a shared server acts as no seat, so it reads no seat's credential",fieldPath=".seatCredentials"
// +kubebuilder:validation:XValidation:rule="self.transport != 'HTTP' || !has(self.seatCredentials) || self.seatCredentials.all(a, self.seatCredentials.exists_one(b, b.key.lowerAscii() == a.key.lowerAscii()))",message="seatCredentials names each header once: HTTP header names are case-insensitive, so its keys must differ in more than case",fieldPath=".seatCredentials"
// +kubebuilder:validation:XValidation:rule="!has(self.access) || !has(self.seatCredentials)",message="access does not apply to a server with seatCredentials: a server reading each agent's own vendor credential reaches the agent seats that are on at that vendor's integration (a Seat's spec.integrations.<vendor>.enabled), the one place that grant is made, so remove access and turn seats on or off there",fieldPath=".access"
// +kubebuilder:validation:XValidation:rule="!has(self.oauth) || self.transport == 'HTTP'",message="oauth signs the company in to a remote server and applies to transport HTTP only: a Stdio server is launched with its credentials in env",fieldPath=".oauth"
// +kubebuilder:validation:XValidation:rule="!has(self.oauth) || !has(self.shared) || self.shared",message="oauth is one sign-in for the whole company, and a server with shared false acts as each seat's own identity: give each seat its credential in a Seat's or a Unit's spec.mcp, or make the server shared",fieldPath=".oauth"
// +kubebuilder:validation:XValidation:rule="!has(self.oauth) || !has(self.headers) || self.headers.all(h, h.name.lowerAscii() != 'authorization')",message="headers may not hold Authorization beside oauth: the OAuth sign-in sends the Authorization header with each request. Remove the header, or remove oauth to send it instead",fieldPath=".headers"
// +kubebuilder:validation:XValidation:rule="!has(self.oauth) || !has(self.headers) || self.headers.all(h, h.name.lowerAscii() != 'oauth.client_secret')",message="headers may not hold a header named oauth.client_secret beside oauth: it is the key the engine's dashboard stores the OAuth client's secret under, and the two could not be told apart. Give the header another name",fieldPath=".headers"
type MCPServerSpec struct {
	// companyRef names the Company this server belongs to. Immutable.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="companyRef is immutable"
	CompanyRef CompanyReference `json:"companyRef,omitzero"`

	// enabled runs the server. false turns it off and keeps it: the engine
	// starts no instance of it, the company's, a seat's or a box's, and grants
	// it to no seat, while this object, its access, and every Seat's and
	// Unit's spec.mcp and spec.sandbox.mcpServerRefs that name it stay as they
	// are, held to every rule they are held to now, for the day it is turned
	// back on. A
	// value it would be started with is then read by nothing, so one whose
	// Secret key holds an empty value is no failure; a Secret or key that
	// does not exist still holds the document, as every reference does.
	// Crewlet's `enabled`.
	// +optional
	// +default=true
	Enabled *bool `json:"enabled,omitempty"`

	// description says what the server is for. Each agent's prompt lists
	// its tool servers by name before it shows any of their tools, and
	// prints this beside the name, so it is what tells an agent which server
	// to look in. One line of at most 200 characters, as Crewlet bounds it:
	// no control character, and neither of Unicode's line and paragraph
	// separators (U+2028, U+2029), which break a line too. Crewlet's
	// `description`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=200
	// +kubebuilder:validation:XValidation:rule="!self.matches('[\\\\p{Cc}\\\\p{Zl}\\\\p{Zp}]')",message="description is one line: it may hold no line break, line or paragraph separator, tab or other control character"
	Description string `json:"description,omitempty"`

	// transport is how the engine reaches the server. Crewlet's `transport`.
	// +optional
	// +default="Stdio"
	Transport MCPTransport `json:"transport,omitempty"`

	// shared launches one instance whose tools every agent seat the server
	// reaches (access) calls. false makes the server a template instead:
	// every agent seat it reaches gets an instance of its own, started with
	// the seat's values for it from its own and its unit's spec.mcp, which
	// is how each agent acts as itself on the server. A seat reached with no
	// such values gets one too, started with the server's own. spec.mcp holds
	// values and grants nothing. Crewlet's `shared`.
	// +optional
	// +default=true
	Shared *bool `json:"shared,omitempty"`

	// access is which agent seats the server reaches: whose turns are
	// offered its tools, from the one shared instance or, with shared
	// false, from an instance of the seat's own, and whose executor box a
	// spec.sandbox.mcpServerRefs entry may give it. Unset, every agent seat
	// of the company, one added later too. Set, exactly the seats it names;
	// set and empty ({}) reaches no seat, which Crewlet warns of, and a
	// shared server's one instance still starts (enabled false starts
	// nothing). A Seat's or a Unit's spec.mcp holds values for the seats a
	// server reaches and grants nothing: Crewlet refuses a Seat's spec.mcp
	// entry or spec.sandbox.mcpServerRefs entry for a server that does not
	// reach the seat, and a Unit's spec.mcp entry for one that reaches none
	// of the Unit's own agent seats, except an entry for a server named
	// github, gitlab, datadog, atlassian, jira or confluence, which Crewlet
	// reads itself. A server with seatCredentials, or one Crewlet recognises
	// by its command or address as reading a vendor's per-seat credential,
	// reaches the agent seats that are on at that vendor's integration
	// (spec.integrations.<vendor>.enabled) and takes no access. Crewlet's
	// `access`.
	// +optional
	Access *MCPServerAccess `json:"access,omitempty"`

	// command is the executable a Stdio server is launched with, as a process
	// of the engine's container, by the engine's user. It must be installed
	// in the engine image, and Crewlet's image ships no Python or Node.js
	// runtime (docs/guides/tool-servers.md). Crewlet's `command`, which it
	// passes on as written: it resolves no ${NAME} in it.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="command may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	Command string `json:"command,omitempty"`

	// args are the executable's arguments. Crewlet's `args`, which it passes
	// on as written: it resolves no ${NAME} in them.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MaxLength=4096
	// +kubebuilder:validation:XValidation:rule="self.all(x, !x.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]'))",message="args may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	Args []string `json:"args,omitempty"`

	// env is the environment of a Stdio server's process, shared by every
	// instance; per-seat values belong in a Seat's or a Unit's spec.mcp,
	// which win over these. It is laid over the part of the engine's own
	// environment Crewlet hands a tool server: PATH, the locale, the TLS and
	// proxy variables and the engine user's HOME, among a few others
	// (docs/guides/tool-servers.md). Nothing else of the engine's
	// environment reaches the server: a variable the engine image sets with
	// ENV, or the class or the Company sets on the pod, reaches it only when
	// it is one of those, so every other variable the server reads is given
	// here. Crewlet's `env`, a credential position.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Env []MCPEnvVar `json:"env,omitempty"`

	// url is the endpoint of an HTTP server: https, or plain http only to a
	// host on no public network, since every request carries the server's
	// credentials. That is a loopback, private, link-local or shared (RFC
	// 6598, where a tailnet addresses its machines) address, a one-label
	// name (a Service's own name in its namespace), localhost, or
	// a name under .localhost, .local (a Service's
	// <name>.<namespace>.svc.cluster.local), .home.arpa or .internal, judged
	// by what the address says, as Crewlet judges it. Read by one pattern
	// over the address rather than through url(), whose parses the API
	// server's cost budget for a rule cannot afford on an address this
	// long. Crewlet's `url`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() in ['http', 'https']",message="url must be an absolute http or https URL"
	// +kubebuilder:validation:XValidation:rule="!self.matches('(?i)^http://') || self.matches('(?i)^http://(localhost|[a-z0-9-]+|[a-z0-9.-]*\\\\.(localhost|local|home\\\\.arpa|internal)|(127|10)(\\\\.[0-9]{1,3}){3}|192\\\\.168(\\\\.[0-9]{1,3}){2}|172\\\\.(1[6-9]|2[0-9]|3[01])(\\\\.[0-9]{1,3}){2}|169\\\\.254(\\\\.[0-9]{1,3}){2}|100\\\\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])(\\\\.[0-9]{1,3}){2}|\\\\[(::1|f[cd][0-9a-f]{2}:[0-9a-f:.]*|fe[89ab][0-9a-f]:[0-9a-f:.]*)\\\\])(:[0-9]+)?([/?#].*)?$')",message="url may be plain http only to a host on no public network, where every request and the credentials in it would travel unencrypted: use https, or a loopback, private or link-local address, a one-label name, localhost, or a name under .localhost, .local, .home.arpa or .internal"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://[^/?#]*@')",message="url may not hold a user name or password: a credential is named through a Secret, never written into an address"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="url may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	URL string `json:"url,omitempty"`

	// headers are sent with every request to an HTTP server, shared by every
	// instance; per-seat values belong in a Seat's or a Unit's spec.mcp.
	// Header names are case-insensitive, so names must differ in more than
	// case. Crewlet's `headers`, a credential position.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:XValidation:rule="self.all(a, self.exists_one(b, b.name.lowerAscii() == a.name.lowerAscii()))",message="names must differ in more than case: HTTP header names are case-insensitive"
	Headers []MCPHeader `json:"headers,omitempty"`

	// oauth signs the company in to an HTTP server with OAuth, for a server
	// that takes no static token: the engine sends the access token it is
	// granted in the Authorization header of every request, and has a new
	// one before it runs out, on every pod that runs the server. Its grant
	// says how the engine is granted access. With AuthorizationCode, the
	// default, the engine registers at the server's authorization server as
	// its client says and a person signs in once from the server's page in
	// the engine's dashboard: until somebody signs in, and again once the
	// vendor refuses to renew the grant, the engine starts no instance of
	// the server and reports it as needing a sign-in, so its tools go
	// unserved, and a grant is sent only to the address it was issued for,
	// so a changed url waits for a sign-in at the new address. With
	// ClientCredentials the engine signs in as the company's own client, a
	// service identity, whenever it needs an access token: nobody signs in,
	// the server starts at once, and a vendor that refuses the client's
	// credentials fails the server's instances, which report the refusal.
	// One grant for the whole company, so only for a server whose shared is
	// true, and never beside an Authorization header in headers, which the
	// sign-in sends itself, nor beside a header named oauth.client_secret,
	// the key the engine's dashboard stores the client's secret under. No
	// executor's box is given such a server: a Seat's
	// spec.sandbox.mcpServerRefs naming it holds the company's document,
	// since a box is handed a server's credentials once, at its launch,
	// and cannot renew the access token, so the box reaches the server's
	// tools through its tool bridge instead. Crewlet's `oauth`.
	// +optional
	OAuth MCPOAuth `json:"oauth,omitzero"`

	// optionalKeys names the keys whose value may go missing: variables of env
	// (Stdio) or headers (HTTP), and the values every Seat and Unit gives the
	// server in spec.mcp, which land in the same environment or the same headers.
	// Crewlet starts a server only once every value it is started with has one, so
	// a value read from a Secret that does not exist, holds no such key or holds
	// an empty value keeps the server from starting. Under a key listed here, such
	// a value is left out instead, whole: the server starts without the variable,
	// or its requests go without the header, as Kubernetes starts a container
	// without an optional secretKeyRef. A missing Secret or key under one is then
	// no failure of the references of the object that gives the value, and holds
	// nothing back. Under the Enforce policy the operator also takes the value it
	// pushed for it out of the engine's secret store, and has the engine read the
	// store again whether or not the document can be applied, so the server is not
	// started again with a deleted Secret's value, but by a revision the engine
	// runs that reads the key as required (one from before the key was listed) or
	// whose reading the operator does not know (another document somebody else
	// activated), which keeps the value until the next document runs, and that one
	// is activated once with it and at once again without it; a Secret key that is
	// also read where it is required holds the document while it is missing, and
	// keeps the value it had in a revision the engine runs that requires it too
	// until the document applies. A Seat's or a Unit's value for a key replaces
	// the server's whole, so one that goes missing leaves the key out rather than
	// handing the server's own value back; and a provisioned value an Integration
	// mints for a seat is waited for whatever this says. Every entry must name a
	// key at least one value read from a Secret, or provisioned, is written under,
	// here or in a Seat's or a Unit's spec.mcp for this server: one under which
	// every value is a literal, which is never missing, or no value at all, leaves
	// nothing out and holds the document (ResolvedRefs=False with
	// OptionalKeyUnused), as Crewlet refuses it. A Stdio server's keys are
	// compared as written, an HTTP server's without case, as headers are.
	// Crewlet's `optional_keys`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=128
	// +kubebuilder:validation:items:Pattern=`^[A-Za-z0-9!#$%&'*+.^_|~-]+$`
	OptionalKeys []string `json:"optionalKeys,omitempty"`

	// seatCredentials declare which integration's per-seat credential the
	// server reads, and under which variable (Stdio) or header (HTTP), so
	// the engine gives each seat's instance that seat's own credential: the
	// one it minted for the seat's account, or the one the Seat hands in
	// for an integration it does not provision. Unset, a server the engine
	// recognises by its command or address (glab mcp serve, slack-mcp-server,
	// mcp-server-mattermost, mcp-atlassian, GitHub's and Datadog's remote
	// servers) reads what the engine knows it reads; a declaration replaces
	// that. A shared server reads none. A server that reads one reaches the
	// agent seats that are on at its integration
	// (spec.integrations.<vendor>.enabled, where Jira and Confluence are
	// Atlassian's) and takes no access. Crewlet's `seat_credentials`.
	// +optional
	// +listType=map
	// +listMapKey=key
	// +kubebuilder:validation:MaxItems=8
	SeatCredentials []MCPSeatCredential `json:"seatCredentials,omitempty"`

	// toolPrefix is put in front of each of the server's tool names, to keep
	// them apart from another server's. Every harness puts a prefix of its
	// own in front of that before its model sees the tool, and a model takes
	// a tool name of at most 64 characters of letters, digits, _ and -, so
	// Crewlet offers a tool only where its prefixed name holds those alone
	// and is at most 50 characters long, what the longest such harness prefix
	// leaves; it never shortens one. A prefix of at most 49 leaves room for a
	// name. Crewlet's `tool_prefix`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=49
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_-]+$`
	ToolPrefix string `json:"toolPrefix,omitempty"`

	// toolAnnotations correct the hints individual tools advertise.
	// Crewlet's `tool_annotations`.
	// +optional
	// +listType=map
	// +listMapKey=tool
	// +kubebuilder:validation:MaxItems=128
	ToolAnnotations []MCPToolAnnotations `json:"toolAnnotations,omitempty"`

	// tools sets the level each of the server's tools runs at: default for
	// every tool, levels for one tool by the server's own name. A Blocked
	// tool is never offered to an agent, a call to a tool at Approve waits for
	// a person's answer, and a Seat's sandbox may list only a server whose
	// every tool runs at Allow. Unset, a tool the server marks read-only runs
	// at Allow and every other tool at Approve. Crewlet's `tools`.
	// +optional
	Tools MCPTools `json:"tools,omitzero"`

	// startupTimeoutSeconds bounds starting the server and the handshake,
	// and separately its first tool listing, so a start can take up to twice
	// it before Crewlet gives the server up. Unset takes Crewlet's default
	// of 120. Crewlet's `startup_timeout_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	StartupTimeoutSeconds int32 `json:"startupTimeoutSeconds,omitempty"`

	// requestTimeoutSeconds bounds one tool call. Unset takes Crewlet's
	// default of 300. Crewlet's `request_timeout_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	RequestTimeoutSeconds int32 `json:"requestTimeoutSeconds,omitempty"`
}

// MCPServerStatus is what the operator observed about an MCPServer.
// +kubebuilder:validation:MinProperties=1
type MCPServerStatus struct {
	// conditions are Accepted (the Company exists), ResolvedRefs (every
	// Secret key exists) and Applied (the server is in the revision the
	// engine runs).
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the metadata.generation this status describes.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// MCPServer is a Model Context Protocol tool server a company's agents call.
//
// Its name is the server's name in the company document, which Crewlet holds
// to lowercase letters, digits and '-', with no '.', at most 32 characters:
// it is the origin of every tool the server serves, the key a Seat's or a
// Unit's spec.mcp names it by, and part of every tool name an executor's box
// hands its model for a server it runs.
//
// The name crewlet is reserved: the engine gives a seat's own tools that name,
// as the one MCP server each run of its harness calls them through, inside
// an executor's box beside the servers its spec.sandbox.mcpServerRefs name.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mcps,categories=crewlet
// +kubebuilder:printcolumn:name="Company",type=string,JSONPath=`.spec.companyRef.name`
// +kubebuilder:printcolumn:name="Transport",type=string,JSONPath=`.spec.transport`
// +kubebuilder:printcolumn:name="Shared",type=boolean,JSONPath=`.spec.shared`
// +kubebuilder:printcolumn:name="Enabled",type=boolean,JSONPath=`.spec.enabled`
// +kubebuilder:printcolumn:name="Applied",type=string,JSONPath=`.status.conditions[?(@.type=="Applied")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name != 'crewlet'",message="the name crewlet is reserved for the seat's own tool bridge"
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z0-9]([a-z0-9-]*[a-z0-9])?$') && size(self.metadata.name) <= 32",message="an MCPServer's name is its Crewlet server name: lowercase letters, digits and '-', with no '.', at most 32 characters"
type MCPServer struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata. Its name is the server's
	// name in the company document.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec is the server's desired state.
	// +required
	Spec MCPServerSpec `json:"spec,omitzero"`

	// status is the server's observed state.
	// +optional
	Status MCPServerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// MCPServerList contains a list of MCPServer.
type MCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []MCPServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &MCPServer{}, &MCPServerList{})
		return nil
	})
}
