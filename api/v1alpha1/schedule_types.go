package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ScheduleFanOut is who runs a schedule that targets a unit. It maps onto
// Crewlet's schedule `target` (Each is `each`, Lead is `lead`).
// +kubebuilder:validation:Enum=Each;Lead
type ScheduleFanOut string

// The fan-outs of a unit's schedule.
const (
	// ScheduleFanOutEach runs one task per agent seat directly in the unit:
	// never the seats of the units below it, and never a Human seat. Each
	// seat runs its own turn, so a slow one never holds up the others.
	ScheduleFanOutEach ScheduleFanOut = "Each"
	// ScheduleFanOutLead runs one task as the unit's lead: its leadRef, or,
	// unset, the lead it inherits from the units above it. It follows a
	// change of lead without a change to the Schedule.
	ScheduleFanOutLead ScheduleFanOut = "Lead"
)

// ScheduleTarget is whose recurring work a schedule is: one seat's, or a
// unit's. It decides which entry of the company document the schedule is
// rendered into: the seat's role, or the unit's.
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:XValidation:rule="has(self.seatRef) != has(self.unitRef)",message="a schedule targets exactly one of seatRef and unitRef"
// +kubebuilder:validation:XValidation:rule="!has(self.fanOut) || has(self.unitRef)",message="fanOut applies to a unit's schedule only: a seat's schedule always runs as that seat"
type ScheduleTarget struct {
	// seatRef names the Seat, in this namespace, whose schedule this is; it
	// always runs as that seat. The Seat must be an Agent: the engine runs
	// no turn for a person. Rendered into the seat's role `schedules`.
	// +optional
	SeatRef LocalObjectReference `json:"seatRef,omitzero"`

	// unitRef names the Unit, in this namespace, whose schedule this is;
	// fanOut says who in it runs it. A unit's schedule is not inherited by
	// the units below it. Rendered into the unit's `schedules`.
	// +optional
	UnitRef LocalObjectReference `json:"unitRef,omitzero"`

	// fanOut is who in the unit runs it. Unset is Each. Crewlet's `target`.
	// The engine refuses a schedule that could never run while it is not
	// suspended: Each on a unit with no agent seat directly in it, and Lead
	// on a unit whose lead is a Human seat.
	// +optional
	FanOut ScheduleFanOut `json:"fanOut,omitempty"`
}

// ScheduleSpec is recurring work a seat or a unit owns: a task the engine
// hands its runner on a cron schedule, as a CronJob runs a Job. It renders to
// one entry of a Crewlet role's or unit's `schedules`, whose `name` is the
// Schedule's metadata.name.
type ScheduleSpec struct {
	// companyRef names the Company this schedule belongs to. Immutable.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="companyRef is immutable"
	CompanyRef CompanyReference `json:"companyRef,omitzero"`

	// target is the seat or the unit whose schedule this is.
	// +required
	Target ScheduleTarget `json:"target,omitzero"`

	// schedule is when the task fires: a standard cron expression of five
	// fields (minute, hour, day of month, month, day of week), such as
	// "30 9 * * 1-5", evaluated in timeZone. When both the day of month and
	// the day of week are restricted, a day matches if either does. The
	// engine's dry run checks each field's values. Crewlet's `cron`.
	// +required
	// +kubebuilder:validation:MinLength=9
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:XValidation:rule="self.matches('^\\\\s*\\\\S+(\\\\s+\\\\S+){4}\\\\s*$')",message="schedule is a cron expression of exactly five fields (minute hour day-of-month month day-of-week), such as 30 9 * * 1-5; macros such as @daily and a CRON_TZ= prefix are not supported: set timeZone instead"
	Schedule string `json:"schedule,omitempty"`

	// timeZone is the IANA time zone the schedule is evaluated in, such as
	// Asia/Tokyo. Unset is the Company's spec.timeZone, which is UTC when
	// that is unset too. The engine refuses a zone it cannot load, and
	// Local, which would be whatever zone the engine's host is set to.
	// Crewlet's `timezone`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+)*$`
	TimeZone string `json:"timeZone,omitempty"`

	// prompt is the task the runner is handed each time the schedule fires,
	// such as "Post your standup: what shipped yesterday, what is next,
	// what blocks you." Crewlet's `task`.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=8192
	// +kubebuilder:validation:XValidation:rule="self.matches('\\\\S')",message="prompt must not be blank: it is the task the schedule hands its runner"
	Prompt string `json:"prompt,omitempty"`

	// suspend keeps the schedule in the company without firing it. Unset
	// is false. Crewlet's `enabled`, inverted.
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// timeoutSeconds caps one scheduled turn's wall-clock time, the seat's
	// executor run included: a run still working when it passes is ended,
	// and the turn starts no further round and ends failed. Unset is the
	// engine's default, 1800 seconds, which leaves a slow ritual room for its
	// box's start, its setup, its run and a review sent back once. A
	// schedule that fires more often than its turns take queues its fires
	// behind the turn, never merging them, so give one that fires every few
	// minutes a timeoutSeconds below its period. Crewlet's
	// `timeout_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`

	// catchUp says whether an engine that starts after missing a fire runs
	// the most recent missed one, when it is recent enough (half the
	// schedule's period, within the engine's catch-up bounds); older misses
	// are never run. Unset is true. Crewlet's `catchup`.
	// +optional
	CatchUp *bool `json:"catchUp,omitempty"`
}

// ScheduleStatus is what the operator observed about a Schedule.
// +kubebuilder:validation:MinProperties=1
type ScheduleStatus struct {
	// conditions are Accepted (the Company exists), ResolvedRefs (the seat
	// or unit it targets exists, and a seat is an Agent) and Applied (the
	// schedule is in the revision the engine runs).
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

// Schedule is recurring work a seat or a unit owns, fired on a cron schedule:
// the CronJob of a company.
//
// The Schedule's metadata.name is the schedule's name in the engine, which
// is half of the identity the engine claims each fire under, so that a fire
// runs once however often the engine evaluates its minute. Two Schedules of
// one seat or unit therefore never share a name, and a fire is never run
// twice.
//
// A fire that comes due while its seat is paused is skipped, not queued.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=sched,categories=crewlet
// +kubebuilder:printcolumn:name="Company",type=string,JSONPath=`.spec.companyRef.name`
// +kubebuilder:printcolumn:name="Seat",type=string,JSONPath=`.spec.target.seatRef.name`
// +kubebuilder:printcolumn:name="Unit",type=string,JSONPath=`.spec.target.unitRef.name`
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="Suspend",type=boolean,JSONPath=`.spec.suspend`
// +kubebuilder:printcolumn:name="Applied",type=string,JSONPath=`.status.conditions[?(@.type=="Applied")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Schedule struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec is the schedule's desired state.
	// +required
	Spec ScheduleSpec `json:"spec,omitzero"`

	// status is the schedule's observed state.
	// +optional
	Status ScheduleStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ScheduleList contains a list of Schedule.
type ScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Schedule `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Schedule{}, &ScheduleList{})
		return nil
	})
}
