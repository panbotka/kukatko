//go:build integration

package photos_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/query"
)

// These tests are the store-level tenant isolation of the photo read side: two
// projects seeded by internal/projecttest, one photo each, and every scoped
// read asked once per project. A read that ignored the project would return
// both photos to both sides.

// photoUIDs returns the uids of list, in order.
func photoUIDs(list []photos.Photo) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, p.UID)
	}
	return out
}

// TestScopedListing_seesOnlyItsProject covers the listing path — List with the
// orderings the grid and the slideshow use, Search, FilterUIDs and the
// query-language keys that resolve a name in another root table — through
// AssertStoreScoped.
func TestScopedListing_seesOnlyItsProject(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()

	listings := map[string]photos.ListParams{
		"default":    {},
		"created_at": {Sort: photos.SortByCreatedAt, Order: photos.OrderAsc},
		"title":      {Sort: photos.SortByTitle},
		"random":     {Sort: photos.SortByRandom, Seed: "s1"},
		"chronology": {Sort: photos.SortByChronology},
		// Each tenant's names share the word, so an unscoped name lookup would
		// match the other side's album, label, person and task as well.
		"album:":  {QueryFilters: query.Parse("album:Album").Filters},
		"label:":  {QueryFilters: query.Parse("label:Štítek").Filters},
		"person:": {QueryFilters: query.Parse("person:Osoba").Filters},
		"family:": {QueryFilters: query.Parse("family:Osoba").Filters},
		"task:":   {QueryFilters: query.Parse("task:Úkol").Filters},
		"text":    {Search: "Fotka"},
	}
	for name, params := range listings {
		t.Run(name, func(t *testing.T) {
			f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
				list, err := store.List(ctx, projectUID, params)
				return photoUIDs(list), err
			})
		})
	}
	t.Run("search", func(t *testing.T) {
		f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
			list, err := store.Search(ctx, projectUID, photos.ListParams{FullText: "Fotka"})
			return photoUIDs(list), err
		})
	})
	t.Run("filter uids", func(t *testing.T) {
		both := []string{f.A.PhotoUID, f.B.PhotoUID}
		f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
			list, err := store.FilterUIDs(ctx, projectUID, both, photos.ListParams{})
			return photoUIDs(list), err
		})
	})
	t.Run("list by uids", func(t *testing.T) {
		both := []string{f.A.PhotoUID, f.B.PhotoUID}
		f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
			list, err := store.ListByUIDsInProject(ctx, projectUID, both)
			return photoUIDs(list), err
		})
	})
}

// TestScopedListing_foreignIDsMatchNothing verifies that naming the other
// project's album, label, person, task or photo by its exact uid finds nothing
// in B — the uid arm of each filter is confined to the project, not just the
// photo it ends up on.
func TestScopedListing_foreignIDsMatchNothing(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()

	for _, q := range []string{
		"album:" + f.A.AlbumUID, "label:" + f.A.LabelUID, "person:" + f.A.SubjectUID,
		"family:" + f.A.SubjectUID, "task:" + f.A.TaskUID, "uid:" + f.A.PhotoUID, "near:" + f.A.PhotoUID,
	} {
		list, err := store.List(ctx, f.B.ProjectUID, photos.ListParams{QueryFilters: query.Parse(q).Filters})
		if err != nil {
			t.Fatalf("List(%q) in B: %v", q, err)
		}
		if len(list) != 0 {
			t.Errorf("List(%q) in B = %v, want nothing", q, photoUIDs(list))
		}
	}
}

// TestScopedAggregations_countOnlyItsProject covers the timeline, the year
// buckets, the counts and the uploader facet. Both seeded photos share a month,
// so the buckets themselves cannot tell the sides apart; the counts can — each
// side holds exactly one photo — and the uploader facet names the uploader.
func TestScopedAggregations_countOnlyItsProject(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()

	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		projectUID := f.Tenant(side).ProjectUID
		timeline, err := store.TimelineBuckets(ctx, projectUID, photos.ListParams{})
		if err != nil {
			t.Fatalf("TimelineBuckets(%s): %v", side, err)
		}
		if timeline.Total != 1 || len(timeline.Buckets) != 1 || timeline.Buckets[0].Count != 1 {
			t.Errorf("timeline of %s = %+v, want one bucket of one photo", side, timeline)
		}
		years, err := store.YearBuckets(ctx, projectUID, photos.ListParams{})
		if err != nil {
			t.Fatalf("YearBuckets(%s): %v", side, err)
		}
		if years.Total != 1 || len(years.Years) != 1 || years.Years[0].Count != 1 {
			t.Errorf("years of %s = %+v, want one year of one photo", side, years)
		}
		counts, err := store.CountMedia(ctx, projectUID, photos.ListParams{})
		if err != nil {
			t.Fatalf("CountMedia(%s): %v", side, err)
		}
		if counts.Total != 1 {
			t.Errorf("CountMedia(%s).Total = %d, want 1", side, counts.Total)
		}
	}
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		buckets, err := store.UploaderBuckets(ctx, projectUID, photos.ListParams{})
		uids := make([]string, 0, len(buckets))
		for _, b := range buckets {
			uids = append(uids, b.UID)
		}
		return uids, err
	})
}

// TestScopedLookups_answerOnlyForTheirProject covers the single-row lookups. The
// same bytes, PhotoPrism uid, PhotoPrism file hash, photo-sorter uid and
// PhotoPrism alias are catalogued in both projects, so a lookup ignoring the
// project would hand one of the two photos to both sides.
func TestScopedLookups_answerOnlyForTheirProject(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()

	twinUID := projecttest.MissingUID("ph")
	if _, err := db.Pool().Exec(ctx, `INSERT INTO photos (uid, project_id, file_hash, file_path, file_name,
		file_size, file_mime) VALUES ($1, $2, $3, $4, 'twin.jpg', 1024, 'image/jpeg')`,
		twinUID, f.B.ProjectUID, f.A.PhotoFileHash, "2026/10/"+twinUID+".jpg"); err != nil {
		t.Fatalf("cataloguing A's bytes in B: %v", err)
	}
	for _, uid := range []string{f.A.PhotoUID, twinUID} {
		if _, err := db.Pool().Exec(ctx, `UPDATE photos SET photoprism_uid = 'pt-shared',
			photoprism_file_hash = 'sha1-shared', photosorter_uid = 'ps-shared' WHERE uid = $1`, uid); err != nil {
			t.Fatalf("stamping the shared source ids on %s: %v", uid, err)
		}
		if err := store.AddPhotoprismAlias(ctx, "pt-alias", uid, ""); err != nil {
			t.Fatalf("AddPhotoprismAlias(%s): %v", uid, err)
		}
	}

	lookups := map[string]func(projectUID string) (photos.Photo, error){
		"by hash": func(p string) (photos.Photo, error) { return store.GetByFileHashInProject(ctx, p, f.A.PhotoFileHash) },
		"by photoprism uid": func(p string) (photos.Photo, error) {
			return store.GetByPhotoprismUID(ctx, p, "pt-shared")
		},
		"by photoprism file hash": func(p string) (photos.Photo, error) {
			return store.GetByPhotoprismFileHash(ctx, p, "sha1-shared")
		},
		"by photosorter uid": func(p string) (photos.Photo, error) { return store.GetByPhotosorterUID(ctx, p, "ps-shared") },
		"by alias":           func(p string) (photos.Photo, error) { return store.GetByPhotoprismAlias(ctx, p, "pt-alias") },
	}
	for name, lookup := range lookups {
		t.Run(name, func(t *testing.T) {
			f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
				photo, err := lookup(projectUID)
				return []string{photo.UID}, err
			})
		})
	}
}

// TestScopedGet_foreignUIDIsNotFound verifies the reads keyed by a photo uid
// answer for another project's photo exactly as for a missing one.
func TestScopedGet_foreignUIDIsNotFound(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()

	if _, err := store.GetByUIDInProject(ctx, f.B.ProjectUID, f.B.PhotoUID); err != nil {
		t.Fatalf("B's own photo in B: %v", err)
	}
	if _, err := store.GetByUIDInProject(ctx, f.B.ProjectUID, f.A.PhotoUID); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("A's photo in B = %v, want ErrPhotoNotFound", err)
	}
	if _, err := store.GetOCR(ctx, f.B.ProjectUID, f.A.PhotoUID); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("OCR of A's photo in B = %v, want ErrPhotoNotFound", err)
	}
	if _, err := store.GetByUIDInProject(ctx, "", f.A.PhotoUID); !errors.Is(err, photos.ErrNoProject) {
		t.Errorf("no project = %v, want ErrNoProject", err)
	}
}

// TestScopedStacks_seeOnlyTheirProject verifies a stack uid shared by photos of
// two projects — which only a bug could produce, so the read must not trust it —
// lists and counts only the project's own members.
func TestScopedStacks_seeOnlyTheirProject(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()

	// One primary per stack uid is a database invariant, so A's photo is it.
	if _, err := db.Pool().Exec(ctx, `UPDATE photos SET stack_uid = 'st-shared', stack_primary = (uid = $2)
		WHERE uid = ANY($1)`, []string{f.A.PhotoUID, f.B.PhotoUID}, f.A.PhotoUID); err != nil {
		t.Fatalf("stacking: %v", err)
	}
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		members, err := store.ListStackMembers(ctx, projectUID, "st-shared")
		return photoUIDs(members), err
	})
	counts, err := store.StackCounts(ctx, f.B.ProjectUID, []string{"st-shared"})
	if err != nil {
		t.Fatalf("StackCounts: %v", err)
	}
	if counts["st-shared"] != 1 {
		t.Errorf("StackCounts in B = %v, want 1 member", counts)
	}
}
