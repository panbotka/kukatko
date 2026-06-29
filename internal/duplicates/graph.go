package duplicates

import (
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// graph accumulates the duplicate links among photos and resolves them into
// connected components. Nodes are dense integer ids assigned per uid; edges come
// from pHash banding (runPhash) and embedding pairs (addEmbedPairs).
type graph struct {
	nodes        []string       // node index -> uid
	index        map[string]int // uid -> node index
	entries      []phashEntry   // per-node perceptual hash, for banded scanning
	phashByNode  map[int]uint64 // node index -> perceptual hash
	uf           *unionFind
	phashMatched map[int]bool       // nodes that gained a pHash edge
	embedMatched map[int]bool       // nodes that gained an embedding edge
	embedDist    map[[2]int]float64 // node pair -> embedding cosine distance
}

// newGraph returns an empty graph ready to accept hashes and pairs.
func newGraph() *graph {
	return &graph{
		index:        make(map[string]int),
		phashByNode:  make(map[int]uint64),
		phashMatched: make(map[int]bool),
		embedMatched: make(map[int]bool),
		embedDist:    make(map[[2]int]float64),
	}
}

// nodeFor returns the node index for uid, allocating a new node the first time a
// uid is seen.
func (g *graph) nodeFor(uid string) int {
	if i, ok := g.index[uid]; ok {
		return i
	}
	i := len(g.nodes)
	g.nodes = append(g.nodes, uid)
	g.index[uid] = i
	return i
}

// addPhashes registers every hashed photo as a node and records its hash for the
// banded pHash scan. It must be called before runPhash and addEmbedPairs so that
// the node set is established (embedding pairs only link existing nodes).
func (g *graph) addPhashes(hashes []photos.Phash) {
	for _, h := range hashes {
		idx := g.nodeFor(h.PhotoUID)
		// The pHash is stored signed; reinterpret its bit pattern as uint64 for
		// Hamming comparison (no numeric meaning is attached to the value).
		ph := uint64(h.Phash) //nolint:gosec // intentional signed->unsigned bit reinterpretation
		g.phashByNode[idx] = ph
		g.entries = append(g.entries, phashEntry{idx: idx, phash: ph})
	}
}

// addEmbedPairs links each near-duplicate embedding pair whose both endpoints are
// known nodes (i.e. non-archived, hashed photos). It records the edge for the
// union, the per-pair distance (keeping the smallest seen), and that both
// endpoints were embedding-matched. Pairs touching an unknown uid are ignored.
func (g *graph) addEmbedPairs(pairs []vectors.DuplicatePair) {
	if g.uf == nil {
		g.uf = newUnionFind(len(g.nodes))
	}
	for _, p := range pairs {
		ia, okA := g.index[p.A]
		ib, okB := g.index[p.B]
		if !okA || !okB || ia == ib {
			continue
		}
		g.uf.union(ia, ib)
		g.embedMatched[ia] = true
		g.embedMatched[ib] = true
		key := orderedPair(ia, ib)
		if cur, ok := g.embedDist[key]; !ok || p.Distance < cur {
			g.embedDist[key] = p.Distance
		}
	}
}

// runPhash performs the banded pHash union over all registered entries, recording
// which nodes gained a pHash edge.
func (g *graph) runPhash(maxDiff int) {
	if g.uf == nil {
		g.uf = newUnionFind(len(g.nodes))
	}
	g.phashMatched = phashUnion(g.entries, maxDiff, g.uf)
}

// components groups node indices by their union-find root, returning only the
// components with at least two members (singletons are not duplicates). The
// returned slices are in ascending node order.
func (g *graph) components() map[int][]int {
	if g.uf == nil {
		return map[int][]int{}
	}
	byRoot := make(map[int][]int)
	for i := range g.nodes {
		root := g.uf.find(i)
		byRoot[root] = append(byRoot[root], i)
	}
	for root, members := range byRoot {
		if len(members) < 2 {
			delete(byRoot, root)
		}
	}
	return byRoot
}

// memberUIDs collects the uids of every node in every component, for a single
// batch metadata fetch.
func memberUIDs(nodes []string, comps map[int][]int) []string {
	var uids []string
	for _, members := range comps {
		for _, idx := range members {
			uids = append(uids, nodes[idx])
		}
	}
	return uids
}

// buildGroups turns each component into a Group enriched with photo metadata,
// dropping members whose photo metadata is missing and components that thereby
// fall below two members.
func (g *graph) buildGroups(comps map[int][]int, byUID map[string]photos.Photo) []Group {
	groups := make([]Group, 0, len(comps))
	for _, nodeIdxs := range comps {
		group, ok := g.buildGroup(nodeIdxs, byUID)
		if ok {
			groups = append(groups, group)
		}
	}
	return groups
}

// buildGroup assembles one group from its node indices, selecting the keeper,
// computing per-member distances to the keeper, and labelling the match reason.
// It returns ok=false when fewer than two members have available metadata.
func (g *graph) buildGroup(nodeIdxs []int, byUID map[string]photos.Photo) (Group, bool) {
	members, present := g.collectMembers(nodeIdxs, byUID)
	if len(members) < 2 {
		return Group{}, false
	}

	keeperPos := selectKeeperIndex(members)
	keeper := members[keeperPos]
	keeperNode := present[keeperPos]
	annotateMembers(members, keeper, keeperNode, present, g.embedDist)
	members[keeperPos].IsKeeper = true

	return Group{
		ID:             minUID(members),
		Reason:         g.reason(nodeIdxs),
		KeeperUID:      keeper.UID,
		Members:        members,
		keeperSortTime: keeper.sortTime,
	}, true
}

// collectMembers builds the Member list for the present node indices and returns
// it alongside the parallel slice of node indices it kept (skipping uids with no
// metadata), so callers can map a member position back to its node.
func (g *graph) collectMembers(nodeIdxs []int, byUID map[string]photos.Photo) ([]Member, []int) {
	members := make([]Member, 0, len(nodeIdxs))
	present := make([]int, 0, len(nodeIdxs))
	for _, idx := range nodeIdxs {
		photo, ok := byUID[g.nodes[idx]]
		if !ok {
			continue
		}
		members = append(members, toMember(photo, g.phashByNode[idx]))
		present = append(present, idx)
	}
	return members, present
}

// reason labels a component by the signals that linked it: both, embedding-only,
// or pHash-only (the default).
func (g *graph) reason(nodeIdxs []int) string {
	hasPhash, hasEmbed := false, false
	for _, idx := range nodeIdxs {
		if g.phashMatched[idx] {
			hasPhash = true
		}
		if g.embedMatched[idx] {
			hasEmbed = true
		}
	}
	switch {
	case hasPhash && hasEmbed:
		return ReasonBoth
	case hasEmbed:
		return ReasonEmbedding
	default:
		return ReasonPhash
	}
}

// toMember projects a photo plus its perceptual hash into a group Member, using
// the capture time when present (else the creation time) as the keeper sort key.
func toMember(p photos.Photo, phash uint64) Member {
	sortTime := p.CreatedAt
	if p.TakenAt != nil {
		sortTime = *p.TakenAt
	}
	return Member{
		UID:       p.UID,
		Title:     p.Title,
		FileName:  p.FileName,
		FileWidth: p.FileWidth, FileHeight: p.FileHeight,
		FileSize:  p.FileSize,
		MediaType: string(p.MediaType),
		TakenAt:   p.TakenAt,
		sortTime:  sortTime,
		phash:     phash,
	}
}

// annotateMembers fills each member's distance to the keeper: the pHash Hamming
// distance (always available) and, when the member shares an embedding edge with
// the keeper, that cosine distance. The keeper's own distances stay nil.
func annotateMembers(members []Member, keeper Member, keeperNode int, nodes []int, embedDist map[[2]int]float64) {
	for i := range members {
		if nodes[i] == keeperNode {
			continue
		}
		d := hamming(members[i].phash, keeper.phash)
		members[i].PhashDistance = &d
		if dist, ok := embedDist[orderedPair(nodes[i], keeperNode)]; ok {
			v := dist
			members[i].EmbeddingDistance = &v
		}
	}
}

// minUID returns the lexicographically smallest member uid, a stable group id.
func minUID(members []Member) string {
	smallest := members[0].UID
	for _, m := range members[1:] {
		if m.UID < smallest {
			smallest = m.UID
		}
	}
	return smallest
}
