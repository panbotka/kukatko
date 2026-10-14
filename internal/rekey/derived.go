package rekey

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/storekeys"
	"github.com/panbotka/kukatko/internal/thumb"
)

// trackedKind reports whether a failed move of a derived object of kind is
// recorded and retried: thumbnails and streaming segments, the objects a signed
// URL names and a viewer fetches. The sidecar is not — Finish rewrites every
// sidecar of the project under the new prefix and removes the root ones that
// have their replacement.
func trackedKind(kind storekeys.Kind) bool {
	return kind == storekeys.KindThumbnail || kind == storekeys.KindHLS
}

// splitTracked separates the derived moves whose failure is tracked from the
// others, keeping the order of each.
func splitTracked(moves []Move) (tracked, others []Move) {
	for _, move := range moves {
		if trackedKind(move.Kind) {
			tracked = append(tracked, move)
		} else {
			others = append(others, move)
		}
	}
	return tracked, others
}

// derivedOutcome is what moving one photo's tracked derived objects came to:
// the kinds that are still at the root, and why.
type derivedOutcome struct {
	// failed are the kinds left at the root, sorted by name, without duplicates.
	failed []string
	// problem joins every per-object error; nil when every object moved.
	problem error
}

// moveTracked moves one photo's thumbnails and streaming segments from their
// root keys to their prefixed ones, in two phases: first every object is made
// to exist under the new key, then — only when every one of them does — the
// root copies are deleted. A copy that fails therefore deletes nothing of the
// photo's, so a later retry still finds every source.
//
// With keepExisting set (a retry), an object already under its new key is not
// copied over — a thumbnail rendered there since the move is newer than the
// root one — and the root copy is simply deleted. A source that does not exist
// is no work. A drop (a Move without To) copies nothing and is deleted in the
// second phase with the rest. A failure that is not about this photo (fatal) is
// returned as the error; everything else is reported in the outcome.
func (s *Service) moveTracked(ctx context.Context, moves []Move, keepExisting bool) (derivedOutcome, error) {
	var (
		present  []Move
		failed   []string
		problems []error
	)
	for _, move := range moves {
		there, err := s.ensureCopied(ctx, move, keepExisting)
		if err != nil {
			if fatal(ctx, err) {
				return derivedOutcome{}, err
			}
			failed = append(failed, move.Kind.String())
			problems = append(problems, err)
			continue
		}
		if there {
			present = append(present, move)
		}
	}
	if len(problems) > 0 {
		return newOutcome(failed, problems), nil
	}
	for _, move := range present {
		err := s.store.Delete(ctx, move.From)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			continue
		}
		err = fmt.Errorf("rekey: deleting old %s %s: %w", move.Kind, move.From, err)
		if fatal(ctx, err) {
			return derivedOutcome{}, err
		}
		failed = append(failed, move.Kind.String())
		problems = append(problems, err)
	}
	return newOutcome(failed, problems), nil
}

// ensureCopied makes sure move.To holds the object and reports whether its root
// copy is now safe to delete: false only when there was nothing at the root to
// move. A drop (no To) has nothing to copy and is always safe to delete.
func (s *Service) ensureCopied(ctx context.Context, move Move, keepExisting bool) (bool, error) {
	if move.To == "" {
		return true, nil
	}
	if keepExisting {
		_, err := s.store.Stat(ctx, move.To)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("rekey: checking %s %s: %w", move.Kind, move.To, err)
		}
	}
	err := s.store.Copy(ctx, move.From, move.To)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("rekey: copying %s %s: %w", move.Kind, move.From, err)
	}
	return true, nil
}

// newOutcome builds an outcome from the failed kinds and the problems met.
func newOutcome(failed []string, problems []error) derivedOutcome {
	slices.Sort(failed)
	return derivedOutcome{failed: slices.Compact(failed), problem: errors.Join(problems...)}
}

// recordDerivedSQL records (or refreshes) a photo whose derived objects are
// still at the root.
const recordDerivedSQL = `
INSERT INTO storage_rekey_derived_failures (photo_uid, project_uid, kinds, message)
VALUES ($1, $2, $3, $4)
ON CONFLICT (photo_uid) DO UPDATE SET kinds = EXCLUDED.kinds, message = EXCLUDED.message,
	attempts = storage_rekey_derived_failures.attempts + 1, updated_at = now()`

// clearDerivedSQL forgets a photo whose derived objects have all moved.
const clearDerivedSQL = `DELETE FROM storage_rekey_derived_failures WHERE photo_uid = $1 AND project_uid = $2`

// noteDerived records the photo when outcome left derived objects at the root
// and forgets it otherwise.
func (s *Service) noteDerived(ctx context.Context, projectUID, photoUID string, outcome derivedOutcome) error {
	if len(outcome.failed) == 0 {
		if _, err := s.pool.Exec(ctx, clearDerivedSQL, photoUID, projectUID); err != nil {
			return fmt.Errorf("rekey: forgetting the derived failure of %s: %w", photoUID, err)
		}
		return nil
	}
	message := ""
	if outcome.problem != nil {
		message = outcome.problem.Error()
	}
	if _, err := s.pool.Exec(ctx, recordDerivedSQL, photoUID, projectUID, outcome.failed, message); err != nil {
		return fmt.Errorf("rekey: recording the derived failure of %s: %w", photoUID, err)
	}
	return nil
}

// derivedBatchSQL reads the next recorded photos of a project after the
// derived cursor.
const derivedBatchSQL = `SELECT photo_uid FROM storage_rekey_derived_failures
WHERE project_uid = $1 AND photo_uid > $2 ORDER BY photo_uid LIMIT $3`

// advanceDerivedSQL moves the run's derived cursor past one photo and returns
// the run's state, so the caller notices a pause.
const advanceDerivedSQL = `UPDATE storage_rekey_runs SET derived_cursor = $2, updated_at = now()
WHERE uid = $1 RETURNING state`

// resetDerivedSQL restarts the run's retry of derived objects.
const resetDerivedSQL = `UPDATE storage_rekey_runs SET derived_cursor = '', updated_at = now() WHERE uid = $1`

// derivedBatch returns the uids of the next recorded photos of run's project
// after its derived cursor.
func (s *Service) derivedBatch(ctx context.Context, run Run, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx, derivedBatchSQL, run.ProjectUID, run.DerivedCursor, limit)
	if err != nil {
		return nil, fmt.Errorf("rekey: listing photos with derived objects to move: %w", err)
	}
	uids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("rekey: reading photos with derived objects to move: %w", err)
	}
	return uids, nil
}

// stepDerived retries the derived objects of up to batchSize recorded photos
// and reports how many it took and whether none was left. Each photo advances
// the derived cursor, so a pass tries each recorded photo once; a pause that
// lands mid-batch is honoured after the photo in hand.
func (s *Service) stepDerived(ctx context.Context, run Run, batchSize int) (int, bool, error) {
	uids, err := s.derivedBatch(ctx, run, batchSize)
	if err != nil {
		return 0, false, err
	}
	if len(uids) == 0 {
		return 0, true, nil
	}
	processed := 0
	for _, uid := range uids {
		if err := s.retryDerived(ctx, run.ProjectUID, uid); err != nil {
			return processed, false, err
		}
		processed++
		var state string
		if err := s.pool.QueryRow(ctx, advanceDerivedSQL, run.UID, uid).Scan(&state); err != nil {
			return processed, false, fmt.Errorf("rekey: advancing the derived cursor: %w", err)
		}
		if State(state) != StateRunning {
			return processed, false, stoppedErr(State(state))
		}
	}
	return processed, false, nil
}

// rootKeysSQL reads the root keys the final sweep recorded for one photo.
const rootKeysSQL = `SELECT root_keys FROM storage_rekey_derived_failures WHERE photo_uid = $1 AND project_uid = $2`

// retryDerived moves the root thumbnails and streaming segments of one recorded
// photo under the prefix its row now points at, and forgets the photo once
// nothing of it is left at the root. It handles the keys its own plan knows and
// every root key the final sweep recorded for the photo (sweptMoves) — the two
// agree on what the photo owns, so a stray the plan does not know cannot keep
// the move from ending. A photo that is gone, or whose row is not under a
// prefix, has nothing to retry and is forgotten. Only a fatal failure is
// returned; a per-photo one refreshes the record.
func (s *Service) retryDerived(ctx context.Context, projectUID, photoUID string) error {
	photo, found, err := s.loadPhoto(ctx, projectUID, photoUID)
	if err != nil {
		return err
	}
	moves := rootDerivedMoves(photo)
	if found && moves != nil {
		if moves, err = s.withSwept(ctx, projectUID, photo, moves); err != nil {
			return err
		}
	}
	if !found || moves == nil {
		_, err := s.pool.Exec(ctx, clearDerivedSQL, photoUID, projectUID)
		if err != nil {
			return fmt.Errorf("rekey: forgetting the derived failure of %s: %w", photoUID, err)
		}
		return nil
	}
	outcome, err := s.moveTracked(ctx, moves, true)
	if err != nil {
		return err
	}
	if outcome.problem != nil {
		s.log.WarnContext(ctx, "rekey: derived objects still at the root",
			"photo", photoUID, "kinds", outcome.failed, "error", outcome.problem)
	}
	return s.noteDerived(ctx, projectUID, photoUID, outcome)
}

// rootDerivedMoves plans, for a photo whose row already points under a
// prefix, the moves of its thumbnails and streaming segments from the root
// layout to that prefix. It returns nil for a photo still in the root layout.
func rootDerivedMoves(photo PhotoFiles) []Move {
	prefix, rest := storekeys.Strip(photo.FilePath)
	if prefix == "" {
		return nil
	}
	root := photo
	root.FilePath = rest
	planned := Plan(root, prefix)
	if len(planned) == 0 {
		return nil
	}
	tracked, _ := splitTracked(planned[1:])
	if tracked == nil {
		tracked = []Move{}
	}
	return tracked
}

// withSwept appends to the planned moves of photo those of the root keys the
// final sweep recorded for it (sweptMoves).
func (s *Service) withSwept(ctx context.Context, projectUID string, photo PhotoFiles, planned []Move) ([]Move, error) {
	var swept []string
	err := s.pool.QueryRow(ctx, rootKeysSQL, photo.UID, projectUID).Scan(&swept)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("rekey: reading the root keys of %s: %w", photo.UID, err)
	}
	return append(planned, sweptMoves(photo, swept, planned)...), nil
}

// sweptMoves turns the root keys the final sweep recorded for photo into the
// moves the retry makes of them, skipping a key planned already holds. Only keys
// of the root thumbnail and streaming trees named after the photo's own content
// hash are taken (rootDerivedMove); anything else recorded is ignored, so the
// retry can never touch an original or another photo's object.
func sweptMoves(photo PhotoFiles, keys []string, planned []Move) []Move {
	prefix := storekeys.PrefixOf(photo.FilePath)
	known := make(map[string]bool, len(planned))
	for _, move := range planned {
		known[move.From] = true
	}
	var out []Move
	for _, key := range keys {
		if known[key] {
			continue
		}
		kind := storekeys.Classify(key)
		if !trackedKind(kind) || storekeys.PrefixOf(key) != "" || rootKeyHash(kind, key) != photo.FileHash {
			continue
		}
		known[key] = true
		out = append(out, rootDerivedMove(key, kind, photo.FileHash, prefix))
	}
	return out
}

// rootDerivedMove says what becomes of one root thumbnail or streaming object of
// the photo with the given content hash, whose row points under prefix: a key the
// layout's own builder spells for that hash (thumb.RelPath for a registered
// size, hls.Key for a valid rendition and name) moves to the same key under the
// prefix; any other key in those trees — a thumbnail size no longer registered, a
// name the streaming layout cannot hold — is never served, so it is dropped (a
// Move without To: deleted, not copied). Both trees are regenerable from the
// original; an original is never a thumbnail or streaming key.
func rootDerivedMove(key string, kind storekeys.Kind, hash, prefix string) Move {
	if validRootDerived(kind, key, hash) {
		return Move{From: key, To: storekeys.Join(prefix, key), Kind: kind}
	}
	return Move{From: key, Kind: kind}
}

// validRootDerived reports whether key is exactly what the layout of kind builds
// for a root-layout object of the photo with the given content hash.
func validRootDerived(kind storekeys.Kind, key, hash string) bool {
	switch kind {
	case storekeys.KindThumbnail:
		size := strings.TrimSuffix(strings.TrimPrefix(path.Base(key), hash+"_"), ".jpg")
		built, err := thumb.RelPath("", hash, size)
		return err == nil && built == key
	case storekeys.KindHLS:
		parts := strings.Split(key, "/")
		if len(parts) != hlsKeyParts {
			return false
		}
		built, err := hls.Key("", hash, parts[2], parts[3])
		return err == nil && built == key
	case storekeys.KindOriginal, storekeys.KindSidecar, storekeys.KindFamilies,
		storekeys.KindDump, storekeys.KindPartial, storekeys.KindForeign:
		return false
	}
	return false
}

// hlsKeyParts is how many slash-separated parts a root streaming key has:
// hls/<hash>/<rendition>/<name>.
const hlsKeyParts = 4

// rootKeyHash returns the content hash a root key of kind is named after, or ""
// for a key of another kind or shape.
func rootKeyHash(kind storekeys.Kind, key string) string {
	switch kind {
	case storekeys.KindThumbnail:
		return thumbKeyHash(key)
	case storekeys.KindHLS:
		return hlsKeyHash(key)
	case storekeys.KindOriginal, storekeys.KindSidecar, storekeys.KindFamilies,
		storekeys.KindDump, storekeys.KindPartial, storekeys.KindForeign:
		return ""
	}
	return ""
}

// RootDerivedPhoto is one moved photo whose thumbnails or streaming segments a
// listing of the store still finds at the root: the photo, its project, and
// the move of each such object from its root key to its key under the prefix
// the photo's row points at.
type RootDerivedPhoto struct {
	// UID identifies the photo.
	UID string
	// ProjectUID is the photo's project.
	ProjectUID string
	// Moves are the root objects found, sorted by root key: each with its key
	// under the prefix, or without one (a drop) for a key the layout cannot
	// hold (rootDerivedMove).
	Moves []Move
}

// RootDerivedResult is what MoveRootDerived did.
type RootDerivedResult struct {
	// Photos is how many photos had every root object moved.
	Photos int `json:"photos"`
	// Objects is how many root objects those photos had.
	Objects int `json:"objects"`
	// Failed is how many photos kept objects at the root.
	Failed int `json:"failed"`
}

// movedProjectsSQL lists the projects a storage move has touched that are no
// longer in the root layout: the only projects whose photos can have derived
// objects at the root.
const movedProjectsSQL = `SELECT uid, storage_prefix FROM projects
WHERE storage_prefix <> '' AND uid IN (SELECT project_uid FROM storage_rekey_runs) ORDER BY uid`

// prefixedPhotosSQL reads every photo of a project whose row points under the
// given prefix.
const prefixedPhotosSQL = `SELECT uid, file_hash FROM photos
WHERE project_id = $1 AND starts_with(file_path, $2) ORDER BY uid`

// FindRootDerived lists, for every project a storage move has moved under its
// own prefix, the moved photos whose thumbnails or streaming segments are still
// at the root of the store. It reads the bucket listing — never a local
// thumbnail cache — and only the root thumbnail and streaming trees, matching
// each key to a moved photo by the content hash it is named after. Read-only.
func (s *Service) FindRootDerived(ctx context.Context) ([]RootDerivedPhoto, error) {
	rows, err := s.pool.Query(ctx, movedProjectsSQL)
	if err != nil {
		return nil, fmt.Errorf("rekey: listing moved projects: %w", err)
	}
	type moved struct{ uid, prefix string }
	projects, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (moved, error) {
		var p moved
		err := row.Scan(&p.uid, &p.prefix)
		return p, err //nolint:wrapcheck // CollectRows' caller wraps it below.
	})
	if err != nil {
		return nil, fmt.Errorf("rekey: reading moved projects: %w", err)
	}
	var out []RootDerivedPhoto
	for _, p := range projects {
		found, err := s.rootDerivedOf(ctx, p.uid, p.prefix)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	return out, nil
}

// rootDerivedOf lists the photos of one project, moved under prefix, whose
// thumbnails or streaming segments a listing still finds at the root — every
// object there, not only the keys a plan of the photo would name. Thumbnail and
// segment keys are named after the photo's content hash, which is unique within
// a project, so a root key matches at most one of its photos. It lists each of
// the two root trees once, whatever the number of photos.
func (s *Service) rootDerivedOf(ctx context.Context, projectUID, prefix string) ([]RootDerivedPhoto, error) {
	byHash, err := s.prefixedHashes(ctx, projectUID, prefix)
	if err != nil || len(byHash) == 0 {
		return nil, err
	}
	found := map[string][]Move{}
	for _, tree := range []struct {
		dir  string
		kind storekeys.Kind
		hash func(string) string
	}{
		{storekeys.ThumbnailDir + "/", storekeys.KindThumbnail, thumbKeyHash},
		{storekeys.HLSDir + "/", storekeys.KindHLS, hlsKeyHash},
	} {
		err := s.store.KeysWithPrefix(ctx, tree.dir, func(key string) error {
			if !strings.HasPrefix(key, tree.dir) {
				return nil
			}
			uid, ok := byHash[tree.hash(key)]
			if !ok {
				return nil
			}
			found[uid] = append(found[uid], rootDerivedMove(key, tree.kind, tree.hash(key), prefix))
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("rekey: listing %s: %w", tree.dir, err)
		}
	}
	out := make([]RootDerivedPhoto, 0, len(found))
	for uid, moves := range found {
		slices.SortFunc(moves, func(a, b Move) int { return strings.Compare(a.From, b.From) })
		out = append(out, RootDerivedPhoto{UID: uid, ProjectUID: projectUID, Moves: moves})
	}
	slices.SortFunc(out, func(a, b RootDerivedPhoto) int { return strings.Compare(a.UID, b.UID) })
	return out, nil
}

// prefixedHashes maps the content hash of every photo of the project whose row
// points under prefix to the photo's uid.
func (s *Service) prefixedHashes(ctx context.Context, projectUID, prefix string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, prefixedPhotosSQL, projectUID, prefix)
	if err != nil {
		return nil, fmt.Errorf("rekey: listing moved photos: %w", err)
	}
	byHash := map[string]string{}
	var uid, hash string
	_, err = pgx.ForEachRow(rows, []any{&uid, &hash}, func() error {
		byHash[hash] = uid
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rekey: reading moved photos: %w", err)
	}
	return byHash, nil
}

// thumbKeyHash returns the content hash a thumbnail key is named after:
// thumb/aa/bb/cc/<hash>_<size>.jpg. A key of another shape yields "".
func thumbKeyHash(key string) string {
	hash, _, found := strings.Cut(path.Base(key), "_")
	if !found {
		return ""
	}
	return hash
}

// hlsKeyHash returns the content hash a streaming key is named after:
// hls/<hash>/<rendition>/<name>. A key of another shape yields "".
func hlsKeyHash(key string) string {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) < 3 || parts[0] != storekeys.HLSDir {
		return ""
	}
	return parts[1]
}

// MoveRootDerived moves the root objects of every photo in found under the
// prefix its row points at — the same two phases a storage move's retry uses:
// an object already under its new key is kept and not copied over, and no root
// copy of a photo is deleted unless every one of them exists under the prefix.
// A photo that moves completely is also forgotten as a derived failure of a
// storage move. A failure that is not about one photo — a store that refuses
// every request, a cancelled context — is returned with what was done so far.
func (s *Service) MoveRootDerived(ctx context.Context, found []RootDerivedPhoto) (RootDerivedResult, error) {
	var result RootDerivedResult
	for _, photo := range found {
		outcome, err := s.moveTracked(ctx, photo.Moves, true)
		if err != nil {
			return result, err
		}
		if outcome.problem != nil {
			s.log.WarnContext(ctx, "rekey: root derived objects kept",
				"photo", photo.UID, "kinds", outcome.failed, "error", outcome.problem)
			result.Failed++
			continue
		}
		if _, err := s.pool.Exec(ctx, clearDerivedSQL, photo.UID, photo.ProjectUID); err != nil {
			return result, fmt.Errorf("rekey: forgetting the derived failure of %s: %w", photo.UID, err)
		}
		result.Photos++
		result.Objects += len(photo.Moves)
	}
	return result, nil
}

// sweepDerivedSQL records the root keys the sweep found, one row per photo:
// $1, $3 and $5 are parallel arrays of photo uid, kind and root key. A photo
// already recorded gains the keys it did not have yet and keeps its kinds and
// message — the retry's own account of what failed (one whose recorded keys hold
// every key found is not written at all). It returns one row per photo written,
// true for a photo recorded only now.
const sweepDerivedSQL = `
INSERT INTO storage_rekey_derived_failures (photo_uid, project_uid, kinds, message, attempts, root_keys)
SELECT t.uid, $2, array_agg(DISTINCT t.kind ORDER BY t.kind), $4, 0, array_agg(DISTINCT t.key ORDER BY t.key)
FROM unnest($1::text[], $3::text[], $5::text[]) AS t(uid, kind, key)
GROUP BY t.uid
ON CONFLICT (photo_uid) DO UPDATE SET
	root_keys = ARRAY(SELECT DISTINCT k FROM unnest(storage_rekey_derived_failures.root_keys || EXCLUDED.root_keys) AS k
		ORDER BY k),
	updated_at = now()
WHERE NOT (EXCLUDED.root_keys <@ storage_rekey_derived_failures.root_keys)
RETURNING xmax = 0`

// sweepRootDerived records every moved photo of run's project whose derived
// objects a listing still finds at the root, together with the keys found, and
// reports how many photos it recorded that were not recorded before. The retry
// pass then moves or drops exactly those keys besides its own plan's
// (sweptMoves), so the next sweep cannot find the same object again unless its
// move failed — and a photo whose move failed stays recorded, is not new, and so
// ends the run in ErrDerivedRemain instead of another round. A photo recorded
// already (its retry failed this pass) gains the keys found for the operator's
// Retry without counting. When it recorded a new photo, the run's derived cursor
// is reset so the retry pass reaches it whatever its uid. The root trees are
// listed once per call.
func (s *Service) sweepRootDerived(ctx context.Context, run Run) (int64, error) {
	found, err := s.rootDerivedOf(ctx, run.ProjectUID, run.TargetPrefix)
	if err != nil || len(found) == 0 {
		return 0, err
	}
	var uids, kinds, keys []string
	for _, photo := range found {
		for _, move := range photo.Moves {
			uids = append(uids, photo.UID)
			kinds = append(kinds, move.Kind.String())
			keys = append(keys, move.From)
		}
	}
	rows, err := s.pool.Query(ctx, sweepDerivedSQL, uids, run.ProjectUID, kinds, msgFoundAtRoot, keys)
	if err != nil {
		return 0, fmt.Errorf("rekey: recording derived objects found at the root: %w", err)
	}
	written, err := pgx.CollectRows(rows, pgx.RowTo[bool])
	if err != nil {
		return 0, fmt.Errorf("rekey: reading the derived objects recorded: %w", err)
	}
	fresh := int64(len(slices.DeleteFunc(written, func(inserted bool) bool { return !inserted })))
	if fresh == 0 {
		return 0, nil
	}
	if _, err := s.pool.Exec(ctx, resetDerivedSQL, run.UID); err != nil {
		return 0, fmt.Errorf("rekey: restarting the derived retry: %w", err)
	}
	return fresh, nil
}

// msgFoundAtRoot is the message of a photo the root sweep recorded.
const msgFoundAtRoot = "found at the root by the final sweep"
