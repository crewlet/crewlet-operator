package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// LLMProviderType is which model API a provider speaks. It maps onto the
// `type` of a Crewlet providers.llm entry: Anthropic is `anthropic`, OpenAI is
// `openai` and OpenAICompatible is `openai-compatible`.
//
// Crewlet's fourth type, `cli-agent`, drives a coding CLI installed next to
// the engine on a subscription login. It is not offered in v1alpha1: the
// engine image ships no coding CLI, and its credentials (a subscription token
// or an exported credential bundle) are per-node logins that a pod which can
// be rescheduled at any time cannot hold.
// +kubebuilder:validation:Enum=Anthropic;OpenAI;OpenAICompatible
type LLMProviderType string

// The LLM provider types.
const (
	// LLMProviderAnthropic is Anthropic's Messages API.
	LLMProviderAnthropic LLMProviderType = "Anthropic"
	// LLMProviderOpenAI is OpenAI's API.
	LLMProviderOpenAI LLMProviderType = "OpenAI"
	// LLMProviderOpenAICompatible is any endpoint that speaks the OpenAI wire
	// format: a gateway, an aggregator, a self-hosted inference server.
	LLMProviderOpenAICompatible LLMProviderType = "OpenAICompatible"
)

// ReasoningEffort is how hard a model thinks. It maps onto Crewlet's
// `reasoning_effort` (Low is `low`, XHigh is `xhigh`, and so on).
// +kubebuilder:validation:Enum=Low;Medium;High;XHigh;Max
type ReasoningEffort string

// The reasoning-effort levels, lowest first.
const (
	ReasoningEffortLow    ReasoningEffort = "Low"
	ReasoningEffortMedium ReasoningEffort = "Medium"
	ReasoningEffortHigh   ReasoningEffort = "High"
	ReasoningEffortXHigh  ReasoningEffort = "XHigh"
	ReasoningEffortMax    ReasoningEffort = "Max"
)

// LLMCredentialCooldowns is how long a key that failed is set aside before it
// is tried again. It maps onto Crewlet's `cooldowns` block.
// +kubebuilder:validation:MinProperties=1
type LLMCredentialCooldowns struct {
	// rateLimitSeconds is how long a key that hit a rate limit is set aside.
	// Unset takes Crewlet's default of 3600.
	// +optional
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:validation:Maximum=86400
	RateLimitSeconds int32 `json:"rateLimitSeconds,omitempty"`

	// authSeconds is how long a key the provider refused is set aside. Unset
	// takes Crewlet's default of 300.
	// +optional
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:validation:Maximum=86400
	AuthSeconds int32 `json:"authSeconds,omitempty"`
}

// LLMProviderSettings is everything about a model provider except where its
// keys come from. LLMProvider and the EngineClass's platform providers share
// it, so a platform-provided model is described exactly like a tenant's own.
// Each field maps onto the same-named field of a Crewlet providers.llm entry.
//
// The cross-field rules over these fields are declared on each type that
// embeds this one (LLMProviderSpec and PlatformLLMProvider), because
// controller-gen does not carry a validation declared on an embedded type into
// the schema of the type that inlines it. The API tests exercise both copies.
type LLMProviderSettings struct {
	// type is which model API the provider speaks. Crewlet's `type`.
	// +required
	Type LLMProviderType `json:"type,omitempty"`

	// model is the model id requests are sent for, as the provider names it.
	// Crewlet's `model`.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Model string `json:"model,omitempty"`

	// claudeModel names the Claude model whose request shape an Anthropic
	// provider uses when model is a gateway alias Crewlet cannot recognise.
	// Crewlet's `claude_model`; type Anthropic only.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	ClaudeModel string `json:"claudeModel,omitempty"`

	// baseURL sends requests to this endpoint instead of the vendor's own,
	// for a gateway or a proxy. Required for type OpenAICompatible. Crewlet's
	// `base_url`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() in ['http', 'https']",message="baseURL must be an absolute http or https URL"
	BaseURL string `json:"baseURL,omitempty"`

	// reasoning turns on an OpenAI reasoning model's effort, so that
	// reasoningEffort is sent. Crewlet's `reasoning`; type OpenAI only.
	// +optional
	Reasoning *bool `json:"reasoning,omitempty"`

	// reasoningEffort is how hard the model thinks. For Anthropic it is sent
	// on every call, and unset means High. For OpenAI it is sent only while
	// reasoning is true, so it requires reasoning; an OpenAICompatible
	// provider cannot turn reasoning on and takes no reasoningEffort.
	// Crewlet's `reasoning_effort`.
	// +optional
	ReasoningEffort ReasoningEffort `json:"reasoningEffort,omitempty"`

	// reasoningBudgetTokens is the thinking budget of an older, budget-based
	// Claude model. Unset means such a model does not think. Crewlet's
	// `reasoning_budget_tokens`; type Anthropic only.
	// +optional
	// +kubebuilder:validation:Minimum=1024
	ReasoningBudgetTokens int32 `json:"reasoningBudgetTokens,omitempty"`

	// timeoutSeconds bounds one HTTP attempt: the whole of a unary call, the
	// longest silence in a streamed one. Unset takes Crewlet's default of
	// 600. Crewlet's `timeout_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`

	// cooldowns is how long a failing key is set aside. Crewlet's
	// `cooldowns`.
	// +optional
	Cooldowns LLMCredentialCooldowns `json:"cooldowns,omitzero"`
}

// LLMProviderSpec is one model provider of a company. It renders to one entry
// of Crewlet's providers.llm, keyed by the LLMProvider's metadata.name; Seats
// and the Company's model order refer to it by that name.
// +kubebuilder:validation:XValidation:rule="!has(self.claudeModel) || self.type == 'Anthropic'",message="claudeModel applies to type Anthropic only"
// +kubebuilder:validation:XValidation:rule="!has(self.reasoning) || self.type == 'OpenAI'",message="reasoning applies to type OpenAI only; an Anthropic model's thinking depth is reasoningEffort"
// +kubebuilder:validation:XValidation:rule="!has(self.reasoningEffort) || self.type == 'Anthropic' || (self.type == 'OpenAI' && has(self.reasoning) && self.reasoning)",message="reasoningEffort is sent to an OpenAI model only while reasoning is true, and never to an OpenAICompatible one"
// +kubebuilder:validation:XValidation:rule="!has(self.reasoningBudgetTokens) || self.type == 'Anthropic'",message="reasoningBudgetTokens applies to type Anthropic only"
// +kubebuilder:validation:XValidation:rule="self.type != 'OpenAICompatible' || has(self.baseURL)",message="baseURL is required for type OpenAICompatible, which has no default endpoint"
// +kubebuilder:validation:XValidation:rule="self.type == 'OpenAICompatible' || has(self.apiKeySecretRefs)",message="apiKeySecretRefs needs at least one key for types Anthropic and OpenAI"
type LLMProviderSpec struct {
	// companyRef names the Company this provider belongs to. Immutable.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="companyRef is immutable"
	CompanyRef CompanyReference `json:"companyRef,omitzero"`

	LLMProviderSettings `json:",inline"`

	// apiKeySecretRefs are the provider's API keys, each a key of a Secret in
	// this namespace. With more than one, Crewlet rotates through them,
	// setting aside a key that hits a rate limit or is refused, in the order
	// listed when several are equally free. Crewlet's `api_keys`, a
	// credential position: the values are pushed into the engine's secret
	// store and the document refers to them by name only. Required for types
	// Anthropic and OpenAI; an OpenAICompatible endpoint that needs no key
	// may omit it.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	APIKeySecretRefs []SecretKeyReference `json:"apiKeySecretRefs,omitempty"`
}

// LLMProviderStatus is what the operator observed about an LLMProvider.
// +kubebuilder:validation:MinProperties=1
type LLMProviderStatus struct {
	// conditions are Accepted (the Company exists), ResolvedRefs (every
	// Secret key exists) and Applied (the provider is in the revision the
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

// LLMProvider is a model provider a company's seats can run on: one entry of
// Crewlet's providers.llm.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=llmp,categories=crewlet
// +kubebuilder:printcolumn:name="Company",type=string,JSONPath=`.spec.companyRef.name`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=`.spec.model`
// +kubebuilder:printcolumn:name="Applied",type=string,JSONPath=`.status.conditions[?(@.type=="Applied")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type LLMProvider struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata. Its name is the provider's
	// key in the company document.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec is the provider's desired state.
	// +required
	Spec LLMProviderSpec `json:"spec,omitzero"`

	// status is the provider's observed state.
	// +optional
	Status LLMProviderStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LLMProviderList contains a list of LLMProvider.
type LLMProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LLMProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &LLMProvider{}, &LLMProviderList{})
		return nil
	})
}
