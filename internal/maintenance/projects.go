package maintenance

import (
	"context"
	"fmt"
	"sort"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/storekeys"
)

// ProjectLister enumerates the instance's projects, the deactivated ones
// included: a project nobody can enter still owns its catalogue and its objects,
// and they are neither orphans nor someone else's. It is satisfied by
// *project.Store.
type ProjectLister interface {
	// List returns every project of the instance.
	List(ctx context.Context) ([]project.Project, error)
}

// soleBootstrap is the project list a Service assumes when it is wired without a
// ProjectLister: one library, the bootstrap project, in the root layout — the
// instance as it was before projects existed.
func soleBootstrap() []project.Project {
	return []project.Project{{UID: project.BootstrapUID, Slug: project.BootstrapSlug}}
}

// listProjects returns the projects the scan reconciles against: the lister's
// answer, or the bootstrap project alone when no lister is wired.
func (s *Service) listProjects(ctx context.Context) ([]project.Project, error) {
	if s.projects == nil {
		return soleBootstrap(), nil
	}
	list, err := s.projects.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("maintenance: listing projects: %w", err)
	}
	return list, nil
}

// projectTally accumulates one project's half of the report while the scan
// walks the catalogue and the store.
type projectTally struct {
	report           ProjectReport
	missingOriginals *findingCollector
	missingThumbs    *findingCollector
	orphans          *findingCollector
}

// projectLedger is the per-project breakdown under construction: one tally per
// project, in the order the projects were listed, plus what the store
// attribution needs to know about them.
type projectLedger struct {
	limit int
	order []string
	byUID map[string]*projectTally
	// rootOwner is the uid of the project still using the root layout (its
	// storage prefix is empty), or "" once no project does — after the bootstrap
	// project's storage move.
	rootOwner string
	// rootDerived are the root keys of derived objects whose photo has moved
	// under its prefix: reported as RootDerived, so the reconciliation passes
	// over them rather than counting them as legacy too.
	rootDerived map[string]struct{}
}

// newProjectLedger returns a ledger with one empty tally per project, keeping
// at most limit samples per finding.
func newProjectLedger(projects []project.Project, limit int) *projectLedger {
	ledger := &projectLedger{limit: limit, byUID: make(map[string]*projectTally, len(projects))}
	for _, p := range projects {
		if _, dup := ledger.byUID[p.UID]; dup {
			continue
		}
		tally := ledger.add(p.UID)
		tally.report.Slug = p.Slug
		tally.report.Name = p.Name
		tally.report.StoragePrefix = p.StoragePrefix
		if p.StoragePrefix == "" && ledger.rootOwner == "" {
			ledger.rootOwner = p.UID
		}
	}
	return ledger
}

// add appends an empty tally for uid.
func (l *projectLedger) add(uid string) *projectTally {
	tally := &projectTally{
		report:           ProjectReport{UID: uid},
		missingOriginals: newFindingCollector(l.limit),
		missingThumbs:    newFindingCollector(l.limit),
		orphans:          newFindingCollector(l.limit),
	}
	l.order = append(l.order, uid)
	l.byUID[uid] = tally
	return tally
}

// known reports whether uid names a listed project.
func (l *projectLedger) known(uid string) bool {
	_, ok := l.byUID[uid]
	return ok
}

// tally returns uid's tally, adding one for a project the listing did not name —
// which the foreign keys make impossible for a catalogue row, but which must not
// make a row vanish from the report if it ever happens.
func (l *projectLedger) tally(uid string) *projectTally {
	if tally, ok := l.byUID[uid]; ok {
		return tally
	}
	return l.add(uid)
}

// absorb distributes a finished store reconciliation over the projects: each
// one's originals in the store and its orphans, in key order.
func (l *projectLedger) absorb(rec storeReconciliation) {
	for uid, n := range rec.perProject {
		l.tally(uid).report.Originals = n
	}
	for _, orphan := range rec.orphans {
		l.tally(orphan.projectUID).orphans.add(orphan.key)
	}
}

// reports returns the finished per-project breakdown, in listing order.
func (l *projectLedger) reports() []ProjectReport {
	out := make([]ProjectReport, 0, len(l.order))
	for _, uid := range l.order {
		tally := l.byUID[uid]
		report := tally.report
		report.MissingOriginals = tally.missingOriginals.finding()
		report.MissingThumbnails = tally.missingThumbs.finding()
		report.OrphanFiles = tally.orphans.finding()
		out = append(out, report)
	}
	return out
}

// attribution is what the scan concludes about one object of the store.
type attribution int

const (
	// attrOwned means the object belongs to a listed project.
	attrOwned attribution = iota
	// attrOrphanProject means the object sits under projects/<uid>/ for a uid no
	// project has.
	attrOrphanProject
	// attrLegacy means the object is in the root layout although no project uses
	// that layout any more, and nothing in the catalogue still points there.
	attrLegacy
	// attrInUse means a root-layout derived object while some catalogued file
	// still lives in the root layout — the state half-way through a storage move,
	// in which the root's thumbnails and segments are still being served.
	attrInUse
)

// ownedKey is an orphan original together with the project it belongs to.
type ownedKey struct {
	key        string
	projectUID string
}

// storeReconciliation is the outcome of reconciling one listing of the store
// against the catalogue, per project.
type storeReconciliation struct {
	// originals counts every original object the store holds, in any layout.
	originals int
	// perProject counts the originals attributed to each project.
	perProject map[string]int
	// orphans are the attributed originals no catalogue row claims, sorted by
	// key once the listing is done.
	orphans []ownedKey
	// orphanProject are the keys under the tree of a project that does not
	// exist, sorted.
	orphanProject []string
	// legacy are the root-layout keys left behind once no project uses the root
	// layout, sorted.
	legacy []string
}

// orphanKeys returns the orphan originals' keys, in key order.
func (r storeReconciliation) orphanKeys() []string {
	keys := make([]string, len(r.orphans))
	for i, orphan := range r.orphans {
		keys[i] = orphan.key
	}
	return keys
}

// reconciler attributes the objects of one store listing to their projects.
type reconciler struct {
	ledger *projectLedger
	// catalogued maps every catalogued file key to its photo's project.
	catalogued map[string]string
	// rootInUse reports whether any catalogued file still lives in the root
	// layout.
	rootInUse bool
	result    storeReconciliation
}

// newReconciler returns a reconciler of the store against files, the whole
// catalogue's file rows.
func newReconciler(ledger *projectLedger, files []photos.CataloguedFile) *reconciler {
	rec := &reconciler{
		ledger:     ledger,
		catalogued: make(map[string]string, len(files)),
		result:     storeReconciliation{perProject: map[string]int{}, orphans: []ownedKey{}},
	}
	for _, f := range files {
		rec.catalogued[f.FilePath] = f.ProjectUID
		if storekeys.PrefixOf(f.FilePath) == "" {
			rec.rootInUse = true
		}
	}
	return rec
}

// attribute decides whose obj is.
//
// A key under projects/<uid>/ belongs to that project, or is an orphan project's
// when no such project exists. A root-layout key belongs to the project still
// using the root layout. Once none does — the bootstrap project's storage has
// been moved — a root original still belongs to the project whose catalogue
// points at it (the move is per photo, so half-way through it many do), and a
// root-layout derived object is still in use for as long as any catalogued file
// lives at the root. Everything else at the root is a leftover of the old
// layout: legacy, for a maintainer to decide about.
func (r *reconciler) attribute(obj StoreEntry) (string, attribution) {
	if obj.ProjectUID != "" {
		if !r.ledger.known(obj.ProjectUID) {
			return "", attrOrphanProject
		}
		return obj.ProjectUID, attrOwned
	}
	if r.ledger.rootOwner != "" {
		return r.ledger.rootOwner, attrOwned
	}
	if obj.Kind == storekeys.KindOriginal {
		if owner, ok := r.catalogued[obj.Key]; ok {
			return owner, attrOwned
		}
		return "", attrLegacy
	}
	if r.rootInUse {
		return "", attrInUse
	}
	return "", attrLegacy
}

// observe records one object of the listing.
func (r *reconciler) observe(obj StoreEntry) {
	if obj.Kind == storekeys.KindOriginal {
		r.result.originals++
	}
	if _, moved := r.ledger.rootDerived[obj.Key]; moved && obj.ProjectUID == "" {
		return
	}
	owner, verdict := r.attribute(obj)
	switch verdict {
	case attrOrphanProject:
		r.result.orphanProject = append(r.result.orphanProject, obj.Key)
	case attrLegacy:
		r.result.legacy = append(r.result.legacy, obj.Key)
	case attrInUse:
	case attrOwned:
		if obj.Kind != storekeys.KindOriginal {
			return
		}
		r.result.perProject[owner]++
		if _, ok := r.catalogued[obj.Key]; !ok {
			r.result.orphans = append(r.result.orphans, ownedKey{key: obj.Key, projectUID: owner})
		}
	}
}

// finish returns the reconciliation with every list sorted by key, for a
// deterministic report.
func (r *reconciler) finish() storeReconciliation {
	out := r.result
	sort.Slice(out.orphans, func(i, j int) bool { return out.orphans[i].key < out.orphans[j].key })
	sort.Strings(out.orphanProject)
	sort.Strings(out.legacy)
	return out
}

// reconcileStore lists the store once and reconciles it against files, the
// whole catalogue's file rows, per the projects in ledger.
func (s *Service) reconcileStore(
	ctx context.Context, ledger *projectLedger, files []photos.CataloguedFile,
) (storeReconciliation, error) {
	rec := newReconciler(ledger, files)
	err := s.store.ListObjects(ctx, func(obj StoreEntry) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("maintenance: store listing interrupted: %w", ctxErr)
		}
		rec.observe(obj)
		return nil
	})
	if err != nil {
		return storeReconciliation{}, fmt.Errorf("maintenance: listing the store: %w", err)
	}
	return rec.finish(), nil
}
