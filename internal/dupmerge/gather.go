package dupmerge

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Read queries that source a single association column for a set of photos. Each
// takes a []string of photo UIDs as $1 and is used both for the keeper (a
// one-element slice) and for the copies, so the diff is "copies minus keeper".
const (
	albumSourceSQL   = `SELECT DISTINCT album_uid FROM album_photos WHERE photo_uid = ANY($1)`
	labelSourceSQL   = `SELECT DISTINCT label_uid FROM photo_labels WHERE photo_uid = ANY($1)`
	subjectSourceSQL = `SELECT DISTINCT subject_uid FROM markers ` +
		`WHERE photo_uid = ANY($1) AND subject_uid IS NOT NULL AND invalid = FALSE`
	loadPhotosSQL = `SELECT uid, title, description, archived_at IS NOT NULL ` +
		`FROM photos WHERE uid = ANY($1)`
)

// buildPlan gathers the current album/label/people/scalar state of the keeper and
// its copies through q and computes the changes the merge will make: the
// associations the copies carry that the keeper lacks, the scalar gaps a copy can
// fill, and the still-active copies to archive. It returns ErrKeeperNotFound when
// the keeper photo does not exist.
func buildPlan(ctx context.Context, q querier, in Input) (plan, error) {
	photos, err := loadPhotos(ctx, q, in.MemberUIDs)
	if err != nil {
		return plan{}, err
	}
	keeper, ok := photos[in.KeeperUID]
	if !ok {
		return plan{}, ErrKeeperNotFound
	}
	copies := copyUIDs(in.MemberUIDs, in.KeeperUID, photos)

	albums, err := diffAssoc(ctx, q, albumSourceSQL, in.KeeperUID, copies)
	if err != nil {
		return plan{}, err
	}
	labels, err := diffAssoc(ctx, q, labelSourceSQL, in.KeeperUID, copies)
	if err != nil {
		return plan{}, err
	}
	subjects, err := diffAssoc(ctx, q, subjectSourceSQL, in.KeeperUID, copies)
	if err != nil {
		return plan{}, err
	}
	fill, err := buildScalarFill(ctx, q, in, keeper, copies, photos)
	if err != nil {
		return plan{}, err
	}
	return plan{
		albumsToAdd:   albums,
		labelsToAdd:   labels,
		subjectsToAdd: subjects,
		fill:          fill,
		archiveUIDs:   activeCopies(copies, photos),
	}, nil
}

// loadPhotos reads the scalar fields of every member that exists, keyed by UID.
// Unknown UIDs are simply absent from the map.
func loadPhotos(ctx context.Context, q querier, uids []string) (map[string]photoRow, error) {
	rows, err := q.Query(ctx, loadPhotosSQL, uids)
	if err != nil {
		return nil, fmt.Errorf("dupmerge: loading photos: %w", err)
	}
	defer rows.Close()

	out := make(map[string]photoRow, len(uids))
	for rows.Next() {
		var r photoRow
		if err := rows.Scan(&r.uid, &r.title, &r.description, &r.archived); err != nil {
			return nil, fmt.Errorf("dupmerge: scanning photo row: %w", err)
		}
		out[r.uid] = r
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dupmerge: iterating photo rows: %w", err)
	}
	return out, nil
}

// copyUIDs returns the members other than the keeper that actually exist,
// de-duplicated and in the caller's order.
func copyUIDs(members []string, keeperUID string, photos map[string]photoRow) []string {
	out := []string{}
	seen := make(map[string]bool, len(members))
	for _, uid := range members {
		if uid == keeperUID || seen[uid] {
			continue
		}
		if _, ok := photos[uid]; ok {
			seen[uid] = true
			out = append(out, uid)
		}
	}
	return out
}

// activeCopies returns the copies that are not already archived, i.e. the ones a
// merge still needs to archive.
func activeCopies(copies []string, photos map[string]photoRow) []string {
	out := []string{}
	for _, uid := range copies {
		if !photos[uid].archived {
			out = append(out, uid)
		}
	}
	return out
}

// diffAssoc returns the association UIDs the copies carry (via sql) that the
// keeper does not, sorted for a deterministic apply order. It short-circuits with
// no query when there are no copies or the copies carry no associations.
func diffAssoc(ctx context.Context, q querier, sql, keeperUID string, copies []string) ([]string, error) {
	if len(copies) == 0 {
		return nil, nil
	}
	copyVals, err := queryStrings(ctx, q, sql, copies)
	if err != nil {
		return nil, err
	}
	if len(copyVals) == 0 {
		return nil, nil
	}
	keeperVals, err := queryStrings(ctx, q, sql, []string{keeperUID})
	if err != nil {
		return nil, err
	}
	return subtract(copyVals, keeperVals), nil
}

// buildScalarFill decides which scalar fields to carry onto the keeper: the
// photo-column title/description from the copies (gaps only), and, when an actor
// is known, that user's favorite/rating/flag from the copies.
func buildScalarFill(
	ctx context.Context, q querier, in Input, keeper photoRow, copies []string, photos map[string]photoRow,
) (scalarFill, error) {
	copyTitles := make([]string, 0, len(copies))
	copyDescs := make([]string, 0, len(copies))
	for _, uid := range copies {
		copyTitles = append(copyTitles, photos[uid].title)
		copyDescs = append(copyDescs, photos[uid].description)
	}
	fill := scalarFill{
		title:       pickFill(keeper.title, copyTitles),
		description: pickFill(keeper.description, copyDescs),
	}
	if in.ActorUID == "" || len(copies) == 0 {
		return fill, nil
	}
	if err := addUserFills(ctx, q, in, copies, &fill); err != nil {
		return scalarFill{}, err
	}
	return fill, nil
}

// Per-user favorite/rating/flag read queries. The keeper query decides whether a
// gap exists; the copy query supplies the value that fills it.
const (
	keeperFavoriteSQL = `SELECT EXISTS(SELECT 1 FROM user_favorites WHERE user_uid = $1 AND photo_uid = $2)`
	copyFavoriteSQL   = `SELECT EXISTS(SELECT 1 FROM user_favorites WHERE user_uid = $1 AND photo_uid = ANY($2))`
	keeperRatingSQL   = `SELECT rating, flag FROM user_ratings WHERE user_uid = $1 AND photo_uid = $2`
	copyRatingSQL     = `SELECT rating FROM user_ratings ` +
		`WHERE user_uid = $1 AND photo_uid = ANY($2) AND rating > 0 ORDER BY rating DESC LIMIT 1`
	copyFlagSQL = `SELECT flag FROM user_ratings ` +
		`WHERE user_uid = $1 AND photo_uid = ANY($2) AND flag <> 'none' ORDER BY flag LIMIT 1`
)

// addUserFills fills the acting user's favorite, rating and flag onto fill from
// the copies, but only where the keeper has no such value of its own.
func addUserFills(ctx context.Context, q querier, in Input, copies []string, fill *scalarFill) error {
	favorite, err := fillFavorite(ctx, q, in.ActorUID, in.KeeperUID, copies)
	if err != nil {
		return err
	}
	fill.favorite = favorite

	rating, flag, err := fillRatingFlag(ctx, q, in.ActorUID, in.KeeperUID, copies)
	if err != nil {
		return err
	}
	fill.rating = rating
	fill.flag = flag
	return nil
}

// fillFavorite reports whether the keeper should be favorited: true only when the
// actor has not favorited the keeper but has favorited at least one copy.
func fillFavorite(ctx context.Context, q querier, actorUID, keeperUID string, copies []string) (bool, error) {
	keeperFav, err := queryBool(ctx, q, keeperFavoriteSQL, actorUID, keeperUID)
	if err != nil {
		return false, err
	}
	if keeperFav {
		return false, nil
	}
	return queryBool(ctx, q, copyFavoriteSQL, actorUID, copies)
}

// fillRatingFlag returns the rating and flag to carry onto the keeper from the
// copies, each non-nil only when the keeper lacks that value (rating 0 / flag
// "none") and a copy supplies one.
func fillRatingFlag(
	ctx context.Context, q querier, actorUID, keeperUID string, copies []string,
) (*int, *string, error) {
	keeperRating, keeperFlag, err := keeperRatingFlag(ctx, q, actorUID, keeperUID)
	if err != nil {
		return nil, nil, err
	}
	var rating *int
	if keeperRating == 0 {
		if rating, err = copyRating(ctx, q, actorUID, copies); err != nil {
			return nil, nil, err
		}
	}
	var flag *string
	if keeperFlag == "none" {
		if flag, err = copyFlag(ctx, q, actorUID, copies); err != nil {
			return nil, nil, err
		}
	}
	return rating, flag, nil
}

// keeperRatingFlag returns the actor's rating and flag on the keeper, defaulting
// to (0, "none") when no rating row exists.
func keeperRatingFlag(ctx context.Context, q querier, actorUID, keeperUID string) (int, string, error) {
	var rating int
	var flag string
	err := q.QueryRow(ctx, keeperRatingSQL, actorUID, keeperUID).Scan(&rating, &flag)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "none", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("dupmerge: reading keeper rating: %w", err)
	}
	return rating, flag, nil
}

// copyRating returns the highest rating any copy carries for the actor, or nil
// when none is rated.
func copyRating(ctx context.Context, q querier, actorUID string, copies []string) (*int, error) {
	var rating int
	err := q.QueryRow(ctx, copyRatingSQL, actorUID, copies).Scan(&rating)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // "no rating found" is legitimately (nil, nil) here
	}
	if err != nil {
		return nil, fmt.Errorf("dupmerge: reading copy rating: %w", err)
	}
	return &rating, nil
}

// copyFlag returns a non-"none" flag a copy carries for the actor (a pick is
// preferred over a reject by the query's ordering), or nil when none is flagged.
func copyFlag(ctx context.Context, q querier, actorUID string, copies []string) (*string, error) {
	var flag string
	err := q.QueryRow(ctx, copyFlagSQL, actorUID, copies).Scan(&flag)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // "no flag found" is legitimately (nil, nil) here
	}
	if err != nil {
		return nil, fmt.Errorf("dupmerge: reading copy flag: %w", err)
	}
	return &flag, nil
}

// queryStrings runs sql (which selects one text column) with uids bound to $1 and
// returns the collected values.
func queryStrings(ctx context.Context, q querier, sql string, uids []string) ([]string, error) {
	rows, err := q.Query(ctx, sql, uids)
	if err != nil {
		return nil, fmt.Errorf("dupmerge: querying associations: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("dupmerge: scanning association: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dupmerge: iterating associations: %w", err)
	}
	return out, nil
}

// queryBool runs sql (which selects one boolean) with the given args.
func queryBool(ctx context.Context, q querier, sql string, args ...any) (bool, error) {
	var v bool
	if err := q.QueryRow(ctx, sql, args...).Scan(&v); err != nil {
		return false, fmt.Errorf("dupmerge: reading boolean: %w", err)
	}
	return v, nil
}
