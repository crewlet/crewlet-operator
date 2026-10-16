package render

import (
	"fmt"
	"slices"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// Placement is where a company document pins its agent seats: the values of
// the two placement labels its roles name (PoolLabel and SeatLabel). The
// runtime reconciler keeps a seat pool's pods running while the revision the
// engine runs pins a seat to them, whatever the Company now declares: a pod
// that stops while its seat is still pinned to it leaves the seat held by
// nobody until the next revision moves it.
type Placement struct {
	// Pools are the values of PoolLabel: data, and the Shared pools that
	// run seats. Sorted, without repetition.
	Pools []string
	// Seats are the values of SeatLabel: the handles of the seats that run
	// in a PerSeat pool. Sorted, without repetition.
	Seats []string
}

// placedDoc is the part of a company document PlacementOf, RolePlacements and
// HumanSeatsOf read: the roles, at the top level and in units nested to any
// depth.
type placedDoc struct {
	Roles []placedRole `json:"roles"`
	Units []placedUnit `json:"units"`
}

type placedUnit struct {
	Roles    []placedRole `json:"roles"`
	Children []placedUnit `json:"children"`
}

type placedRole struct {
	Handle    string `json:"handle"`
	Kind      string `json:"kind"`
	Placement *struct {
		Labels map[string]string `json:"labels"`
	} `json:"placement"`
}

// documentRoles is every role of a company document, at the top level and in
// units nested to any depth. what names the reading in its error.
func documentRoles(document []byte, what string) ([]placedRole, error) {
	var doc placedDoc
	if err := yaml.Unmarshal(document, &doc); err != nil {
		return nil, fmt.Errorf("reading the %s of a company document: %w", what, err)
	}
	roles := slices.Clone(doc.Roles)
	var walk func([]placedUnit)
	walk = func(units []placedUnit) {
		for _, u := range units {
			roles = append(roles, u.Roles...)
			walk(u.Children)
		}
	}
	walk(doc.Units)
	return roles, nil
}

// PlacementOf reads where a company document pins its agent seats. It reads
// any document the engine serves, not only one the operator rendered, since
// under the Observe and Seed policies the engine's own document is the one
// that runs; a role placed by other labels, or by a node, pins nothing the
// operator runs.
func PlacementOf(document []byte) (Placement, error) {
	roles, err := documentRoles(document, "placements")
	if err != nil {
		return Placement{}, err
	}
	var p Placement
	for _, r := range roles {
		if r.Placement == nil {
			continue
		}
		if v, ok := r.Placement.Labels[PoolLabel]; ok {
			p.Pools = append(p.Pools, v)
		}
		if v, ok := r.Placement.Labels[SeatLabel]; ok {
			p.Seats = append(p.Seats, v)
		}
	}
	slices.Sort(p.Pools)
	slices.Sort(p.Seats)
	p.Pools = slices.Compact(p.Pools)
	p.Seats = slices.Compact(p.Seats)
	return p, nil
}

// RolePlacements reads the role.placement labels of every role a company
// document pins by labels, by the role's handle, at the top level and in
// units nested to any depth.
func RolePlacements(document []byte) (map[string]map[string]string, error) {
	roles, err := documentRoles(document, "placements")
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]string{}
	for _, r := range roles {
		if r.Handle != "" && r.Placement != nil && len(r.Placement.Labels) > 0 {
			out[r.Handle] = r.Placement.Labels
		}
	}
	return out, nil
}

// HumanSeatsOf reads the handles of a company document's human seats, sorted:
// the seats a person can be invited or added onto. Like PlacementOf it reads
// any document the engine serves, since under the Observe and Seed policies
// the engine's own document is the one that runs; the engine stores every
// role with its handle.
func HumanSeatsOf(document []byte) ([]string, error) {
	roles, err := documentRoles(document, "human seats")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range roles {
		if r.Kind == seatKind(v1alpha1.SeatKindHuman) && r.Handle != "" {
			out = append(out, r.Handle)
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}
