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

	"github.com/panbotka/kukatko/internal/storekeys"
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
// is no work. A failure that is not about this photo (fatal) is returned as the
// error; everything else is reported in the outcome.
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
// move.
func (s *Service) ensureCopied(ctx context.Context, move Move, keepExisting bool) (bool, error) {
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

// retryDerived moves the root thumbnails and streaming segments of one recorded
// photo under the prefix its row now points at, and forgets the photo once
// nothing of it is left at the root. A photo that is gone, or whose row is not
// under a prefix, has nothing to retry and is forgotten. Only a fatal failure is
// returned; a per-photo one refreshes the record.
func (s *Service) retryDerived(ctx context.Context, projectUID, photoUID string) error {
	photo, found, err := s.loadPhoto(ctx, projectUID, photoUID)
	if err != nil {
		return err
	}
	moves := rootDerivedMoves(photo)
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

// RootDerivedPhoto is one moved photo whose thumbnails or streaming segments a
// listing of the store still finds at the root: the photo, its project, and
// the move of each such object from its root key to its key under the prefix
// the photo's row points at.
type RootDerivedPhoto struct {
	// UID identifies the photo.
	UID string
	// ProjectUID is the photo's project.
	ProjectUID string
	// Moves are the root objects found, each with its key under the prefix,
	// sorted by root key.
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
// thumbnails or streaming segments a listing still finds at the root. Thumbnail
// and segment keys are named after the photo's content hash, which is unique
// within a project, so a root key matches at most one of its photos.
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
			found[uid] = append(found[uid], Move{From: key, To: storekeys.Join(prefix, key), Kind: tree.kind})
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

// sweepDerivedSQL records, without touching a photo already recorded, the
// photos the root sweep found; $3 holds each photo's kinds comma-joined.
const sweepDerivedSQL = `
INSERT INTO storage_rekey_derived_failures (photo_uid, project_uid, kinds, message, attempts)
SELECT t.uid, $2, string_to_array(t.kinds, ','), $4, 0
FROM unnest($1::text[], $3::text[]) AS t(uid, kinds)
ON CONFLICT (photo_uid) DO NOTHING`

// sweepRootDerived records every moved photo of run's project whose derived
// objects a listing still finds at the root and that is not recorded yet, and
// reports how many it recorded. When it recorded any, the run's derived cursor
// is reset so the retry pass reaches them whatever their uid.
func (s *Service) sweepRootDerived(ctx context.Context, run Run) (int64, error) {
	found, err := s.rootDerivedOf(ctx, run.ProjectUID, run.TargetPrefix)
	if err != nil || len(found) == 0 {
		return 0, err
	}
	uids := make([]string, 0, len(found))
	kinds := make([]string, 0, len(found))
	for _, photo := range found {
		var names []string
		for _, move := range photo.Moves {
			names = append(names, move.Kind.String())
		}
		slices.Sort(names)
		uids = append(uids, photo.UID)
		kinds = append(kinds, strings.Join(slices.Compact(names), ","))
	}
	tag, err := s.pool.Exec(ctx, sweepDerivedSQL, uids, run.ProjectUID, kinds, msgFoundAtRoot)
	if err != nil {
		return 0, fmt.Errorf("rekey: recording derived objects found at the root: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, nil
	}
	if _, err := s.pool.Exec(ctx, resetDerivedSQL, run.UID); err != nil {
		return 0, fmt.Errorf("rekey: restarting the derived retry: %w", err)
	}
	return tag.RowsAffected(), nil
}

// msgFoundAtRoot is the message of a photo the root sweep recorded.
const msgFoundAtRoot = "found at the root by the final sweep"
