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

// MCPServerSpec is one tool server a company's agents call. It renders to one
// entry of Crewlet's mcp_servers, named by the MCPServer's metadata.name;
// Seats and Units give it per-seat credentials by that name.
// +kubebuilder:validation:XValidation:rule="self.transport == 'Stdio' ? has(self.command) : !has(self.command)",message="command is required with transport Stdio and not allowed with HTTP"
// +kubebuilder:validation:XValidation:rule="self.transport == 'Stdio' || (!has(self.args) && !has(self.env))",message="args and env apply to transport Stdio only"
// +kubebuilder:validation:XValidation:rule="self.transport == 'HTTP' ? has(self.url) : !has(self.url)",message="url is required with transport HTTP and not allowed with Stdio"
// +kubebuilder:validation:XValidation:rule="self.transport == 'HTTP' || !has(self.headers)",message="headers apply to transport HTTP only"
type MCPServerSpec struct {
	// companyRef names the Company this server belongs to. Immutable.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="companyRef is immutable"
	CompanyRef CompanyReference `json:"companyRef,omitzero"`

	// transport is how the engine reaches the server. Crewlet's `transport`.
	// +optional
	// +default="Stdio"
	Transport MCPTransport `json:"transport,omitempty"`

	// shared launches one instance whose tools every agent calls. false
	// makes the server a template instead: every seat that has credentials
	// for it in spec.mcp gets an instance of its own, which is how each
	// agent acts as itself on the server. Crewlet's `shared`.
	// +optional
	// +default=true
	Shared *bool `json:"shared,omitempty"`

	// command is the executable a Stdio server is launched with. Crewlet's
	// `command`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	Command string `json:"command,omitempty"`

	// args are the executable's arguments. Crewlet's `args`.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MaxLength=4096
	Args []string `json:"args,omitempty"`

	// env is the environment of a Stdio server's process, shared by every
	// instance; per-seat values belong in a Seat's or a Unit's spec.mcp.
	// Crewlet's `env`, a credential position.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Env []SecretEnvVar `json:"env,omitempty"`

	// url is the endpoint of an HTTP server. Crewlet's `url`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() in ['http', 'https']",message="url must be an absolute http or https URL"
	URL string `json:"url,omitempty"`

	// headers are sent with every request to an HTTP server, shared by every
	// instance; per-seat values belong in a Seat's or a Unit's spec.mcp.
	// Crewlet's `headers`, a credential position.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Headers []SecretHeader `json:"headers,omitempty"`

	// toolPrefix is put in front of the server's tool names, to keep them
	// apart from another server's. Crewlet's `tool_prefix`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	// +kubebuilder:validation:Pattern=`^[A-Za-z][A-Za-z0-9_-]*$`
	ToolPrefix string `json:"toolPrefix,omitempty"`

	// toolAnnotations correct the hints individual tools advertise.
	// Crewlet's `tool_annotations`.
	// +optional
	// +listType=map
	// +listMapKey=tool
	// +kubebuilder:validation:MaxItems=128
	ToolAnnotations []MCPToolAnnotations `json:"toolAnnotations,omitempty"`

	// startupTimeoutSeconds bounds starting the server, the handshake and
	// its first tool listing. Unset takes Crewlet's default of 60. Crewlet's
	// `startup_timeout_seconds`.
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
// The name crewlet is reserved: the engine gives a seat's own tool bridge that
// name inside a coding agent's sandbox.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mcps,categories=crewlet
// +kubebuilder:printcolumn:name="Company",type=string,JSONPath=`.spec.companyRef.name`
// +kubebuilder:printcolumn:name="Transport",type=string,JSONPath=`.spec.transport`
// +kubebuilder:printcolumn:name="Shared",type=boolean,JSONPath=`.spec.shared`
// +kubebuilder:printcolumn:name="Applied",type=string,JSONPath=`.status.conditions[?(@.type=="Applied")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name != 'crewlet'",message="the name crewlet is reserved for the seat's own tool bridge"
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
