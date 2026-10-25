package render

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	doc, err := Company(companyCaseNamed(t, caseSeatPools).input(t))
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
// pods of its own pool and no other pod: a pin matching another pool's pod
// could run the seat where it was not placed.
func TestEveryPodMatchesExactlyOnePlacementGroup(t *testing.T) {
	in := companyCaseNamed(t, caseSeatPools).input(t)
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
	data, err := Bootstrap(reachedAtDefaults(&BootstrapInput{
		Company: in.Company, Class: in.Class, Keyring: keyring, Tokens: testTokens,
		VolumeSize: resource.MustParse("16Gi"),
	}))
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
	in := companyCaseNamed(t, caseSeatPools).input(t)
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

// TestPlacementRefusals covers every seat its class does not let run where it
// is placed, and every topology the class does not admit. Each is refused
// both when the topology is planned (for the workloads) and when the document
// is rendered, on the object a person has to change. The plan still comes
// with the refusal, less every Seat refused on its own: the runtime runs the
// pools from it while the document is blocked. A seat in a pool the Company
// does not declare is in no pool of the plan, and refused nowhere.
func TestPlacementRefusals(t *testing.T) {
	const (
		// Every seat where the seat-pools company places it.
		everySeat = "batch:dee data:cy general:bo isolated:ana"
		// Every seat but ana, refused its PerSeat pool.
		anaRefused = "batch:dee data:cy general:bo isolated:"
	)
	for _, c := range []struct {
		name    string
		mutate  func(*Input)
		want    []string
		planned string
	}{
		{
			// It runs in no pool, and is reported rather than refused
			// (TestCompanyReportsWhatItRendersPast).
			name: "a seat in a pool the company does not declare",
			mutate: func(in *Input) {
				seatNamed(in, seatDee).Spec.Placement.Pool = "nowhere"
			},
			planned: "batch: data:cy general:bo isolated:ana",
		},
		{
			// The Company is refused, not its Seats.
			name: "seat pools under a class that forbids them",
			mutate: func(in *Input) {
				in.Class.Spec.SeatPools = v1alpha1.SeatPoolSettings{Policy: v1alpha1.SeatPoolsForbidden}
			},
			want:    []string{"Company/nimbus spec.topology.seatPools SeatPoolsNotAllowed"},
			planned: everySeat,
		},
		{
			name:    "seat pools under a class that says nothing about them",
			mutate:  func(in *Input) { in.Class.Spec.SeatPools = v1alpha1.SeatPoolSettings{} },
			want:    []string{"Company/nimbus spec.topology.seatPools SeatPoolsNotAllowed"},
			planned: everySeat,
		},
		{
			name:    "a seat in a PerSeat pool under a class that does not allow one",
			mutate:  func(in *Input) { in.Class.Spec.SeatPools.AllowPerSeat = nil },
			want:    []string{anaPerSeatNotAllowed},
			planned: anaRefused,
		},
		{
			// general's 2 pods and ana's 2 are 4.
			name:    "a PerSeat seat whose pods pass maxPods",
			mutate:  func(in *Input) { in.Class.Spec.SeatPools.MaxPods = 3 },
			want:    []string{anaOverCap},
			planned: anaRefused,
		},
		{
			name:    "Shared pools whose pods alone pass maxPods",
			mutate:  func(in *Input) { in.Class.Spec.SeatPools.MaxPods = 1 },
			want:    []string{"Company/nimbus spec.topology.seatPools PoolCapacityExceeded"},
			planned: everySeat,
		},
		{
			name:    "pods exactly at maxPods",
			mutate:  func(in *Input) { in.Class.Spec.SeatPools.MaxPods = 4 },
			planned: everySeat,
		},
		{
			// A seat refused for its pool takes no pod, so ana's two do not
			// count against the cap.
			name: "a refused PerSeat seat takes no pod",
			mutate: func(in *Input) {
				in.Class.Spec.SeatPools.AllowPerSeat = nil
				in.Class.Spec.SeatPools.MaxPods = 2
			},
			want:    []string{anaPerSeatNotAllowed},
			planned: anaRefused,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, caseSeatPools).input(t)
			c.mutate(in)
			plan, planErr := PlanTopology(in.Company, in.Class, in.Seats)
			_, renderErr := Company(in)
			for what, err := range map[string]error{"PlanTopology": planErr, "Company": renderErr} {
				got := refusalsOf(err)
				if !slices.Equal(got, c.want) {
					t.Errorf("%s refused %v (%v), want %v", what, got, err, c.want)
				}
			}
			if got := planText(plan); got != c.planned {
				t.Errorf("planned %q, want %q", got, c.planned)
			}
		})
	}
}

// The refusals of seats ana and eve, whose PerSeat pods the class's maxPods
// does not admit.
const (
	anaOverCap = "Seat/ana spec.placement.pool PoolCapacityExceeded"
	eveOverCap = "Seat/eve spec.placement.pool PoolCapacityExceeded"
)

// anaPerSeatNotAllowed is the refusal of seat ana, in the seat-pools
// company's PerSeat pool, under a class that allows no PerSeat pool.
const anaPerSeatNotAllowed = "Seat/ana spec.placement.pool PerSeatNotAllowed"

// planText is each pool of a plan, data included, with its seats, by pool
// name: "data:cy general:bo,dee".
func planText(t *Topology) string {
	pools := make([]string, 0, 1+len(t.Pools))
	pools = append(pools, poolData+":"+strings.Join(t.DataSeats, ","))
	for i := range t.Pools {
		pools = append(pools, t.Pools[i].Name+":"+strings.Join(t.Pools[i].Seats, ","))
	}
	slices.Sort(pools)
	return strings.Join(pools, " ")
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

// TestPerSeatCapacityRefusesOnlyTheSeatsPastIt holds the class's maxPods to
// the PerSeat seats that pass it: they are admitted oldest first, then by
// handle, and only the ones whose pods would take the company past the cap
// are refused, so that adding a seat never refuses those already running.
func TestPerSeatCapacityRefusesOnlyTheSeatsPastIt(t *testing.T) {
	// eve is a second seat of the PerSeat pool, beside ana.
	const seatEve = "eve"
	at := func(minute int) metav1.Time { return metav1.NewTime(time.Date(2026, 10, 9, 8, minute, 0, 0, time.UTC)) }
	for _, c := range []struct {
		name    string
		created map[string]metav1.Time
		want    []string
		planned string
	}{
		{
			name:    "the newest seat passes the cap",
			created: map[string]metav1.Time{seatAna: at(0), seatEve: at(5)},
			want:    []string{eveOverCap},
			planned: "batch:dee data:cy general:bo isolated:ana",
		},
		{
			name:    "an older seat comes first whatever its handle",
			created: map[string]metav1.Time{seatAna: at(5), seatEve: at(0)},
			want:    []string{anaOverCap},
			planned: "batch:dee data:cy general:bo isolated:eve",
		},
		{
			name:    "seats created together, by handle",
			created: map[string]metav1.Time{seatAna: at(0), seatEve: at(0)},
			want:    []string{eveOverCap},
			planned: "batch:dee data:cy general:bo isolated:ana",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, caseSeatPools).input(t)
			eve := seatNamed(in, seatAna).DeepCopy()
			eve.Name = seatEve
			in.Seats = append(in.Seats, *eve)
			for name, created := range c.created {
				seatNamed(in, name).CreationTimestamp = created
			}
			// general's 2 pods and one PerSeat seat's 2 are 4; a second's
			// would make 6.
			in.Class.Spec.SeatPools.MaxPods = 5
			plan, err := PlanTopology(in.Company, in.Class, in.Seats)
			if got := refusalsOf(err); !slices.Equal(got, c.want) {
				t.Errorf("refused %v, want %v", got, c.want)
			}
			if got := planText(plan); got != c.planned {
				t.Errorf("planned %q, want %q", got, c.planned)
			}
		})
	}
}

// TestAHeldSeatKeepsItsRunningPin renders a seat whose move is held back with
// the pin it runs under, and every other seat where it is placed: the
// controller holds a seat's pin until its new pool has a ready pod.
func TestAHeldSeatKeepsItsRunningPin(t *testing.T) {
	in := companyCaseNamed(t, caseSeatPools).input(t)
	running := map[string]string{SeatLabel: seatAna}
	in.HeldPlacements = map[string]map[string]string{seatBo: running}
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	placements := rolePlacements(t, doc.YAML)
	if !maps.Equal(placements[seatBo], running) {
		t.Errorf("held seat bo is pinned to %v, want %v", placements[seatBo], running)
	}
	if want := map[string]string{PoolLabel: poolBatch}; !maps.Equal(placements[seatDee], want) {
		t.Errorf("seat dee, not held, is pinned to %v, want %v", placements[seatDee], want)
	}
	got, err := RolePlacements(doc.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(got[seatBo], running) || len(got) != 4 {
		t.Errorf("RolePlacements read %v, want the four agent seats' pins", got)
	}
}

// TestPerSeatCapacityAdmitsTheRunningSeatsFirst holds the order the PerSeat
// seats are admitted in to the one that blames the change: the seats the
// running revision already runs in pods of their own come before every other,
// however old, and the first seat that does not fit ends the admission, so a
// seat behind it in a smaller pool is refused too rather than slipped in.
func TestPerSeatCapacityAdmitsTheRunningSeatsFirst(t *testing.T) {
	const (
		seatEve  = "eve"
		poolSolo = "solo"
	)
	at := func(minute int) metav1.Time { return metav1.NewTime(time.Date(2026, 10, 9, 8, minute, 0, 0, time.UTC)) }
	for _, c := range []struct {
		name    string
		mutate  func(*Input)
		want    []string
		planned string
	}{
		{
			// cy is older than ana, and moved into isolated while ana runs
			// there: general's 2 pods and ana's 2 are 4, cy's would make 6.
			name: "an older seat moved into a full pool beside a running one",
			mutate: func(in *Input) {
				in.Company.Status.Config.Placement.Seats = []string{seatAna}
				seatNamed(in, seatCy).Spec.Placement.Pool = poolIsolated
				seatNamed(in, seatCy).CreationTimestamp = at(0)
				seatNamed(in, seatAna).CreationTimestamp = at(5)
			},
			want:    []string{"Seat/cy spec.placement.pool PoolCapacityExceeded"},
			planned: "batch:dee data: general:bo isolated:ana",
		},
		{
			// The control: with nothing running in pods of its own, the
			// older seat comes first.
			name: "the same seats before either runs",
			mutate: func(in *Input) {
				seatNamed(in, seatCy).Spec.Placement.Pool = poolIsolated
				seatNamed(in, seatCy).CreationTimestamp = at(0)
				seatNamed(in, seatAna).CreationTimestamp = at(5)
			},
			want:    []string{anaOverCap},
			planned: "batch:dee data: general:bo isolated:cy",
		},
		{
			// ana's 2 make 4; cy's 2 would make 6 and are refused; eve's
			// one pod would fit in 5, and is refused all the same.
			name: "the first seat that does not fit ends the admission",
			mutate: func(in *Input) {
				in.Company.Spec.Topology.SeatPools = append(in.Company.Spec.Topology.SeatPools, v1alpha1.SeatPool{
					Name: poolSolo, Mode: v1alpha1.SeatPoolPerSeat, Replicas: new(int32(1)),
				})
				eve := seatNamed(in, seatAna).DeepCopy()
				eve.Name = seatEve
				eve.Spec.Placement.Pool = poolSolo
				eve.CreationTimestamp = at(10)
				in.Seats = append(in.Seats, *eve)
				seatNamed(in, seatAna).CreationTimestamp = at(0)
				seatNamed(in, seatCy).Spec.Placement.Pool = poolIsolated
				seatNamed(in, seatCy).CreationTimestamp = at(5)
			},
			want: []string{
				"Seat/cy spec.placement.pool PoolCapacityExceeded",
				eveOverCap,
			},
			planned: "batch:dee data: general:bo isolated:ana solo:",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, caseSeatPools).input(t)
			in.Class.Spec.SeatPools.MaxPods = 5
			c.mutate(in)
			plan, err := PlanTopology(in.Company, in.Class, in.Seats)
			if got := refusalsOf(err); !slices.Equal(got, c.want) {
				t.Errorf("refused %v (%v), want %v", got, err, c.want)
			}
			if got := planText(plan); got != c.planned {
				t.Errorf("planned %q, want %q", got, c.planned)
			}
		})
	}
}

// TestAnUndeclaredPoolPinsNoPod holds a seat placed in a pool the Company
// does not declare to a pin no pod can carry, now or once the pool is
// declared, which the document records as no pool. A pin to the pool's own
// label would let the engine place the seat on the pods of a pool declared
// later under the revision it already runs, before a revision placing it
// there had been through the render's checks of what those pods can run: a
// seat on a CLI signed in with a credential bundle would be restored and
// refreshed on a seat pod, logging every seat on that login out. And a pool
// the document records would keep a StatefulSet for it, and open the data
// pods' leaf listener under a class that runs no seat pods.
func TestAnUndeclaredPoolPinsNoPod(t *testing.T) {
	const later = "later"
	in := companyCaseNamed(t, caseSeatPools).input(t)
	bundled := cliProvider(v1alpha1.CLIAgentModeHost)
	bundled.Spec.Auth = v1alpha1.CLIAgentAuth{
		CredentialBundleSecretRef: v1alpha1.SecretKeyReference{Name: providerCLI, Key: "bundle"},
	}
	in.LLMProviders = append(in.LLMProviders, bundled)
	bo := seatNamed(in, seatBo)
	bo.Spec.LLM.Default = []v1alpha1.LLMProviderReference{{Name: providerCLI}}
	bo.Spec.Placement.Pool = later

	doc, err := Company(in)
	if err != nil {
		t.Fatalf("a seat in a pool the Company does not declare holds the document: %v", err)
	}
	pin := rolePlacements(t, doc.YAML)[seatBo]
	if want := map[string]string{UndeclaredPoolLabel: later}; !maps.Equal(pin, want) {
		t.Fatalf("seat %s is pinned to %v, want %v", seatBo, pin, want)
	}
	placement, err := PlacementOf(doc.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(placement.Pools, later) {
		t.Errorf("the document records pool %s, which the Company does not declare: %v", later, placement.Pools)
	}

	// The pool declared: its pods carry its label, and the seat's pin
	// does not match them.
	in.Company.Spec.Topology.SeatPools = append(in.Company.Spec.Topology.SeatPools,
		v1alpha1.SeatPool{Name: later, Mode: v1alpha1.SeatPoolShared})
	topology, _ := PlanTopology(in.Company, in.Class, in.Seats)
	keyring := Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": testKeyMaterial}}
	pod, err := SeatPodBootstrap(&SeatPodInput{
		Company: in.Company, Class: in.Class, Pool: topology.Pool(later), Keyring: keyring, DataMembers: testDataMembers,
	})
	if err != nil {
		t.Fatal(err)
	}
	if labels := nodeLabels(t, pod); matches(pin, labels) {
		t.Errorf("a pod of pool %s, once declared, carries %v, which the pin %v of the revision before matches", later, labels, pin)
	}
	// And the revision that would place the seat there is refused.
	_, err = Company(in)
	if got := refusalsOf(err); !slices.Equal(got, []string{"Seat/bo spec.llm.default[0].name CLIAuthNotPortable"}) {
		t.Errorf("with pool %s declared, refused %v (%v), want the bundled login refused on its pods", later, got, err)
	}

	// Under a class that runs no seat pods, the pin keeps the data pods'
	// leaf listener closed.
	in = companyCaseNamed(t, caseSeatPools).input(t)
	in.Class.Spec.SeatPools.Policy = v1alpha1.SeatPoolsForbidden
	in.Company.Spec.Topology = v1alpha1.CompanyTopology{}
	for i := range in.Seats {
		in.Seats[i].Spec.Placement.Pool = ""
	}
	seatNamed(in, seatBo).Spec.Placement.Pool = later
	doc, err = Company(in)
	if err != nil {
		t.Fatalf("a seat in a pool the Company does not declare holds the document: %v", err)
	}
	placement, err = PlacementOf(doc.YAML)
	if err != nil {
		t.Fatal(err)
	}
	in.Company.Status.Config.Placement = v1alpha1.RevisionPlacement{Pools: placement.Pools, Seats: placement.Seats}
	if LeafOpen(in.Company, in.Class) {
		t.Errorf("under a class whose seatPools.policy is Forbidden, the revision %+v opens the data pods' leaf listener",
			in.Company.Status.Config.Placement)
	}
}
