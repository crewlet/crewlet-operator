package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// UnitType is an informational label for a unit; nothing in the engine
// behaves differently for one. It maps onto Crewlet's well-known unit types
// (Division is `division`, Team is `team`, and so on).
// +kubebuilder:validation:Enum=Division;Department;Group;Team;Squad;Pod;Guild;Chapter;Unit
type UnitType string

// The unit types.
const (
	UnitTypeDivision   UnitType = "Division"
	UnitTypeDepartment UnitType = "Department"
	UnitTypeGroup      UnitType = "Group"
	UnitTypeTeam       UnitType = "Team"
	UnitTypeSquad      UnitType = "Squad"
	UnitTypePod        UnitType = "Pod"
	UnitTypeGuild      UnitType = "Guild"
	UnitTypeChapter    UnitType = "Chapter"
	UnitTypeUnit       UnitType = "Unit"
)

// MCPCredentialValue is one value a tool server is started with for a seat:
// one of its credentials, from a Secret, or a setting that is no credential,
// written as a literal value. Whether it is an
// environment variable or a request header depends on the server's
// transport, which is why its name follows the grammar of an HTTP header
// name, of which an environment variable's name is a subset.
//
// A literal value is for a setting such as a path: slack-mcp-server, for one,
// keeps the channels its bot can see in a file named after the workspace
// alone, so each seat names its own (SLACK_MCP_CHANNELS_CACHE), or seats on
// one engine pod are shown each other's private channels. Crewlet treats every
// value of mcp_env as a credential and masks it on every read, and cannot
// tell a setting from a credential; the name can, so a name that says it
// holds a credential, as Crewlet reads one (see MCPEnvVar), takes valueFrom.
// +kubebuilder:validation:XValidation:rule="has(self.value) != has(self.valueFrom)",message="exactly one of value and valueFrom must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.value) || !(self.name.matches('(?i)(^|[^A-Za-z0-9])(KEY|KEYS|APIKEY|ACCESSKEY|SECRETKEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|CREDENTIALS|AUTH|OAUTH|PAT)([^A-Za-z0-9]|$)') || self.name.lowerAscii() in ['authorization', 'proxy-authorization', 'cookie'])",message="value may not be given to a name that says it holds a credential, which comes from a Secret through valueFrom: one of its parts, cut at every character that is not a letter or a digit, is, in any case, KEY, KEYS, APIKEY, ACCESSKEY, SECRETKEY, TOKEN, SECRET, PASSWORD, PASSWD, CREDENTIAL, CREDENTIALS, AUTH, OAUTH or PAT, as Crewlet reads a credential's name, or it is the Authorization, Proxy-Authorization or Cookie header",fieldPath=".value"
type MCPCredentialValue struct {
	// name is the environment variable's name (Stdio) or the header's name
	// (HTTP).
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

	// valueFrom is the Secret key a credential comes from. For a header the
	// value is the whole header, scheme included (for example "Bearer
	// <token>"). A credential the engine mints for a seat's account at a
	// vendor it provisions is never named here: the engine gives it to the
	// vendor's tool server itself, and refuses a credential of that vendor's
	// server here.
	// +optional
	ValueFrom SecretValueSource `json:"valueFrom,omitzero"`
}

// MCPCredentials are the values one tool server is started with for a seat:
// environment variables for a Stdio server, request headers for an HTTP one.
// They map onto one server's entry of Crewlet's mcp_env, a credential
// position.
//
// The server is named by a top-level name, as an imagePullSecrets entry names
// its Secret, rather than by a nested reference, because name is the key of
// the list that holds these: with a scalar key, server-side apply merges the
// list entry by entry, so that one field manager (a control plane's GitHub
// flow, say) can own the credentials of one server without owning, or
// overwriting, every other server's.
type MCPCredentials struct {
	// name is the metadata.name of the MCPServer, in this namespace, the
	// values are for.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`

	// env are the values. For a Stdio server each is an environment
	// variable of its process; for an HTTP server each is a request header
	// (the name is the header's name). Names must differ in more than case:
	// HTTP header names are case-insensitive, so Authorization and
	// authorization are one header.
	// +required
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:XValidation:rule="self.all(a, self.exists_one(b, b.name.lowerAscii() == a.name.lowerAscii()))",message="names must differ in more than case: HTTP header names are case-insensitive"
	Env []MCPCredentialValue `json:"env,omitempty"`
}

// UnitSpec is one node of a company's org chart: a division, a team, a squad.
// It renders to one entry of Crewlet's units, nested under its parent's.
type UnitSpec struct {
	// companyRef names the Company this unit belongs to. Immutable.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="companyRef is immutable"
	CompanyRef CompanyReference `json:"companyRef,omitzero"`

	// displayName is the unit's name as people read it, in prompts and on
	// boards. Unset is the Unit's metadata.name. The metadata.name itself is
	// the unit's stable id, which survives a rename. Crewlet's `name`; the
	// metadata.name is Crewlet's `id`, the key a seat's manages entry names
	// the unit by. The displayName is for reading only: nothing refers to a
	// unit by it, so two Units may share one.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	DisplayName string `json:"displayName,omitempty"`

	// type is an informational label. Crewlet's `type`.
	// +optional
	Type UnitType `json:"type,omitempty"`

	// parentRef names the Unit this one belongs to. Unset makes it a
	// top-level unit. A parent that does not exist, or a chain of parents
	// that leads back to this unit, is reported on this Unit.
	// +optional
	ParentRef LocalObjectReference `json:"parentRef,omitzero"`

	// leadRef names the Seat that leads the unit. Unset inherits the
	// parent's lead. Crewlet's `lead`.
	// +optional
	LeadRef LocalObjectReference `json:"leadRef,omitzero"`

	// purpose is what the unit is for. Crewlet's `purpose`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	Purpose string `json:"purpose,omitempty"`

	// goals are what the unit is working toward. Crewlet's `goals`.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=1024
	Goals []string `json:"goals,omitempty"`

	// channel is the unit's channel on the company's chat surface, such as
	// #platform; child units without one inherit it. Crewlet's `channel`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Channel string `json:"channel,omitempty"`

	// knowledge are free-text references to knowledge the unit relies on.
	// They are not a read scope. Crewlet's `knowledge`.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=1024
	Knowledge []string `json:"knowledge,omitempty"`

	// project is the tracker project the unit files work in, by its key.
	// Crewlet requires a container key to be an upper-case letter followed
	// by 1 to 9 upper-case letters or digits, such as PLAT. Crewlet's
	// `project`.
	// +optional
	// +kubebuilder:validation:MinLength=2
	// +kubebuilder:validation:MaxLength=10
	// +kubebuilder:validation:Pattern=`^[A-Z][A-Z0-9]{1,9}$`
	Project string `json:"project,omitempty"`

	// space is the knowledge container the unit writes pages in, by its
	// key, which follows the same rule as project. Crewlet's `space`.
	// +optional
	// +kubebuilder:validation:MinLength=2
	// +kubebuilder:validation:MaxLength=10
	// +kubebuilder:validation:Pattern=`^[A-Z][A-Z0-9]{1,9}$`
	Space string `json:"space,omitempty"`

	// mcp are tool-server credentials and settings every agent seat
	// directly in this unit inherits; a seat's own values win per variable.
	// One entry per server, keyed by its name. Crewlet's `mcp_env`, a
	// credential position. A value one seat must not share, such as
	// slack-mcp-server's channel cache or a seat's own credential, belongs
	// on the Seat.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=16
	MCP []MCPCredentials `json:"mcp,omitempty"`
}

// UnitStatus is what the operator observed about a Unit.
// +kubebuilder:validation:MinProperties=1
type UnitStatus struct {
	// conditions are Accepted (the Company exists), ResolvedRefs (the
	// parent, the lead, the tool servers and every Secret key exist, and the
	// parents form no cycle) and Applied (the unit is in the revision the
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

	// depth is the unit's distance from the top of the chart: 0 for a
	// top-level unit.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Depth *int32 `json:"depth,omitempty"`

	// seats is the number of Seats directly in the unit.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Seats *int32 `json:"seats,omitempty"`
}

// Unit is one node of a company's org chart. Units nest through
// spec.parentRef; Seats join one through spec.unitRef.
//
// The Unit's metadata.name is the unit's stable id in Crewlet, which has its
// own rules: it must start with a lowercase letter and be at most 64
// characters, and may hold lowercase letters, digits and '-'. (Crewlet also
// admits '_' in a unit id, which no Kubernetes name can hold.)
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=un,categories=crewlet
// +kubebuilder:printcolumn:name="Company",type=string,JSONPath=`.spec.companyRef.name`
// +kubebuilder:printcolumn:name="Parent",type=string,JSONPath=`.spec.parentRef.name`
// +kubebuilder:printcolumn:name="Lead",type=string,JSONPath=`.spec.leadRef.name`
// +kubebuilder:printcolumn:name="Applied",type=string,JSONPath=`.status.conditions[?(@.type=="Applied")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z][a-z0-9-]{0,63}$')",message="a Unit's name is its Crewlet unit id: it must start with a lowercase letter, hold only lowercase letters, digits and '-', and be at most 64 characters"
type Unit struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec is the unit's desired state.
	// +required
	Spec UnitSpec `json:"spec,omitzero"`

	// status is the unit's observed state.
	// +optional
	Status UnitStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// UnitList contains a list of Unit.
type UnitList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Unit `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Unit{}, &UnitList{})
		return nil
	})
}
