package render

import (
	"slices"
	"testing"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// TestSeatPodProviders holds a seat placed in a seat pool to the harness logins a
// seat pod can keep: no harness signed in with a credential bundle, which every
// seat pod would restore and refresh on its own and so log every pod on that
// login out, the data pods' included; a key in env, the sign-in of Hermes, Pi
// and OpenCode, is as portable as a token. A seat on the data pods may run on
// either. Where a seat's executor runs, and whether it can, is ExecutorOf's
// report (TestExecutor).
func TestSeatPodProviders(t *testing.T) {
	bundled := func(in *Input) {
		p := claudeCodeProvider()
		p.Spec.Auth = v1alpha1.HarnessAuth{
			CredentialBundleSecretRef: v1alpha1.SecretKeyReference{Name: providerClaudeCode, Key: "bundle"},
		}
		in.LLMProviders = append(in.LLMProviders, p)
	}
	// keyInEnv is a harness signed in through env, its model provider's key, as
	// the seat's reviewer, a phase every harness serves.
	keyInEnv := func(harness v1alpha1.Harness) func(*Input) {
		return func(in *Input) {
			p := claudeCodeProvider()
			p.Spec.Model = modelOpenCode
			p.Spec.Harness = harness
			p.Spec.Auth = v1alpha1.HarnessAuth{Mode: v1alpha1.HarnessAuthSubscription}
			p.Spec.Env = []v1alpha1.SecretEnvVar{{
				Name:      envGatewayKey,
				ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: providerClaudeCode, Key: openRouter}},
			}}
			in.LLMProviders = append(in.LLMProviders, p)
			seatNamed(in, seatBo).Spec.LLM.Review = []v1alpha1.LLMProviderReference{{Name: providerClaudeCode}}
		}
	}
	chain := func(name string) []v1alpha1.LLMProviderReference {
		return []v1alpha1.LLMProviderReference{{Name: name}}
	}
	for _, c := range []struct {
		name   string
		mutate func(*Input)
		want   []string
	}{
		{
			name: "a bundled login in a phase of a seat in a pool",
			mutate: func(in *Input) {
				bundled(in)
				seatNamed(in, seatDee).Spec.LLM.Auxiliary = chain(providerClaudeCode)
			},
			want: []string{"Seat/dee spec.llm.auxiliary[0].name HarnessAuthNotPortable"},
		},
		{
			// Every phase runs on the default chain; the seat is refused
			// once for the provider.
			name: "a bundled login as the default chain of a seat in a pool",
			mutate: func(in *Input) {
				bundled(in)
				seatNamed(in, seatBo).Spec.LLM.Default = chain(providerClaudeCode)
			},
			want: []string{"Seat/bo spec.llm.default[0].name HarnessAuthNotPortable"},
		},
		{
			name: "a bundled login on the data pods",
			mutate: func(in *Input) {
				bundled(in)
				seatNamed(in, seatCy).Spec.LLM.Default = chain(providerClaudeCode)
			},
		},
		{
			// A key in env is a ${NAME} each pod resolves from the
			// fleet's store, and nothing refreshes it.
			name:   "Hermes on a key in env in a pool",
			mutate: keyInEnv(v1alpha1.HarnessHermes),
		},
		{
			name:   "Pi on a key in env in a pool",
			mutate: keyInEnv(v1alpha1.HarnessPi),
		},
		{
			name:   "OpenCode on a key in env in a pool",
			mutate: keyInEnv(v1alpha1.HarnessOpenCode),
		},
		{
			// Any member of a fleet may take the seat's work, so each one's
			// login is held to the pool, at the field naming the fleet.
			name: "a bundled login as a member of the fleet a seat in a pool runs on",
			mutate: func(in *Input) {
				bundled(in)
				in.Company.Spec.LLM.Fleets = []v1alpha1.LLMFleet{{Name: fleetSonnet, Members: chain(providerClaudeCode)}}
				seatNamed(in, seatBo).Spec.LLM.Default = fleetChain(fleetSonnet)
			},
			want: []string{"Seat/" + seatBo + " " + atDefaultHead + " HarnessAuthNotPortable"},
		},
		{
			// A seat with no chain runs on default-fleet once it has a
			// member, ahead of the Company's model order: every such seat in
			// a pool.
			name: "a bundled login as a member of default-fleet, which the seats in pools with no chain run on",
			mutate: func(in *Input) {
				bundled(in)
				in.Company.Spec.LLM.Fleets = []v1alpha1.LLMFleet{{Name: v1alpha1.DefaultLLMFleet, Members: chain(providerClaudeCode)}}
			},
			want: []string{
				"Seat/ana spec.llm HarnessAuthNotPortable", "Seat/bo spec.llm HarnessAuthNotPortable",
				"Seat/dee spec.llm HarnessAuthNotPortable",
			},
		},
		{
			name: "a headless token in a pool",
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, claudeCodeProvider())
				seatNamed(in, seatBo).Spec.LLM.Default = chain(providerClaudeCode)
			},
		},
		{
			// The seat runs nowhere its pool's pods could fail it: the
			// refusal of its pool says all there is to say.
			name: "a seat refused its pool",
			mutate: func(in *Input) {
				bundled(in)
				in.Class.Spec.SeatPools.AllowPerSeat = nil
				seatNamed(in, seatAna).Spec.LLM.Default = chain(providerClaudeCode)
			},
			want: []string{anaPerSeatNotAllowed},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, caseSeatPools).input(t)
			c.mutate(in)
			_, err := Company(in)
			got := refusalsOf(err)
			slices.Sort(got)
			if !slices.Equal(got, c.want) {
				t.Errorf("refused %v (%v), want %v", got, err, c.want)
			}
		})
	}
}
