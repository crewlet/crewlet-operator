package render

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The seats of the seat-pools company, one in each kind of place.
const (
	seatBo   = "bo"
	seatCy   = "cy"
	seatDee  = "dee"
	poolData = v1alpha1.DataPool
)

// The pools of the seat-pools company, and the scratch limit its class gives
// them.
const (
	poolGeneral  = "general"
	poolBatch    = "batch"
	poolIsolated = "isolated"
	classScratch = "4Gi"
)

// rolePlacements reads each role's placement labels out of a rendered company
// document, by handle; a role with no placement maps to nil.
func rolePlacements(t *testing.T, document []byte) map[string]map[string]string {
	t.Helper()
	var doc struct {
		Roles []struct {
			Handle    string `json:"handle"`
			Placement *struct {
				Node   string            `json:"node"`
				Labels map[string]string `json:"labels"`
			} `json:"placement"`
		} `json:"roles"`
	}
	if err := yaml.Unmarshal(document, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]string{}
	for _, r := range doc.Roles {
		out[r.Handle] = nil
		if r.Placement != nil {
			if r.Placement.Node != "" {
				t.Errorf("role %s is pinned to node %s: a pin names a label, never a node", r.Handle, r.Placement.Node)
			}
			out[r.Handle] = r.Placement.Labels
		}
	}
	return out
}

// TestSeatsArePinnedToTheirPools pins the placement each kind of seat is
// rendered with: the data pool's label on the data pods, a Shared pool's
// label in it (the company's default pool included), the seat's own label in
// a PerSeat pool, and none for a person, whom the engine never runs.
func TestSeatsArePinnedToTheirPools(t *testing.T) {
	doc, err := Company(companyCaseNamed(t, "seat-pools").input(t))
	if err != nil {
		t.Fatal(err)
	}
	got := rolePlacements(t, doc.YAML)
	want := map[string]map[string]string{
		seatAna:  {SeatLabel: seatAna},
		seatBo:   {PoolLabel: poolGeneral},
		seatCy:   {PoolLabel: poolData},
		seatDee:  {PoolLabel: poolBatch},
		seatJane: nil,
	}
	if !maps.EqualFunc(got, want, maps.Equal) || len(got) != len(want) {
		t.Fatalf("placements = %v, want %v", got, want)
	}
}

// TestEveryPodMatchesExactlyOnePlacementGroup renders every kind of pod the
// seat-pools company runs (the data pod, a Shared pool's pod, a PerSeat
// seat's pod) and every seat's pin, and holds them to the rule the
// placement depends on: each pod carries exactly one placement label, so it
// is in exactly one placement group, and each agent seat's pin matches the
// pods of its own pool and no other pod. A pod in two groups can be filled
// from one and leave the other's seat held by nobody.
func TestEveryPodMatchesExactlyOnePlacementGroup(t *testing.T) {
	in := companyCaseNamed(t, "seat-pools").input(t)
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	pins := rolePlacements(t, doc.YAML)
	topology, err := PlanTopology(in.Company, in.Class, in.Seats)
	if err != nil {
		t.Fatal(err)
	}

	// Every pod the company runs, by the pool it belongs to, with its
	// node.labels as the engine reads them: references resolved from the
	// pod's environment.
	type pod struct {
		pool, seat string
		labels     map[string]string
	}
	keyring := Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": testKeyMaterial}}
	data, err := Bootstrap(&BootstrapInput{
		Company: in.Company, Class: in.Class, Keyring: keyring, Tokens: testTokens,
		VolumeSize: resource.MustParse("10Gi"),
	})
	if err != nil {
		t.Fatal(err)
	}
	pods := []pod{{pool: poolData, labels: nodeLabels(t, data)}}
	for i := range topology.Pools {
		pool := &topology.Pools[i]
		seats := []string{""}
		if pool.Mode == v1alpha1.SeatPoolPerSeat {
			seats = pool.Seats
		}
		for _, seat := range seats {
			doc, err := SeatPodBootstrap(&SeatPodInput{
				Company: in.Company, Class: in.Class, Pool: pool, Seat: seat, Keyring: keyring, DataMembers: testDataMembers,
			})
			if err != nil {
				t.Fatal(err)
			}
			pods = append(pods, pod{pool: pool.Name, seat: seat, labels: nodeLabels(t, doc)})
		}
	}

	for _, p := range pods {
		if len(p.labels) != 1 {
			t.Errorf("a pod of pool %s carries the placement labels %v; it must carry exactly one", p.pool, p.labels)
		}
	}
	for handle, pin := range pins {
		if pin == nil {
			continue
		}
		var matched []string
		for _, p := range pods {
			if matches(pin, p.labels) {
				matched = append(matched, p.pool+"/"+p.seat)
			}
		}
		var s *v1alpha1.Seat
		for i := range in.Seats {
			if SeatHandle(&in.Seats[i]) == handle {
				s = &in.Seats[i]
			}
		}
		pool := SeatPool(in.Company, s)
		want := pool + "/"
		if p := topology.Pool(pool); p != nil && p.Mode == v1alpha1.SeatPoolPerSeat {
			want += handle
		}
		if !slices.Equal(matched, []string{want}) {
			t.Errorf("seat %s, placed in pool %s, is pinned to %v, which the pods %v match; want the pods %s alone",
				handle, pool, pin, matched, want)
		}
	}
}

// nodeLabels reads a Tier A's node.labels with each ${NAME} resolved from the
// pod's environment, as the engine resolves them.
func nodeLabels(t *testing.T, doc *BootstrapDocument) map[string]string {
	t.Helper()
	var tierA struct {
		Node struct {
			Labels map[string]string `json:"labels"`
		} `json:"node"`
	}
	if err := yaml.Unmarshal(doc.YAML, &tierA); err != nil {
		t.Fatal(err)
	}
	env := bootstrapEnv(doc)
	for k, v := range tierA.Node.Labels {
		tierA.Node.Labels[k] = envPointer.ReplaceAllStringFunc(v, func(ref string) string {
			return env[envPointer.FindStringSubmatch(ref)[1]]
		})
	}
	return tierA.Node.Labels
}

// matches is Crewlet's placement rule: every label of the pin is on the node,
// with the same value.
func matches(pin, labels map[string]string) bool {
	for k, v := range pin {
		if got, ok := labels[k]; !ok || got != v {
			return false
		}
	}
	return true
}

// TestPlanTopologySizesEachPool resolves what a pool leaves unset from the
// class's defaults and then from the operator's own, and counts the pods the
// pools run, a PerSeat pool's for each of its seats.
func TestPlanTopologySizesEachPool(t *testing.T) {
	in := companyCaseNamed(t, "seat-pools").input(t)
	topology, err := PlanTopology(in.Company, in.Class, in.Seats)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(topology.DataSeats, []string{seatCy}) {
		t.Errorf("data seats = %v, want [%s]", topology.DataSeats, seatCy)
	}
	type sizing struct {
		mode               v1alpha1.SeatPoolMode
		replicas           int32
		pods               int
		seats              string
		maxConcurrentTurns int32
		grace              int64
		scratch            string
	}
	want := map[string]sizing{
		// The class's defaults.
		poolGeneral: {mode: v1alpha1.SeatPoolShared, replicas: 2, pods: 2, seats: seatBo, maxConcurrentTurns: 4, grace: 600, scratch: classScratch},
		poolIsolated: {
			mode: v1alpha1.SeatPoolPerSeat, replicas: 2, pods: 2, seats: seatAna, maxConcurrentTurns: 4, grace: 600, scratch: classScratch,
		},
		// The pool's own, over the class's.
		poolBatch: {mode: v1alpha1.SeatPoolShared, replicas: 0, pods: 0, seats: seatDee, maxConcurrentTurns: 8, grace: 120, scratch: classScratch},
	}
	for name, w := range want {
		p := topology.Pool(name)
		if p == nil {
			t.Fatalf("no pool %s", name)
		}
		got := sizing{
			mode: p.Mode, replicas: p.Replicas, pods: p.Pods(), seats: strings.Join(p.Seats, ","),
			maxConcurrentTurns: p.MaxConcurrentTurns, grace: p.TerminationGracePeriodSeconds, scratch: p.ScratchSizeLimit.String(),
		}
		if got != w {
			t.Errorf("pool %s = %+v, want %+v", name, got, w)
		}
	}
	if got := topology.Pods(); got != 4 {
		t.Errorf("the pools run %d pods, want 4", got)
	}

	// A class with no defaults: the engine's turn limit, and the
	// operator's grace period and scratch limit.
	in.Class.Spec.SeatPools.Defaults = v1alpha1.SeatPoolDefaults{}
	topology, err = PlanTopology(in.Company, in.Class, in.Seats)
	if err != nil {
		t.Fatal(err)
	}
	p := topology.Pool(poolGeneral)
	if p.MaxConcurrentTurns != 0 || p.TerminationGracePeriodSeconds != DefaultSeatPodTerminationGracePeriodSeconds ||
		p.ScratchSizeLimit.String() != DefaultSeatPodScratchSizeLimit {
		t.Errorf("pool general with no defaults = %+v, want no turn limit, %ds and %s", p,
			DefaultSeatPodTerminationGracePeriodSeconds, DefaultSeatPodScratchSizeLimit)
	}
}

// refusalsOf is each RefError's object, field and reason, for comparison.
func refusalsOf(err error) []string {
	refusals := RefErrors(err)
	out := make([]string, 0, len(refusals))
	for _, r := range refusals {
		out = append(out, fmt.Sprintf("%s/%s %s %s", r.Kind, r.Name, r.Field, r.Reason))
	}
	return out
}

// TestPlacementRefusals covers every seat that cannot run where it is
// placed, and every topology the class does not admit. Each is refused
// both when the topology is planned (for the workloads) and when the document
// is rendered, on the object a person has to change.
func TestPlacementRefusals(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(*Input)
		want   []string
	}{
		{
			name: "a seat in a pool the company does not declare",
			mutate: func(in *Input) {
				seatNamed(in, seatDee).Spec.Placement.Pool = "nowhere"
			},
			want: []string{"Seat/dee spec.placement.pool PoolNotFound"},
		},
		{
			name: "seat pools under a class that forbids them",
			mutate: func(in *Input) {
				in.Class.Spec.SeatPools = v1alpha1.SeatPoolSettings{Policy: v1alpha1.SeatPoolsForbidden}
			},
			want: []string{"Company/nimbus spec.topology.seatPools SeatPoolsNotAllowed"},
		},
		{
			name:   "seat pools under a class that says nothing about them",
			mutate: func(in *Input) { in.Class.Spec.SeatPools = v1alpha1.SeatPoolSettings{} },
			want:   []string{"Company/nimbus spec.topology.seatPools SeatPoolsNotAllowed"},
		},
		{
			name:   "a seat in a PerSeat pool under a class that does not allow one",
			mutate: func(in *Input) { in.Class.Spec.SeatPools.AllowPerSeat = nil },
			want:   []string{"Seat/ana spec.placement.pool PerSeatNotAllowed"},
		},
		{
			// general's 2 pods and ana's 2 are 4.
			name:   "a PerSeat seat whose pods pass maxPods",
			mutate: func(in *Input) { in.Class.Spec.SeatPools.MaxPods = 3 },
			want:   []string{"Seat/ana spec.placement.pool PoolCapacityExceeded"},
		},
		{
			name:   "Shared pools whose pods alone pass maxPods",
			mutate: func(in *Input) { in.Class.Spec.SeatPools.MaxPods = 1 },
			want:   []string{"Company/nimbus spec.topology.seatPools PoolCapacityExceeded"},
		},
		{
			name:   "pods exactly at maxPods",
			mutate: func(in *Input) { in.Class.Spec.SeatPools.MaxPods = 4 },
		},
		{
			// A seat refused for its pool takes no pod, so ana's two do not
			// count against the cap.
			name: "a refused PerSeat seat takes no pod",
			mutate: func(in *Input) {
				in.Class.Spec.SeatPools.AllowPerSeat = nil
				in.Class.Spec.SeatPools.MaxPods = 2
			},
			want: []string{"Seat/ana spec.placement.pool PerSeatNotAllowed"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, "seat-pools").input(t)
			c.mutate(in)
			_, planErr := PlanTopology(in.Company, in.Class, in.Seats)
			_, renderErr := Company(in)
			for what, err := range map[string]error{"PlanTopology": planErr, "Company": renderErr} {
				got := refusalsOf(err)
				if !slices.Equal(got, c.want) {
					t.Errorf("%s refused %v (%v), want %v", what, got, err, c.want)
				}
			}
		})
	}
}

// seatNamed is the Seat of that name in an input.
func seatNamed(in *Input, name string) *v1alpha1.Seat {
	for i := range in.Seats {
		if in.Seats[i].Name == name {
			return &in.Seats[i]
		}
	}
	panic("no Seat " + name)
}
