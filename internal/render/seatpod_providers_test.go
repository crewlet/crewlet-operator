package render

import (
	"slices"
	"testing"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// TestSeatPodProviders holds a seat placed in a seat pool to the providers a
// seat pod can serve: no agent-mode CLI where the executor runs it, since a
// seat pod serves no tool bridge for its runs to call back through (only the
// head of the executor's chain runs as one), and no
// CLI signed in with a credential bundle, which every seat pod would restore
// and refresh on its own; a key in env, the sign-in of Hermes, Pi and
// OpenCode, is as portable as a token. A seat on the data pods may run on either, and an
// agent-mode CLI in a phase that only makes text calls is no bridge's
// business.
func TestSeatPodProviders(t *testing.T) {
	agent := func(in *Input) {
		in.LLMProviders = append(in.LLMProviders, cliProvider(v1alpha1.CLIAgentModeAgent))
	}
	bundled := func(in *Input) {
		p := cliProvider(v1alpha1.CLIAgentModeText)
		p.Spec.CLI.Auth = v1alpha1.CLIAgentAuth{
			CredentialBundleSecretRef: v1alpha1.SecretKeyReference{Name: providerCLI, Key: "bundle"},
		}
		in.LLMProviders = append(in.LLMProviders, p)
	}
	// keyInEnv is a CLI signed in through env, its model provider's key.
	keyInEnv := func(agent v1alpha1.CLIAgentName) func(*Input) {
		return func(in *Input) {
			p := cliProvider(v1alpha1.CLIAgentModeText)
			p.Spec.CLI.Agent = agent
			p.Spec.CLI.Auth = v1alpha1.CLIAgentAuth{Mode: v1alpha1.CLIAgentAuthSubscription}
			p.Spec.CLI.Env = []v1alpha1.SecretEnvVar{{
				Name:      "OPENROUTER_API_KEY",
				ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: providerCLI, Key: "openrouter"}},
			}}
			in.LLMProviders = append(in.LLMProviders, p)
			seatNamed(in, seatBo).Spec.LLM.Default = []v1alpha1.LLMProviderReference{{Name: providerCLI}}
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
			name: "an agent-mode executor in a Shared pool",
			mutate: func(in *Input) {
				agent(in)
				seatNamed(in, seatBo).Spec.LLM.Default = chain(providerCLI)
			},
			want: []string{"Seat/bo spec.llm.default[0].name AgentModeBridgeUnsupported"},
		},
		{
			// Crewlet runs only the head of the executor's chain as an
			// agentic run; a fallback behind it is a text call.
			name: "an agent-mode CLI behind another provider in a PerSeat pool",
			mutate: func(in *Input) {
				agent(in)
				seatNamed(in, seatAna).Spec.LLM.Default = append(chain("claude"), chain(providerCLI)...)
			},
		},
		{
			name: "an agent-mode executor ahead of another provider in a PerSeat pool",
			mutate: func(in *Input) {
				agent(in)
				seatNamed(in, seatAna).Spec.LLM.Default = append(chain(providerCLI), chain("claude")...)
			},
			want: []string{"Seat/ana spec.llm.default[0].name AgentModeBridgeUnsupported"},
		},
		{
			name: "an agent-mode executor on the data pods",
			mutate: func(in *Input) {
				agent(in)
				seatNamed(in, seatCy).Spec.LLM.Default = chain(providerCLI)
			},
		},
		{
			name: "an agent-mode CLI in a phase that makes text calls",
			mutate: func(in *Input) {
				agent(in)
				seatNamed(in, seatBo).Spec.LLM.Review = chain(providerCLI)
			},
		},
		{
			// Every seat with no chain of its own runs on the head of the
			// company's order: each one in a seat pool is refused.
			name: "an agent-mode CLI at the head of the company's order",
			mutate: func(in *Input) {
				agent(in)
				in.Company.Spec.LLM.Order = chain(providerCLI)
			},
			want: []string{
				"Seat/ana spec.llm AgentModeBridgeUnsupported",
				"Seat/bo spec.llm AgentModeBridgeUnsupported",
				"Seat/dee spec.llm AgentModeBridgeUnsupported",
			},
		},
		{
			name: "a bundled login in a phase of a seat in a pool",
			mutate: func(in *Input) {
				bundled(in)
				seatNamed(in, seatDee).Spec.LLM.Auxiliary = chain(providerCLI)
			},
			want: []string{"Seat/dee spec.llm.auxiliary[0].name CLIAuthNotPortable"},
		},
		{
			// Every phase runs on the default chain; the seat is refused
			// once for the provider.
			name: "a bundled login as the default chain of a seat in a pool",
			mutate: func(in *Input) {
				bundled(in)
				seatNamed(in, seatBo).Spec.LLM.Default = chain(providerCLI)
			},
			want: []string{"Seat/bo spec.llm.default[0].name CLIAuthNotPortable"},
		},
		{
			name: "a bundled login on the data pods",
			mutate: func(in *Input) {
				bundled(in)
				seatNamed(in, seatCy).Spec.LLM.Default = chain(providerCLI)
			},
		},
		{
			// A key in env is a ${NAME} each pod resolves from the
			// fleet's store, and nothing refreshes it.
			name:   "Hermes on a key in env in a pool",
			mutate: keyInEnv(v1alpha1.CLIAgentHermes),
		},
		{
			name:   "Pi on a key in env in a pool",
			mutate: keyInEnv(v1alpha1.CLIAgentPi),
		},
		{
			name:   "OpenCode on a key in env in a pool",
			mutate: keyInEnv(v1alpha1.CLIAgentOpenCode),
		},
		{
			name: "a headless token in a pool",
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, cliProvider(v1alpha1.CLIAgentModeText))
				seatNamed(in, seatBo).Spec.LLM.Default = chain(providerCLI)
			},
		},
		{
			// The seat runs nowhere its pool's pods could fail it: the
			// refusal of its pool says all there is to say.
			name: "a seat refused its pool",
			mutate: func(in *Input) {
				agent(in)
				in.Class.Spec.SeatPools.AllowPerSeat = nil
				seatNamed(in, seatAna).Spec.LLM.Default = chain(providerCLI)
			},
			want: []string{anaPerSeatNotAllowed},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, "seat-pools").input(t)
			// An agent-mode run needs the company's public address.
			in.PublicBaseURL = "https://nimbus.example.com"
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
