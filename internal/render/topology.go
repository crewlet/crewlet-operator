package render

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The node labels that place seats. Crewlet's role.placement is a hard pin:
// a seat runs only on a node whose node.labels carry every label its
// placement names, and never anywhere else. Every agent seat is rendered with
// a placement naming exactly one of these labels, and every pod that runs
// seats carries exactly one of them, so each pod matches exactly one
// placement group.
//
// One group per pod is what makes the pin hold. Crewlet gives a node a fair
// share of each group it matches and sums the shares into one capacity, which
// it fills with whichever eligible seats it reaches first: a pod matching two
// groups can fill its capacity from one of them and leave a seat of the other
// held by nobody, with no warning, because a matching node exists. A pod that
// matches one group never does.
const (
	// PoolLabel places the seats of a pool whose pods share them: the data
	// pods' (data) and each Shared pool's, with the pool's name as its value.
	PoolLabel = "crewlet.ai/pool"
	// SeatLabel places the seat of a PerSeat pool, with the seat's handle as
	// its value: only that seat's own pods carry it.
	SeatLabel = "crewlet.ai/seat"
)

// What a seat pool runs with when neither it nor its class says.
const (
	// DefaultSeatPodTerminationGracePeriodSeconds is a seat pod's grace
	// period: five minutes, as for a data pod. A seat pod finishes its
	// running turns before it exits and puts no bound on that itself, and
	// Crewlet suggests two to five minutes; the Kubernetes default of thirty
	// seconds would kill a turn in the middle.
	DefaultSeatPodTerminationGracePeriodSeconds = 300
	// DefaultSeatPodScratchSizeLimit caps a seat pod's scratch emptyDir:
	// the scratch store (a copy of its seats' memory and its working rows),
	// the engine's native library of about 20 MB, and what the processes it
	// runs write (git checkouts, stdio MCP servers' files). Two gibibytes
	// is ample for the store and the library and leaves the rest to the
	// processes; a class whose seats clone large repositories raises it.
	DefaultSeatPodScratchSizeLimit = "2Gi"
)

// Topology is where a company's agent seats run: on the data pods, or in one
// of its seat pools. It is computed from the Company, its EngineClass and its
// Seats by PlanTopology, and it is what both the company document (each
// seat's role.placement) and the seat pools' workloads are made from, so the
// two never disagree on where a seat runs.
type Topology struct {
	// DataSeats are the handles of the agent seats placed in the pool data,
	// sorted.
	DataSeats []string
	// Pools are the Company's seat pools, in the order spec.topology.seatPools
	// lists them, each with its seats and its sizing resolved.
	Pools []PoolPlan
}

// PoolPlan is one seat pool as it runs: its spec, with what the pool leaves
// unset taken from the class's seatPools.defaults and then from the
// operator's defaults, and the seats placed in it.
type PoolPlan struct {
	Name string
	Mode v1alpha1.SeatPoolMode
	// Replicas is the pool's pods for a Shared pool, and each seat's pods for
	// a PerSeat pool.
	Replicas int32
	// Seats are the handles of the agent seats placed in the pool, sorted.
	Seats []string
	// MaxConcurrentTurns is Crewlet's node.max_concurrent for each of the
	// pool's pods; 0 leaves the engine's default.
	MaxConcurrentTurns int32
	// Resources are the engine container's, or nil for none. It is the
	// object the Company or the class holds; treat it as read-only.
	Resources                     *corev1.ResourceRequirements
	TerminationGracePeriodSeconds int64
	ScratchSizeLimit              resource.Quantity
}

// Pods is how many pods the pool runs: its replicas, or for a PerSeat pool
// its replicas for each of its seats.
func (p *PoolPlan) Pods() int {
	if p.Mode == v1alpha1.SeatPoolPerSeat {
		return int(p.Replicas) * len(p.Seats)
	}
	return int(p.Replicas)
}

// Pods is how many seat pods the company runs, all of its pools together: what
// the class's seatPools.maxPods caps. The data pods are not counted.
func (t *Topology) Pods() int {
	var n int
	for i := range t.Pools {
		n += t.Pools[i].Pods()
	}
	return n
}

// Pool is the pool of that name, or nil when there is none; data is not a
// seat pool.
func (t *Topology) Pool(name string) *PoolPlan {
	for i := range t.Pools {
		if t.Pools[i].Name == name {
			return &t.Pools[i]
		}
	}
	return nil
}

// Placement is the role.placement labels of an agent seat placed in pool:
// the pool's own label, or in a PerSeat pool the seat's. They are literal
// values: Crewlet expands no ${NAME} in a Tier B placement, so a pin can only
// name what the pods spell out.
func (t *Topology) Placement(pool, handle string) map[string]string {
	if p := t.Pool(pool); p != nil && p.Mode == v1alpha1.SeatPoolPerSeat {
		return map[string]string{SeatLabel: handle}
	}
	return map[string]string{PoolLabel: pool}
}

// SeatPool is the pool an agent Seat is placed in: its spec.placement.pool, or
// the Company's spec.topology.defaultSeatPool, or data. A Human seat is in
// no pool: the engine never runs it.
func SeatPool(c *v1alpha1.Company, s *v1alpha1.Seat) string {
	if s.Spec.Placement.Pool != "" {
		return s.Spec.Placement.Pool
	}
	return cmp.Or(c.Spec.Topology.DefaultSeatPool, v1alpha1.DataPool)
}

// PlanTopology places a company's agent seats in its pools.
//
// It fails with *RefError values, joined (see RefErrors), when a seat cannot
// run where it is placed: it names a pool the Company does not declare
// (ReasonPoolNotFound), a PerSeat pool its class does not allow
// (ReasonPerSeatNotAllowed), or a PerSeat pool whose pods would take the
// company past its class's seatPools.maxPods (ReasonPoolCapacityExceeded);
// and with refusals of the Company itself when it declares seat pools its
// class does not allow at all (ReasonSeatPoolsNotAllowed) or Shared pools
// whose pods alone pass maxPods (ReasonPoolCapacityExceeded). Company refuses
// the document for the same reasons, so a topology that fails here is never
// one the engine runs.
//
// The plan is returned with the error, never nil: every pool the Company
// declares, with the seats placed in it, less each Seat refused on its own
// (the refusals of the Company itself refuse no Seat). The runtime reconciler
// sizes the pools' pods from it while the document is blocked, since the
// revision the engine keeps running still needs them.
func PlanTopology(c *v1alpha1.Company, class *v1alpha1.EngineClass, seats []v1alpha1.Seat) (*Topology, error) {
	var errs refErrors
	t := planTopology(c, class, seats, &errs)
	return t, errs.err()
}

// planTopology is PlanTopology recording its refusals in errs, so that a
// render reports them together with every other refusal of the document. The
// plan it returns leaves every refused seat out of its pool.
func planTopology(c *v1alpha1.Company, class *v1alpha1.EngineClass, seats []v1alpha1.Seat, errs *refErrors) *Topology {
	settings := class.Spec.SeatPools
	t := &Topology{}
	for i := range c.Spec.Topology.SeatPools {
		t.Pools = append(t.Pools, poolPlan(&c.Spec.Topology.SeatPools[i], settings.Defaults))
	}

	allowed := settings.Policy == v1alpha1.SeatPoolsAllowed
	if len(t.Pools) > 0 && !allowed {
		errs.add(KindCompany, c.Name, "spec.topology.seatPools", v1alpha1.ReasonSeatPoolsNotAllowed, fmt.Sprintf(
			"declares %d seat pools, and EngineClass %s does not allow seat pools (spec.seatPools.policy is %s): "+
				"remove them, or run the company with a class whose policy is Allowed",
			len(t.Pools), class.Name, cmp.Or(settings.Policy, v1alpha1.SeatPoolsForbidden)))
	}
	perSeatAllowed := settings.AllowPerSeat != nil && *settings.AllowPerSeat

	// Placed in handle order, so the seats of a pool come out sorted and
	// the refusals in the same order on every render.
	agents := make([]*v1alpha1.Seat, 0, len(seats))
	for i := range seats {
		if seats[i].Spec.Kind != v1alpha1.SeatKindHuman {
			agents = append(agents, &seats[i])
		}
	}
	slices.SortFunc(agents, func(a, b *v1alpha1.Seat) int {
		return cmp.Or(strings.Compare(SeatHandle(a), SeatHandle(b)), strings.Compare(a.Name, b.Name))
	})
	perSeat := map[string][]*v1alpha1.Seat{}
	for _, s := range agents {
		name, handle := SeatPool(c, s), SeatHandle(s)
		if name == v1alpha1.DataPool {
			t.DataSeats = append(t.DataSeats, handle)
			continue
		}
		pool := t.Pool(name)
		switch {
		case pool == nil:
			errs.add(KindSeat, s.Name, "spec.placement.pool", v1alpha1.ReasonPoolNotFound, fmt.Sprintf(
				"names pool %s, which is neither data nor one of Company %s's spec.topology.seatPools", name, c.Name))
			continue
		case !allowed:
			// The Company is refused for declaring the pool at all; a
			// refusal of each of its seats would say nothing more.
		case pool.Mode == v1alpha1.SeatPoolPerSeat && !perSeatAllowed:
			errs.add(KindSeat, s.Name, "spec.placement.pool", v1alpha1.ReasonPerSeatNotAllowed, fmt.Sprintf(
				"places the seat in PerSeat pool %s, and EngineClass %s does not allow PerSeat pools "+
					"(spec.seatPools.allowPerSeat): place it in data or in a Shared pool", name, class.Name))
			continue
		case pool.Mode == v1alpha1.SeatPoolPerSeat:
			perSeat[name] = append(perSeat[name], s)
		}
		pool.Seats = append(pool.Seats, handle)
	}
	for i := range t.Pools {
		// Two Seats of one handle are refused as a Conflict elsewhere; a
		// handle names one seat, and one pod set, here.
		t.Pools[i].Seats = slices.Compact(t.Pools[i].Seats)
	}
	if allowed {
		checkCapacity(c, class, t, perSeat, errs)
	}
	return t
}

// checkCapacity holds the pools to the class's seatPools.maxPods. The Shared
// pools are the Company's own choice and run whatever seats they hold, so
// when they alone pass the cap the Company is refused; otherwise it is the
// PerSeat seats whose pods pass it, and each of them is refused, so that the
// message is on the objects whose removal brings the company back under it.
func checkCapacity(c *v1alpha1.Company, class *v1alpha1.EngineClass, t *Topology, perSeat map[string][]*v1alpha1.Seat, errs *refErrors) {
	limit := int(class.Spec.SeatPools.MaxPods)
	if limit == 0 {
		return
	}
	var shared int
	for i := range t.Pools {
		if t.Pools[i].Mode == v1alpha1.SeatPoolShared {
			shared += t.Pools[i].Pods()
		}
	}
	if shared > limit {
		errs.add(KindCompany, c.Name, "spec.topology.seatPools", v1alpha1.ReasonPoolCapacityExceeded, fmt.Sprintf(
			"its Shared pools run %d pods together, and EngineClass %s allows the company %d seat pods (spec.seatPools.maxPods)",
			shared, class.Name, limit))
		return
	}
	total := t.Pods()
	if total <= limit {
		return
	}
	for name, seats := range perSeat {
		pool := t.Pool(name)
		for _, s := range seats {
			errs.add(KindSeat, s.Name, "spec.placement.pool", v1alpha1.ReasonPoolCapacityExceeded, fmt.Sprintf(
				"places the seat in PerSeat pool %s, which runs %d pods for it; the company's seat pools would run %d pods "+
					"together, and EngineClass %s allows %d (spec.seatPools.maxPods)",
				name, pool.Replicas, total, class.Name, limit))
		}
		// Refused, so placed nowhere: the plan runs no pod for them.
		pool.Seats = nil
	}
}

// poolPlan resolves a pool's spec against the class's defaults.
func poolPlan(p *v1alpha1.SeatPool, defaults v1alpha1.SeatPoolDefaults) PoolPlan {
	plan := PoolPlan{
		Name: p.Name,
		Mode: cmp.Or(p.Mode, v1alpha1.SeatPoolShared),
		// The API defaults replicas to 1; a pool read before the API server
		// defaulted it takes the same.
		Replicas:                      1,
		MaxConcurrentTurns:            cmp.Or(p.MaxConcurrentTurns, defaults.MaxConcurrentTurns),
		Resources:                     cmp.Or(p.Resources, defaults.Resources),
		TerminationGracePeriodSeconds: cmp.Or(p.TerminationGracePeriodSeconds, defaults.TerminationGracePeriodSeconds, DefaultSeatPodTerminationGracePeriodSeconds),
		ScratchSizeLimit:              resource.MustParse(DefaultSeatPodScratchSizeLimit),
	}
	if p.Replicas != nil {
		plan.Replicas = *p.Replicas
	}
	if defaults.ScratchSizeLimit != nil {
		plan.ScratchSizeLimit = *defaults.ScratchSizeLimit
	}
	return plan
}
