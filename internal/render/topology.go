package render

import (
	"cmp"
	"fmt"
	"maps"
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
// Every agent seat is pinned, the data pods' too, because an unpinned seat
// may run on any node, a seat pod among them, and a seat pod cannot serve
// every seat (a bundled login: seatPodProviders; a Direct executor while the
// Company has no public address: ExecutorOf). The
// pins are literals, since Crewlet expands no ${NAME} in a Tier B placement,
// and one label per pod lets placement status and settling map each pod to
// exactly one workload. Capacity is no reason for it: Crewlet bounds a node
// by its share of each group separately, never by the shares pooled
// (internal/seat/placement, "A share bounds its own group and nothing else").
const (
	// PoolLabel places the seats of a pool whose pods share them: the data
	// pods' (data) and each Shared pool's, with the pool's name as its value.
	PoolLabel = "crewlet.ai/pool"
	// SeatLabel places the seat of a PerSeat pool, with the seat's handle as
	// its value: only that seat's own pods carry it.
	SeatLabel = "crewlet.ai/seat"
	// UndeclaredPoolLabel pins a seat placed in a pool the Company does not
	// declare, with the pool's name as its value. No pod carries it, the
	// operator's least of all: PoolLabel would not do, because the pods of
	// a pool declared later carry PoolLabel with that name, and the engine
	// would place the seat on them under the revision it already runs,
	// before a revision placing it there had been through the render's
	// checks of what those pods can run (seatPodProviders). And
	// PlacementOf, which reads PoolLabel and SeatLabel alone, records no
	// pool for it, so it neither keeps a StatefulSet nor opens the data
	// pods' leaf listener (PinsSeatPods).
	UndeclaredPoolLabel = "crewlet.ai/undeclared-pool"
)

// What a seat pool runs with when neither it nor its class says.
const (
	// DefaultSeatPodTerminationGracePeriodSeconds is a seat pod's grace
	// period: five minutes, as for a data pod. A seat pod finishes the turn
	// segments it runs in its own process before it exits (a seat's executor
	// run is not one, and is not waited for) and puts no bound on that
	// itself, and Crewlet suggests two to five minutes; the Kubernetes
	// default of thirty seconds would kill a segment in the middle.
	DefaultSeatPodTerminationGracePeriodSeconds = 300
)

// DefaultScratchSizeLimit caps an engine pod's scratch emptyDir when its class
// does not: data.scratchSizeLimit for a data pod, which keeps its /tmp and
// HOME there, and seatPools.defaults.scratchSizeLimit for a seat pod, which
// keeps its scratch store and the store's native library there besides.
//
// What fills it is what the processes the engine runs on the pod write off
// the data volume and outside a box: the stdio tool servers' packages, which
// a launcher such as npx or uvx installs under HOME, a few hundred MiB each,
// so a dozen of them come to a few GiB; their and git's scratch files under
// /tmp, and the sockets of the tool sessions; a seat pod's scratch store, a
// copy of its seats' memory and working rows, and the library of about 20 MB.
// And what a seat's Direct executor run writes outside its box: Crewlet
// points a box's temporary directories into it (TMPDIR, TMP, TEMP,
// CLAUDE_CODE_TMPDIR) and confines the run to nothing else, so a command
// that names /tmp outright, or the engine's home, writes here, on the pod's
// scratch rather than its sandboxes volume (SandboxDir). 10Gi leaves several
// GiB above the tool servers to that, half of what DefaultSandboxSizeLimit
// leaves several runs to write inside their boxes.
//
// It is a ceiling, not a reservation, as DefaultSandboxSizeLimit is, and errs
// high for the same reason: it takes nothing from the node until it is
// written, while a pod that reaches it is evicted with every run and turn on
// it (on a data pod, the company's API too). What it guards against is a
// process that writes without end, which it stops at its own pod rather than
// letting it fill the node's disk under the pods beside it. Nothing measured
// sets the figure: the right one for a tier is one measured on its own
// workloads.
const DefaultScratchSizeLimit = "10Gi"

// ScratchSizeLimitOf is the limit of an engine pod's scratch volume: the
// class's, or DefaultScratchSizeLimit when it sets none.
func ScratchSizeLimitOf(class *resource.Quantity) resource.Quantity {
	if class != nil {
		return *class
	}
	return resource.MustParse(DefaultScratchSizeLimit)
}

// DefaultSandboxSizeLimit caps an engine pod's sandboxes emptyDir
// (SandboxDir) when its class does not: data.sandboxSizeLimit for a data
// pod, seatPools.defaults.sandboxSizeLimit for a seat pod.
//
// The volume holds at most one box for each agent seat of the pod, and a box
// holds the run's checkout, its coding CLI's home and temporary files, and
// whatever the run installs and builds there: a language's module or
// package cache and a build tree come to a few GiB for an ordinary project,
// and more for a large one. 20Gi lets several such runs write at once on
// one pod.
//
// It is a ceiling, not a reservation, which is why it errs high: an
// emptyDir's limit takes nothing from the node until the boxes write it, so
// a generous one costs nothing while they stay small, while one a run
// reaches evicts the pod, with every run and turn on it (on a data pod, the
// company's API too). What it guards against is a run that writes without
// end inside its box, which it stops at its own pod rather than letting it
// fill the node's disk under the pods beside it. It bounds the box alone: a
// Direct run is not confined to it, and what the run writes elsewhere counts
// against the volume it lands on, a path under /tmp named outright or the
// engine's home against the pod's scratch volume (DefaultScratchSizeLimit),
// and on a data pod anything under DataDir against the company's data
// volume, which no emptyDir limit bounds. A class whose seats build large trees
// raises it, and one that schedules by an ephemeral-storage request sizes
// the request for it; the right figure for a tier is one measured on its
// own workloads.
const DefaultSandboxSizeLimit = "20Gi"

// SandboxSizeLimitOf is the limit of an engine pod's sandboxes volume: the
// class's, or DefaultSandboxSizeLimit when it sets none.
func SandboxSizeLimitOf(class *resource.Quantity) resource.Quantity {
	if class != nil {
		return *class
	}
	return resource.MustParse(DefaultSandboxSizeLimit)
}

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
	// Resources are the engine container's, or nil for none: the class's,
	// which alone sizes a seat pod. It is the object the class holds; treat
	// it as read-only.
	Resources                     *corev1.ResourceRequirements
	TerminationGracePeriodSeconds int64
	ScratchSizeLimit              resource.Quantity
	// SandboxSizeLimit caps each of the pool's pods' sandboxes volume
	// (SandboxSizeLimitOf), which only the class sets.
	SandboxSizeLimit resource.Quantity
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

// Places reports whether the plan runs the seat handle on the pods of seat
// pool pool: the pool is one of the Company's, and the seat is not refused
// it. The data pods are no seat pool.
func (t *Topology) Places(pool, handle string) bool {
	p := t.Pool(pool)
	return p != nil && slices.Contains(p.Seats, handle)
}

// Placement is the role.placement labels of an agent seat placed in pool:
// the pool's own label, or in a PerSeat pool the seat's, or for a pool the
// Company does not declare UndeclaredPoolLabel, which no pod carries. They
// are literal values: Crewlet expands no ${NAME} in a Tier B placement, so a
// pin can only name what the pods spell out.
func (t *Topology) Placement(pool, handle string) map[string]string {
	p := t.Pool(pool)
	switch {
	case pool == v1alpha1.DataPool:
	case p == nil:
		return map[string]string{UndeclaredPoolLabel: pool}
	case p.Mode == v1alpha1.SeatPoolPerSeat:
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
// It fails with *RefError values, joined (see RefErrors), when the class does
// not let a seat run where it is placed: a PerSeat pool its class does not
// allow (ReasonPerSeatNotAllowed), or a PerSeat pool whose pods would take the
// company past its class's seatPools.maxPods (ReasonPoolCapacityExceeded);
// and with refusals of the Company itself when it declares seat pools its
// class does not allow at all (ReasonSeatPoolsNotAllowed) or Shared pools
// whose pods alone pass maxPods (ReasonPoolCapacityExceeded). Company refuses
// the document for the same reasons, so a topology that fails here is never
// one the engine runs.
//
// A seat placed in a pool the Company does not declare is in no pool of the
// plan and fails nothing: it is pinned to UndeclaredPoolLabel, which no pod
// carries, and the render reports it (ReasonPoolNotFound, Basis Reported),
// since only that seat does not run.
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
		t.Pools = append(t.Pools, PoolPlanOf(&c.Spec.Topology.SeatPools[i], settings.Defaults))
	}

	allowed := settings.Policy == v1alpha1.SeatPoolsAllowed
	if len(t.Pools) > 0 && !allowed {
		errs.add(ClassPolicy, KindCompany, c.Name, "spec.topology.seatPools", v1alpha1.ReasonSeatPoolsNotAllowed, fmt.Sprintf(
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
			errs.add(Reported, KindSeat, s.Name, "spec.placement.pool", v1alpha1.ReasonPoolNotFound, fmt.Sprintf(
				"names pool %s, which is neither data nor one of Company %s's spec.topology.seatPools, so no pod runs "+
					"the seat until the Company declares it", name, c.Name))
			continue
		case !allowed:
			// The Company is refused for declaring the pool at all; a
			// refusal of each of its seats would say nothing more.
		case pool.Mode == v1alpha1.SeatPoolPerSeat && !perSeatAllowed:
			errs.add(ClassPolicy, KindSeat, s.Name, "spec.placement.pool", v1alpha1.ReasonPerSeatNotAllowed, fmt.Sprintf(
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
// when they alone pass the cap the Company is refused. Otherwise the PerSeat
// seats are admitted in one order, each with its pool's pods, until the first
// that does not fit; that seat and every seat after it are refused, so the
// refused seats are always a tail of the order and never a younger seat
// slipped in behind an older one in a smaller pool.
//
// The order puts first the seats the running revision already runs in pods of
// their own (status.config.placement.seats), then the rest, each group oldest
// first (by creation, then by handle). Creation alone is not the seat that
// pushed the company over the cap: an old Seat moved into a full PerSeat pool
// is older than the seat running there, and admitting it first would refuse
// the running seat, which changed nothing, and plan pods for the one that
// cannot run, while the running seat's pods are kept for the revision that
// still pins it. Refusing the newcomer blames the change.
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
		errs.add(ClassPolicy, KindCompany, c.Name, "spec.topology.seatPools", v1alpha1.ReasonPoolCapacityExceeded, fmt.Sprintf(
			"its Shared pools run %d pods together, and EngineClass %s allows the company %d seat pods (spec.seatPools.maxPods)",
			shared, class.Name, limit))
		return
	}
	type placed struct {
		seat *v1alpha1.Seat
		pool string
	}
	var queue []placed
	for _, name := range slices.Sorted(maps.Keys(perSeat)) {
		for _, s := range perSeat[name] {
			queue = append(queue, placed{seat: s, pool: name})
		}
	}
	running := c.Status.Config.Placement.Seats
	// newcomer is 0 for a seat the running revision runs in its own pods and
	// 1 for any other, so the running ones sort first.
	newcomer := func(s *v1alpha1.Seat) int {
		if slices.Contains(running, SeatHandle(s)) {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(queue, func(a, b placed) int {
		return cmp.Or(cmp.Compare(newcomer(a.seat), newcomer(b.seat)),
			a.seat.CreationTimestamp.Compare(b.seat.CreationTimestamp.Time),
			strings.Compare(SeatHandle(a.seat), SeatHandle(b.seat)))
	})
	pods := shared
	var first *v1alpha1.Seat
	for _, q := range queue {
		pool := t.Pool(q.pool)
		switch {
		case first == nil && pods+int(pool.Replicas) <= limit:
			pods += int(pool.Replicas)
			continue
		case first == nil:
			first = q.seat
			errs.add(ClassPolicy, KindSeat, q.seat.Name, "spec.placement.pool", v1alpha1.ReasonPoolCapacityExceeded, fmt.Sprintf(
				"places the seat in PerSeat pool %s, which runs %d pods for it; the company's seat pods would number %d with "+
					"them, and EngineClass %s allows %d (spec.seatPools.maxPods): the seats already running in pods of their "+
					"own, then the older seats, take the rest",
				q.pool, pool.Replicas, pods+int(pool.Replicas), class.Name, limit))
		default:
			errs.add(ClassPolicy, KindSeat, q.seat.Name, "spec.placement.pool", v1alpha1.ReasonPoolCapacityExceeded, fmt.Sprintf(
				"places the seat in PerSeat pool %s, and comes after Seat %s, which already takes the company past "+
					"EngineClass %s's %d seat pods (spec.seatPools.maxPods): the seats already running in pods of their "+
					"own, then the older seats, are admitted first",
				q.pool, first.Name, class.Name, limit))
		}
		// Refused, so placed nowhere: the plan runs no pod for it.
		pool.Seats = slices.DeleteFunc(pool.Seats, func(h string) bool { return h == SeatHandle(q.seat) })
	}
}

// PoolPlanOf resolves a pool's spec against the class's defaults, with no
// seats placed in it.
func PoolPlanOf(p *v1alpha1.SeatPool, defaults v1alpha1.SeatPoolDefaults) PoolPlan {
	plan := PoolPlan{
		Name: p.Name,
		Mode: cmp.Or(p.Mode, v1alpha1.SeatPoolShared),
		// The API defaults replicas to 1; a pool read before the API server
		// defaulted it takes the same.
		Replicas:                      1,
		MaxConcurrentTurns:            cmp.Or(p.MaxConcurrentTurns, defaults.MaxConcurrentTurns),
		Resources:                     defaults.Resources,
		TerminationGracePeriodSeconds: cmp.Or(p.TerminationGracePeriodSeconds, defaults.TerminationGracePeriodSeconds, DefaultSeatPodTerminationGracePeriodSeconds),
		ScratchSizeLimit:              ScratchSizeLimitOf(defaults.ScratchSizeLimit),
		SandboxSizeLimit:              SandboxSizeLimitOf(defaults.SandboxSizeLimit),
	}
	if p.Replicas != nil {
		plan.Replicas = *p.Replicas
	}
	return plan
}
