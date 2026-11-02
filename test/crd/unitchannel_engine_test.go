package crd_test

import (
	"strings"
	"testing"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// keyChannel is Crewlet's `channel` on a unit, where its refusal of a
// channel name is.
const keyChannel = "channel"

// unitChannels are channel names on either side of Crewlet's rule: lowercase
// letters, digits, - and _, starting with a letter or a digit, up to 64
// characters, written verbatim.
var unitChannels = []string{
	unitName,
	"2fa-rollout",
	"eng_ops",
	"a",
	"p" + strings.Repeat("a", 63),
	"p" + strings.Repeat("a", 64),
	"#platform",
	"Platform",
	"C_ENG",
	"-ops",
	"_ops",
	" ops",
	"ops ",
	"eng.ops",
	"eng ops",
}

// TestUnitChannelFollowsTheEngine holds the Unit API's rule on channel to
// Crewlet's, both ways: the API refuses a channel exactly when crewlet
// validate refuses the document it renders to. Crewlet refuses the whole
// document over one unit's channel, so a name the API admitted and the engine
// refuses would hold back every revision of the company; and the API may not
// be stricter than the engine either, since a channel is a room name a
// vendor company already has. A Crewlet change to the rule fails here, on the
// next run, until the API follows it.
func TestUnitChannelFollowsTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	for _, channel := range unitChannels {
		t.Run(channel, func(t *testing.T) {
			probe := unit(namespace, unitName)
			probe.Spec.Channel = channel
			refused := apiVerdict(t, probe) != ""

			in := probeCompany()
			u := unit(probeNamespace, unitName)
			u.Spec.Channel = channel
			in.Units = []v1.Unit{*u}
			doc, err := render.Company(in)
			if err != nil {
				t.Fatalf("rendering the unit: %v", err)
			}
			problems, _ := validateVerdict(t, doc, doc.YAML)
			var faults []string
			for _, p := range problems {
				if n := len(p.Segments); n > 0 && p.Segments[n-1].Key == keyChannel {
					faults = append(faults, p.Message)
				} else {
					t.Errorf("crewlet validate refused the probe elsewhere: %s", p.Message)
				}
			}
			switch {
			case refused && len(faults) == 0:
				t.Errorf("the API refuses a channel the engine takes")
			case !refused && len(faults) > 0:
				t.Errorf("the API admits a channel the engine refuses: %v", faults)
			}
		})
	}
}
