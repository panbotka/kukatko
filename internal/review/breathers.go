package review

// Breather cards: the one card in a round that asks nothing.
//
// A round is ten decisions in a row, and a game made only of decisions is work
// with a scoreboard on it. The breather is the opposite card: a photo the player
// (or somebody) already said was good — five stars, or a favourite — with its
// title and its year, and nothing to answer. It is the reason to keep going that
// the questions themselves cannot be, because a question is by definition about
// something the machine is unsure of, and those are rarely the good pictures.
//
// It is read-only in the strongest sense: it has no id the answer endpoint would
// accept, it is carried outside the questions array, and it is typed as
// "breather" so a client cannot mistake one for a question even by accident. A
// client that has never heard of breathers ignores an extra JSON field and plays
// exactly as before.
//
// The picks are one per era, newest era first, so consecutive rounds rotating
// through them show the fifties after the two-thousands rather than four
// pictures from one wedding. That ordering is the whole of the era variation —
// there is no sampling and no randomness, which keeps a round reproducible.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/photos"
)

const (
	// BreatherKind is the type tag every breather card carries, so a client can
	// branch on it without inferring anything from which fields are present.
	BreatherKind = "breather"
	// breatherMinRating is the star rating a photo needs to qualify on merit
	// alone. Four rather than five: five stars is a shortlist people curate for a
	// print, and a library where nobody has ever used it would otherwise show no
	// breathers at all.
	breatherMinRating = 4
	// breatherPoolSize is how many candidates one lookup fetches — one per era,
	// so a round can rotate through the eras across a session without asking the
	// database again for each.
	breatherPoolSize = 8
	// breathersPerRound is how many breather cards accompany one round. One: the
	// card is a pause, and two pauses in ten questions is an interruption.
	breathersPerRound = 1
)

// BreatherReasonFavorite and BreatherReasonRated say why a photo was picked: the
// player favourited it, or somebody rated it highly.
const (
	BreatherReasonFavorite = "favorite"
	BreatherReasonRated    = "rated"
)

// BreatherPick is one candidate breather as the catalogue reports it: the photo
// and why it qualified. The photo record itself is hydrated by the caller
// through the store it already has, so this query stays a uid lookup.
type BreatherPick struct {
	// PhotoUID identifies the photo.
	PhotoUID string
	// Rating is the user's own star rating (0 when they have not rated it).
	Rating int
	// Favorite reports whether the user has favourited the photo.
	Favorite bool
}

// BreatherStore picks breather candidates straight from the catalogue. It is
// read-only and safe for concurrent use.
type BreatherStore struct {
	pool *pgxpool.Pool
}

// NewBreatherStore returns a BreatherStore backed by pool.
func NewBreatherStore(pool *pgxpool.Pool) *BreatherStore {
	return &BreatherStore{pool: pool}
}

// breatherQuery picks the best-liked photo of each era for one user. The inner
// select joins the two per-user opinion tables ($1 is the user) and keeps only
// the photos the library would show anyway — not archived, not a hidden
// document, not a non-primary stack member — while DISTINCT ON collapses each
// decade to its highest-rated member. $2 is the rating floor, $3 the cap on how
// many eras come back.
const breatherQuery = `
SELECT DISTINCT ON (decade) photo_uid, rating, favorite
FROM (
    SELECT p.uid AS photo_uid,
           COALESCE(r.rating, 0) AS rating,
           (f.photo_uid IS NOT NULL) AS favorite,
           COALESCE(EXTRACT(YEAR FROM p.taken_at)::int, 0) / 10 AS decade
    FROM photos p
    LEFT JOIN user_ratings r ON r.photo_uid = p.uid AND r.user_uid = $1
    LEFT JOIN user_favorites f ON f.photo_uid = p.uid AND f.user_uid = $1
    WHERE p.archived_at IS NULL
      AND (p.stack_uid IS NULL OR p.stack_primary)
      AND p.hidden_from_library = FALSE
      AND (r.rating >= $2 OR f.photo_uid IS NOT NULL)
) liked
ORDER BY decade DESC, rating DESC, favorite DESC, photo_uid
LIMIT $3`

// PickBreathers returns up to limit breather candidates for the user, at most
// one per era and newest era first. A user with no favourites and no high
// ratings yields an empty slice, not an error — a library nobody has curated yet
// simply has no breathers to show.
func (s *BreatherStore) PickBreathers(
	ctx context.Context, userUID string, limit int,
) ([]BreatherPick, error) {
	if limit <= 0 {
		limit = breatherPoolSize
	}
	rows, err := s.pool.Query(ctx, breatherQuery, userUID, breatherMinRating, limit)
	if err != nil {
		return nil, fmt.Errorf("review: querying breather candidates: %w", err)
	}
	defer rows.Close()

	var picks []BreatherPick
	for rows.Next() {
		var pick BreatherPick
		if err := rows.Scan(&pick.PhotoUID, &pick.Rating, &pick.Favorite); err != nil {
			return nil, fmt.Errorf("review: scanning breather candidate: %w", err)
		}
		picks = append(picks, pick)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("review: iterating breather candidates: %w", err)
	}
	return picks, nil
}

// breathersFor builds the round's breather cards: it takes the round's turn out
// of the rotating pick list, hydrates those photos through the store the queue
// already uses and stamps their media URLs.
//
// Every failure degrades to no breather at all. The card is a garnish on a queue
// that has to keep working when the box is offline and half the library's jobs
// are pending, so it must never be the reason a round fails to load.
func (s *Service) breathersFor(ctx context.Context, userUID string, sequence int) []Breather {
	if s.breathers == nil || s.photos == nil {
		return nil
	}
	picks, err := s.breathers.PickBreathers(ctx, userUID, breatherPoolSize)
	if err != nil {
		s.log.WarnContext(ctx, "review: picking breathers failed", "error", err)
		return nil
	}
	turn := rotatePicks(picks, sequence, breathersPerRound)
	if len(turn) == 0 {
		return nil
	}
	uids := make([]string, 0, len(turn))
	for _, pick := range turn {
		uids = append(uids, pick.PhotoUID)
	}
	byUID, err := s.photosByUID(ctx, uids)
	if err != nil {
		s.log.WarnContext(ctx, "review: loading breather photos failed", "error", err)
		return nil
	}
	return s.breatherCards(turn, byUID)
}

// breatherCards turns the round's picks into cards, dropping any whose photo has
// gone or has since been hidden from the library.
func (s *Service) breatherCards(
	picks []BreatherPick, byUID map[string]photos.Photo,
) []Breather {
	cards := make([]Breather, 0, len(picks))
	for _, pick := range picks {
		photo, ok := byUID[pick.PhotoUID]
		if !ok || photo.HiddenFromLibrary {
			continue
		}
		s.media.DecorateOne(&photo)
		cards = append(cards, Breather{
			Kind:   BreatherKind,
			Photo:  photo,
			Title:  breatherTitle(photo),
			Year:   breatherYear(photo),
			Reason: breatherReason(pick),
		})
	}
	if len(cards) == 0 {
		return nil
	}
	return cards
}

// rotatePicks takes count picks starting at the round's turn, wrapping past the
// end. It is what makes consecutive rounds show different eras: the picks are
// one per era, so advancing by one round advances by one era.
func rotatePicks(picks []BreatherPick, sequence, count int) []BreatherPick {
	if len(picks) == 0 || count <= 0 {
		return nil
	}
	count = min(count, len(picks))
	start := wrapOffset(sequence, len(picks))
	out := make([]BreatherPick, 0, count)
	for i := range count {
		out = append(out, picks[(start+i)%len(picks)])
	}
	return out
}

// breatherTitle is what the card is captioned with: the photo's title, falling
// back to the file name so a card is never captioned with nothing.
func breatherTitle(photo photos.Photo) string {
	if photo.Title != "" {
		return photo.Title
	}
	return photo.FileName
}

// breatherYear is the year the photo was taken, or zero when it has no date —
// the client then simply shows no year rather than the year nought.
func breatherYear(photo photos.Photo) int {
	if photo.TakenAt == nil {
		return 0
	}
	return photo.TakenAt.Year()
}

// breatherReason says why the photo qualified, preferring the player's own
// favourite over a rating somebody gave it.
func breatherReason(pick BreatherPick) string {
	if pick.Favorite {
		return BreatherReasonFavorite
	}
	return BreatherReasonRated
}
