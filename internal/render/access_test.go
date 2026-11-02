package render

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// The org-chart case's tool servers and seats the access tests read.
const (
	serverCatalog = "catalog"
	serverArchive = "archive"
	seatWelder    = "welder"
)

// reachedBy is every agent Seat the render counts server as reaching, by
// metadata.name, sorted.
func reachedBy(in *Input, server string) []string {
	r := newRenderer(in)
	s := r.mcp[server]
	var out []string
	for _, seat := range sortedByName(in.Seats) {
		if seat.Spec.Kind != v1alpha1.SeatKindHuman && r.reaches(s, seat) {
			out = append(out, seat.Name)
		}
	}
	return out
}

// TestAToolServerReachesTheSeatsItsAccessNames holds the render's reading of
// an MCPServer's access to Crewlet's MCPServer.Reach on the org-chart case:
// a Unit stands for every agent seat beneath it at any depth (smith, in
// alloys beneath metals), a Seat for itself (the assistant, at the top of the
// chart), exceptSeatRefs leaves out a seat a Unit holds (welder), seatRefs
// wins over it, an empty access reaches nobody, an absent one every agent
// seat, and a person is reached by nothing.
func TestAToolServerReachesTheSeatsItsAccessNames(t *testing.T) {
	agents := []string{"assistant", "caster", "smith", "vp", seatWelder}
	cases := []struct {
		name   string
		mutate func(*Input)
		server string
		want   []string
	}{
		{name: "units, a seat and a seat left out", server: serverCatalog, want: []string{"assistant", "caster", "smith"}},
		{name: "an empty access", server: serverArchive},
		{
			name:   "no access",
			mutate: func(in *Input) { toolServer(in, serverCatalog).Spec.Access = nil },
			server: serverCatalog, want: agents,
		},
		{
			name: "a seat named and left out",
			mutate: func(in *Input) {
				a := toolServer(in, serverCatalog).Spec.Access
				a.SeatRefs = append(a.SeatRefs, v1alpha1.LocalObjectReference{Name: seatWelder})
			},
			server: serverCatalog, want: []string{"assistant", "caster", "smith", seatWelder},
		},
		{
			name: "the top of the chart",
			mutate: func(in *Input) {
				toolServer(in, serverCatalog).Spec.Access = &v1alpha1.MCPServerAccess{UnitRefs: localRefs("rnd")}
			},
			server: serverCatalog, want: []string{"caster", "smith", "vp", seatWelder},
		},
		{
			name: "a person named",
			mutate: func(in *Input) {
				toolServer(in, serverCatalog).Spec.Access = &v1alpha1.MCPServerAccess{SeatRefs: localRefs("wile")}
			},
			server: serverCatalog,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, "org-chart").input(t)
			if c.mutate != nil {
				c.mutate(in)
			}
			if got := reachedBy(in, c.server); !slices.Equal(got, c.want) {
				t.Errorf("%s reaches %v, want %v", c.server, got, c.want)
			}
		})
	}
}

// TestContractAToolServerReachesWhomTheEngineGrants applies the sample company,
// with a tool server of every shape of grant beside its own, to a running
// engine, and holds the seats the render counts each server as reaching
// (markStarted's reaches) to the ones the engine's GET /mcp-servers/{name}
// says it reaches: a server with no access, an empty one, units with and
// without a seat left out, a seat, one turned off, and one declaring the
// seat credentials it reads, which the seats on at that integration's lane
// get. The sample's GitHub server reads a seat credential the engine
// recognises by its address, which the render cannot see: it counts every
// agent seat there, and the engine's seats must be among them.
func TestContractAToolServerReachesWhomTheEngineGrants(t *testing.T) {
	in := companyCaseNamed(t, sampleCompany).input(t)
	probe := func(name string, shared, enabled bool, access *v1alpha1.MCPServerAccess) v1alpha1.MCPServer {
		return v1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: in.Company.Namespace},
			Spec: v1alpha1.MCPServerSpec{
				CompanyRef: v1alpha1.CompanyReference{Name: in.Company.Name},
				Transport:  v1alpha1.MCPTransportStdio, Command: "probe-mcp",
				Shared: new(shared), Enabled: new(enabled), Access: access,
			},
		}
	}
	chat := probe("chat", false, true, nil)
	chat.Spec.SeatCredentials = []v1alpha1.MCPSeatCredential{{Kind: v1alpha1.SeatCredentialSlack, Key: "SLACK_MCP_XOXB_TOKEN"}}
	in.MCPServers = append(in.MCPServers,
		probe("every-agent", true, true, nil),
		probe("nobody", true, true, &v1alpha1.MCPServerAccess{}),
		probe("engineering", false, true, &v1alpha1.MCPServerAccess{UnitRefs: localRefs(unitEngineering)}),
		probe("platform", true, true, &v1alpha1.MCPServerAccess{UnitRefs: localRefs(unitPlatform)}),
		probe("cto-only", false, true, &v1alpha1.MCPServerAccess{SeatRefs: localRefs(seatCTO)}),
		probe("all-but-ana", true, true, &v1alpha1.MCPServerAccess{
			UnitRefs: localRefs(unitEngineering), ExceptSeatRefs: localRefs(seatAna),
		}),
		probe("off", true, false, &v1alpha1.MCPServerAccess{SeatRefs: localRefs(seatAna)}),
		chat,
	)
	toolServer(in, sampleGitLab).Spec.SeatCredentials = []v1alpha1.MCPSeatCredential{
		{Kind: v1alpha1.SeatCredentialGitLab, Key: "GITLAB_TOKEN"},
	}
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}

	e := enginetest.Start(t)
	client := e.Client()
	for _, k := range doc.Refs {
		if err := client.PutSecret(t.Context(), secretsync.StoreName(k), []byte(dummyCredential(k))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.ApplyConfig(t.Context(), engine.ApplyRequest{Document: doc.YAML, CreateOnly: true, Summary: "grants"}); err != nil {
		t.Fatalf("applying the company document: %v", err)
	}
	for deadline := time.Now().Add(time.Minute); client.Ready(t.Context()) != nil; {
		if time.Now().After(deadline) {
			t.Fatalf("the engine never became ready: %v", client.Ready(t.Context()))
		}
		time.Sleep(200 * time.Millisecond)
	}

	r := newRenderer(in)
	for _, s := range sortedByName(in.MCPServers) {
		t.Run(s.Name, func(t *testing.T) {
			var want []string
			for _, seat := range sortedByName(in.Seats) {
				if seat.Spec.Kind != v1alpha1.SeatKindHuman && r.reaches(s, seat) {
					want = append(want, SeatHandle(seat))
				}
			}
			got := engineReaches(t, e, s.Name)
			if s.Name == sampleGitHub {
				for _, handle := range got {
					if !slices.Contains(want, handle) {
						t.Errorf("the engine grants %s to %s, which the render does not count among %v", s.Name, handle, want)
					}
				}
				return
			}
			if !slices.Equal(got, want) {
				t.Errorf("the engine grants %s to %v, and the render counts %v", s.Name, got, want)
			}
		})
	}
}

// engineReaches is the handles of the agent seats e's GET /mcp-servers/{name}
// says server reaches, sorted.
func engineReaches(t *testing.T, e *enginetest.Engine, server string) []string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, e.URL+"/mcp-servers/"+server, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+e.OperatorToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var detail struct {
		Reaches []struct {
			Seat string `json:"seat"`
		} `json:"reaches"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /mcp-servers/%s: %d, %v", server, resp.StatusCode, err)
	}
	var out []string
	for _, reach := range detail.Reaches {
		out = append(out, reach.Seat)
	}
	slices.Sort(out)
	return out
}

// TestContractAccessRefusalsReachTheirObject has the engine's dry run refuse
// what it alone refuses of a tool server's grant, as no object's own spec
// shows it and the render does not copy the engine's recognition of a server
// by its command or address, and checks that the document attributes every
// problem to the object that holds the setting, which is where the controller
// reports it (Invalid): a Seat's spec.mcp entry or its box's mcpServerRefs
// entry for a server that does not reach the seat, a Unit's spec.mcp entry for
// one that reaches none of its own agent seats, and access on a server the
// engine recognises as reading a vendor's per-seat credential.
func TestContractAccessRefusalsReachTheirObject(t *testing.T) {
	client := enginetest.Start(t).Client()
	ctoOnly := func(in *Input) {
		toolServer(in, sampleLinear).Spec.Access = &v1alpha1.MCPServerAccess{SeatRefs: localRefs(seatCTO)}
	}
	for _, c := range []struct {
		name   string
		mutate func(*Input)
		want   ObjectRef
	}{
		{
			name:   "a seat's values for a server not granted to it",
			mutate: ctoOnly,
			want:   ObjectRef{Kind: KindSeat, Name: seatAna},
		},
		{
			name: "a unit's values for a server granted to none of its seats",
			mutate: func(in *Input) {
				ctoOnly(in)
				unit(in, unitPlatform).Spec.MCP = seat(in, seatAna).Spec.MCP
				seat(in, seatAna).Spec.MCP = nil
			},
			want: ObjectRef{Kind: KindUnit, Name: unitPlatform},
		},
		{
			name: "a box given a server not granted to its seat",
			mutate: func(in *Input) {
				ctoOnly(in)
				seat(in, seatAna).Spec.MCP = nil
				seat(in, seatAna).Spec.Sandbox.MCPServerRefs = localRefs(sampleGitHub, sampleLinear)
			},
			want: ObjectRef{Kind: KindSeat, Name: seatAna},
		},
		{
			name: "a grant on a server the engine recognises as reading a seat credential",
			mutate: func(in *Input) {
				toolServer(in, sampleGitLab).Spec.Access = &v1alpha1.MCPServerAccess{SeatRefs: localRefs(seatAna)}
			},
			want: ObjectRef{Kind: KindMCPServer, Name: sampleGitLab},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, sampleCompany).input(t)
			c.mutate(in)
			doc, err := Company(in)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ApplyConfig(t.Context(), engine.ApplyRequest{Document: doc.YAML, DryRun: true})
			verr, ok := errors.AsType[*engine.ValidationError](err)
			if !ok {
				t.Fatalf("the dry run answered %v, want a refusal", err)
			}
			if len(verr.Problems) == 0 {
				t.Fatalf("the refusal names no problem: %v", verr)
			}
			for _, p := range verr.Problems {
				if got, ok := doc.Attribute(PathOf(p.Segments)); !ok || got != c.want {
					t.Errorf("the engine's problem at %q (%s) is attributed to %v, %v; want %v", p.Path, p.Message, got, ok, c.want)
				}
			}
		})
	}
}
