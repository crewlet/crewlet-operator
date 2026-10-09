package render

import (
	"fmt"
	"slices"

	"sigs.k8s.io/yaml"
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

// placedDoc is the part of a company document PlacementOf reads: the roles,
// at the top level and in units nested to any depth.
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
	Placement *struct {
		Labels map[string]string `json:"labels"`
	} `json:"placement"`
}

// PlacementOf reads where a company document pins its agent seats. It reads
// any document the engine serves, not only one the operator rendered, since
// under the Observe and Seed policies the engine's own document is the one
// that runs; a role placed by other labels, or by a node, pins nothing the
// operator runs.
func PlacementOf(document []byte) (Placement, error) {
	var doc placedDoc
	if err := yaml.Unmarshal(document, &doc); err != nil {
		return Placement{}, fmt.Errorf("reading the placements of a company document: %w", err)
	}
	var p Placement
	add := func(roles []placedRole) {
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
	}
	var walk func([]placedUnit)
	walk = func(units []placedUnit) {
		for _, u := range units {
			add(u.Roles)
			walk(u.Children)
		}
	}
	add(doc.Roles)
	walk(doc.Units)
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
	var doc placedDoc
	if err := yaml.Unmarshal(document, &doc); err != nil {
		return nil, fmt.Errorf("reading the placements of a company document: %w", err)
	}
	out := map[string]map[string]string{}
	add := func(roles []placedRole) {
		for _, r := range roles {
			if r.Handle != "" && r.Placement != nil && len(r.Placement.Labels) > 0 {
				out[r.Handle] = r.Placement.Labels
			}
		}
	}
	var walk func([]placedUnit)
	walk = func(units []placedUnit) {
		for _, u := range units {
			add(u.Roles)
			walk(u.Children)
		}
	}
	add(doc.Roles)
	walk(doc.Units)
	return out, nil
}
