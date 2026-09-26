package family

import (
	"cmp"
	"context"
	"fmt"
	"slices"
)

// networkFamiliesSQL reads every family one of the given people belongs to —
// as a partner or as a child — with all of its children. It is one step of the
// network walk: the walk hands it a breadth-first frontier and gets back the
// family boxes that frontier opens.
//
// Unlike treeFamiliesSQL it does not filter the children to a walked set: the
// network walk takes a family into the drawing only together with everybody in
// it, so every child it lists is a member by construction. That missing filter
// is the whole reason aunts and cousins appear here and in neither directional
// walk.
const networkFamiliesSQL = `
SELECT f.uid, f.partner_a_uid, f.partner_b_uid, f.kind, f.from_year, f.to_year,
       f.note, f.created_at, f.updated_at,
       ARRAY(SELECT c.child_uid FROM subject_family_children c
              WHERE c.family_uid = f.uid) AS child_uids
FROM subject_families f
WHERE f.partner_a_uid = ANY($1) OR f.partner_b_uid = ANY($1)
   OR EXISTS (SELECT 1 FROM subject_family_children c
               WHERE c.family_uid = f.uid AND c.child_uid = ANY($1))`

// networkMembersSQL hydrates the walked set: the relative columns of every
// member plus the signed generation the walk gave them, read side by side from
// two parallel arrays. The depth column is the distance in generations, so the
// row scans exactly like a directional walk's.
const networkMembersSQL = `
SELECT ` + relativeColumns + `, ABS(w.generation), FALSE
FROM unnest($1::varchar[], $2::int[]) AS w(uid, generation)
JOIN subjects s ON s.uid = w.uid
ORDER BY w.generation, s.birth_year NULLS LAST, s.name, s.uid`

// familyFetcher reads the families any of uids belongs to, as a partner or as a
// child, each with all of its children. The network walk is written against it
// rather than against the pool, so the traversal is a pure function a unit test
// can drive with an in-memory graph.
type familyFetcher func(ctx context.Context, uids []string) ([]TreeFamily, error)

// network is what the walk found: the members with their signed generations in
// the order they were reached, the family boxes tying them together, whether
// the cap cut the component short, and how many people the whole component
// holds — which is the "of M" a page needs to say how much it left out.
type network struct {
	order      []string
	generation map[string]int
	families   []TreeFamily
	truncated  bool
	total      int
	// keptPeople and keptFamilies mark where the cap cut the walk: the lengths
	// of order and families at the first family that would not fit. Meaningful
	// only once truncated is set.
	keptPeople   int
	keptFamilies int
}

// walkNetwork walks the whole connected component around rootUID over the
// bipartite graph of people and families: a person is joined to a family by
// being one of its partners or one of its children, and both kinds of edge are
// followed in both directions, breadth-first, until nothing new is reached.
//
// Generations are signed and propagate along those edges — a family takes the
// generation of its partners, its children one more — and the first write wins.
// Because the walk is breadth-first, the first write is the nearest
// relationship: a woman who married her mother's cousin is drawn as a wife, one
// step away, rather than as a cousin three steps away, and she is drawn once.
// The order is deterministic — the frontier in the order it was reached, a
// person's families by uid, a family's partners before its children — so two
// equally near readings always resolve the same way.
//
// A family is taken in whole or not at all, so every uid a returned family
// names is a member and a renderer is never handed an edge to a person it has
// no node for. When taking the next family would push the walk past limit
// people, the drawing is cut there and the walk reports truncated; everything
// kept is nearer than anything left out. The walk itself goes on to the end of
// the component, only counting, so total says how many people it holds: the
// order is deterministic, so what a capped walk keeps is exactly the prefix an
// uncapped one had reached at that moment. The walk visits each person and each
// family once, so it terminates on any graph, cycles included, without a depth
// guard.
func walkNetwork(ctx context.Context, rootUID string, limit int, fetch familyFetcher) (network, error) {
	walk := network{order: []string{rootUID}, generation: map[string]int{rootUID: 0}}
	taken := map[string]bool{}
	for frontier := []string{rootUID}; len(frontier) > 0; {
		families, err := fetch(ctx, frontier)
		if err != nil {
			return network{}, err
		}
		frontier = walk.expand(frontier, families, taken, limit)
	}
	walk.total = len(walk.order)
	if walk.truncated {
		walk.order = walk.order[:walk.keptPeople]
		walk.families = walk.families[:walk.keptFamilies]
	}
	return walk, nil
}

// expand takes, in frontier order, every family of a frontier person the walk
// has not taken yet, and returns the people reached for the first time — the
// next frontier. At the first family that would not fit under limit it marks
// the walk truncated and records where the kept drawing ends.
func (w *network) expand(frontier []string, families []TreeFamily, taken map[string]bool, limit int) []string {
	slices.SortFunc(families, func(a, b TreeFamily) int { return cmp.Compare(a.UID, b.UID) })
	var next []string
	for _, person := range frontier {
		for _, fam := range families {
			generation, ok := w.familyGeneration(fam, person)
			if taken[fam.UID] || !ok {
				continue
			}
			if !w.truncated && len(w.order)+len(w.newcomers(fam)) > limit {
				w.truncated = true
				w.keptPeople, w.keptFamilies = len(w.order), len(w.families)
			}
			taken[fam.UID] = true
			next = w.take(next, fam, generation)
		}
	}
	return next
}

// take adds fam to the drawing at the given generation: its partners share it,
// its children are one below. Everybody reached for the first time is appended
// to next.
func (w *network) take(next []string, fam TreeFamily, generation int) []string {
	w.families = append(w.families, fam)
	for _, uid := range fam.partnerUIDs() {
		next = w.assign(next, uid, generation)
	}
	for _, uid := range fam.ChildUIDs {
		next = w.assign(next, uid, generation+1)
	}
	return next
}

// familyGeneration reports the generation fam takes when it is reached from
// person: theirs when they are one of its partners, one less when they are one
// of its children. ok is false when person is not in fam at all.
func (w *network) familyGeneration(fam TreeFamily, person string) (int, bool) {
	if slices.Contains(fam.partnerUIDs(), person) {
		return w.generation[person], true
	}
	if slices.Contains(fam.ChildUIDs, person) {
		return w.generation[person] - 1, true
	}
	return 0, false
}

// newcomers returns the people of fam the walk has not reached yet, each once.
func (w *network) newcomers(fam TreeFamily) []string {
	var out []string
	for _, uid := range append(fam.partnerUIDs(), fam.ChildUIDs...) {
		if _, seen := w.generation[uid]; !seen && !slices.Contains(out, uid) {
			out = append(out, uid)
		}
	}
	return out
}

// assign gives uid its generation unless an earlier — and therefore nearer —
// reading already did, and appends a person reached for the first time to next.
func (w *network) assign(next []string, uid string, generation int) []string {
	if _, seen := w.generation[uid]; seen {
		return next
	}
	w.generation[uid] = generation
	w.order = append(w.order, uid)
	return append(next, uid)
}

// network walks the component around rootUID (see walkNetwork), keeping at most
// limit people, and hydrates it into the tree payload. The caller has already
// checked that the root exists.
func (s *Store) network(ctx context.Context, root Relative, limit int) (Tree, error) {
	walk, err := walkNetwork(ctx, root.UID, limit, s.networkFamilies)
	if err != nil {
		return Tree{}, err
	}
	uids := make([]string, 0, len(walk.order))
	generations := make([]int, 0, len(walk.order))
	for _, uid := range walk.order {
		uids = append(uids, uid)
		generations = append(generations, walk.generation[uid])
	}
	members, err := queryMembers(ctx, s.pool, "the network", networkMembersSQL, uids, generations)
	if err != nil {
		return Tree{}, err
	}
	for i := range members {
		members[i].Generation = walk.generation[members[i].UID]
	}
	slices.SortStableFunc(walk.families, compareTreeFamilies)
	families := walk.families
	if families == nil {
		families = []TreeFamily{}
	}
	return Tree{
		Root: root, Direction: DirectionNetwork, Members: members, Families: families,
		Truncated: walk.truncated, Total: walk.total,
	}, nil
}

// compareTreeFamilies orders family boxes the way treeFamiliesSQL does: by the
// year the union began, unknown last, then by creation and uid, so the payload
// reads the same whichever walk produced it.
func compareTreeFamilies(a, b TreeFamily) int {
	switch {
	case a.FromYear == nil && b.FromYear != nil:
		return 1
	case a.FromYear != nil && b.FromYear == nil:
		return -1
	case a.FromYear != nil && *a.FromYear != *b.FromYear:
		return cmp.Compare(*a.FromYear, *b.FromYear)
	}
	if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
		return c
	}
	return cmp.Compare(a.UID, b.UID)
}

// networkFamilies is the walk's familyFetcher over the database: the families
// uids belong to, each with all of its children in byte order.
func (s *Store) networkFamilies(ctx context.Context, uids []string) ([]TreeFamily, error) {
	rows, err := s.pool.Query(ctx, networkFamiliesSQL, uids)
	if err != nil {
		return nil, fmt.Errorf("family: reading network families: %w", err)
	}
	defer rows.Close()

	var out []TreeFamily
	for rows.Next() {
		var fam TreeFamily
		if err := rows.Scan(
			&fam.UID, &fam.PartnerA, &fam.PartnerB, &fam.Kind, &fam.FromYear, &fam.ToYear,
			&fam.Note, &fam.CreatedAt, &fam.UpdatedAt, &fam.ChildUIDs,
		); err != nil {
			return nil, fmt.Errorf("family: scanning network family: %w", err)
		}
		slices.Sort(fam.ChildUIDs)
		out = append(out, fam)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("family: reading network families: %w", err)
	}
	return out, nil
}
