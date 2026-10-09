package crd_test

import (
	"strings"
	"testing"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The messages of the Schedule's rules.
const (
	msgScheduleTarget = "a schedule targets exactly one of seatRef and unitRef"
	msgScheduleFanOut = "fanOut applies to a unit's schedule only"
	msgScheduleCron   = "schedule is a cron expression of exactly five fields"
	msgSchedulePrompt = "prompt must not be blank"
)

func TestScheduleValidation(t *testing.T) {
	seatTarget := func(s *v1.Schedule) {
		s.Spec.Target = v1.ScheduleTarget{SeatRef: v1.LocalObjectReference{Name: seatHandle}}
	}
	runCreateCases(t, unitSchedule, []createCase[*v1.Schedule]{
		{name: "a unit's schedule is accepted"},
		{
			name: "a unit's schedule with every field is accepted",
			mutate: func(s *v1.Schedule) {
				s.Spec.Target.FanOut = v1.ScheduleFanOutLead
				s.Spec.TimeZone = "America/Argentina/Buenos_Aires"
				s.Spec.Suspend = new(true)
				s.Spec.TimeoutSeconds = 900
				s.Spec.CatchUp = new(false)
			},
		},
		{name: "a seat's schedule is accepted", mutate: seatTarget},
		{
			name: "a cron expression with ranges, steps, lists and names is accepted",
			mutate: func(s *v1.Schedule) {
				s.Spec.Schedule = " */15 9-17 1,15 JAN-JUN mon-fri "
			},
		},
		{
			name:   "a schedule with no target is refused",
			mutate: func(s *v1.Schedule) { s.Spec.Target = v1.ScheduleTarget{} },
			want:   "spec.target: Required value",
		},
		{
			name:   "a target that names neither a seat nor a unit is refused",
			mutate: func(s *v1.Schedule) { s.Spec.Target = v1.ScheduleTarget{FanOut: v1.ScheduleFanOutLead} },
			want:   msgScheduleTarget,
		},
		{
			name: "a schedule with a seat and a unit is refused",
			mutate: func(s *v1.Schedule) {
				s.Spec.Target.SeatRef = v1.LocalObjectReference{Name: seatHandle}
			},
			want: msgScheduleTarget,
		},
		{
			name: "a seat's schedule with a fanOut is refused",
			mutate: func(s *v1.Schedule) {
				seatTarget(s)
				s.Spec.Target.FanOut = v1.ScheduleFanOutEach
			},
			want: msgScheduleFanOut,
		},
		{
			name:   "an unknown fanOut is refused",
			mutate: func(s *v1.Schedule) { s.Spec.Target.FanOut = "Everyone" },
			want:   `supported values: "Each", "Lead"`,
		},
		{
			name:   "a cron expression of four fields is refused",
			mutate: func(s *v1.Schedule) { s.Spec.Schedule = "30 9 * *" },
			want:   msgScheduleCron,
		},
		{
			name:   "a cron expression of six fields (seconds) is refused",
			mutate: func(s *v1.Schedule) { s.Spec.Schedule = "0 30 9 * * 1-5" },
			want:   msgScheduleCron,
		},
		{
			name:   "a macro is refused",
			mutate: func(s *v1.Schedule) { s.Spec.Schedule = "@midnight" },
			want:   msgScheduleCron,
		},
		{
			name:   "a CRON_TZ prefix is refused",
			mutate: func(s *v1.Schedule) { s.Spec.Schedule = "CRON_TZ=UTC 0 9 * * *" },
			want:   msgScheduleCron,
		},
		{
			name:   "a time zone that is no IANA name is refused",
			mutate: func(s *v1.Schedule) { s.Spec.TimeZone = "+02:00" },
			want:   "spec.timeZone: Invalid value",
		},
		{
			name:   "a blank prompt is refused",
			mutate: func(s *v1.Schedule) { s.Spec.Prompt = " \n\t" },
			want:   msgSchedulePrompt,
		},
		{
			name:   "a prompt past its bound is refused",
			mutate: func(s *v1.Schedule) { s.Spec.Prompt = strings.Repeat("x", 8193) },
			want:   "spec.prompt: Too long",
		},
		{
			name:   "a negative timeout is refused",
			mutate: func(s *v1.Schedule) { s.Spec.TimeoutSeconds = -1 },
			want:   "spec.timeoutSeconds: Invalid value",
		},
	})
}
