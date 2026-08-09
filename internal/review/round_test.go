package review

// Round-level tests: what a Queue response is now that one request is one round,
// plus the two read-only extras a round carries — the breather card and the
// reveal an answer sends back. The mixer's own rules are asserted in
// mixer_test.go, against the pure function.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

// roundLibrary scripts enough people and labels that a rebuild fills its pool
// with material from both sides — several rounds' worth.
func roundLibrary(f *fixture) {
	for i := range 8 {
		f.sweeper.people = append(f.sweeper.people,
			scannedPerson(fmt.Sprintf("subj%02d", i), 0.05, 0.09, 0.30, 0.34))
		uid := fmt.Sprintf("lab%02d", i)
		f.organize.labels = append(f.organize.labels, labelCount(uid, 10))
		f.expander.results[uid] = labelResult(uid, 0.95, 0.91, 0.70, 0.66)
	}
}

func TestQueue_oneRequestIsOneRound(t *testing.T) {
	t.Parallel()
	f := newFixture(t, roundLibrary)
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != DefaultRoundSize {
		t.Fatalf("round = %d questions, want %d", len(res.Questions), DefaultRoundSize)
	}
	round := res.Round
	if round.Index != 1 || round.Size != DefaultRoundSize || round.Remaining != DefaultRoundSize {
		t.Errorf("round = %+v, want the first round of %d, none of it answered yet",
			round, DefaultRoundSize)
	}
	if round.Last {
		t.Error("round marked last, but the pool holds more than one round")
	}
	// The between-rounds summary has to add up to the round the player just saw.
	total := 0
	for _, count := range round.Kinds {
		total += count
	}
	if total != len(res.Questions) {
		t.Errorf("kind counts sum to %d, want %d (%v)", total, len(res.Questions), round.Kinds)
	}
	if round.Sure+round.Band != len(res.Questions) {
		t.Errorf("sure+band = %d, want %d — every question here carries a tier",
			round.Sure+round.Band, len(res.Questions))
	}
	if round.Entities < 2 {
		t.Errorf("round asks about %d entities, want a mixed round", round.Entities)
	}
	if res.Remaining <= len(res.Questions) {
		t.Errorf("pool = %d, want more than the round's %d behind it",
			res.Remaining, len(res.Questions))
	}
}

func TestQueue_refetchingBeforeAnsweringReturnsTheSameRound(t *testing.T) {
	t.Parallel()
	// The client prefetches and retries. A second fetch that minted a fresh round
	// would silently drop the questions already on screen.
	f := newFixture(t, roundLibrary)
	ctx := context.Background()
	first, err := f.svc.Queue(ctx, "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	second, err := f.svc.Queue(ctx, "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if mixIDs(first.Questions) != mixIDs(second.Questions) {
		t.Errorf("a refetch changed the round:\n%s\n%s",
			mixIDs(first.Questions), mixIDs(second.Questions))
	}
	if second.Round.Index != 1 {
		t.Errorf("round index = %d after a refetch, want it still to be the first",
			second.Round.Index)
	}
}

func TestQueue_answeringARoundThroughMintsTheNext(t *testing.T) {
	t.Parallel()
	f := newFixture(t, roundLibrary)
	ctx := context.Background()
	first, err := f.svc.Queue(ctx, "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	// Skipping settles a question without writing anything, which is all this
	// needs: the round shrinks the same way whatever the verdict was.
	for i, q := range first.Questions {
		if _, err := f.svc.Answer(ctx, "user", q.ID, AnswerSkip, audit.Meta{}); err != nil {
			t.Fatalf("Answer %d: %v", i, err)
		}
		if i < len(first.Questions)-1 {
			mid, midErr := f.svc.Queue(ctx, "user", SourceBoth, 0)
			if midErr != nil {
				t.Fatalf("Queue mid-round: %v", midErr)
			}
			if mid.Round.Index != 1 {
				t.Fatalf("round index = %d after %d answers, want the round to continue",
					mid.Round.Index, i+1)
			}
			if mid.Round.Size != DefaultRoundSize || mid.Round.Remaining != len(mid.Questions) {
				t.Fatalf("round = %+v after %d answers, want size %d and %d remaining",
					mid.Round, i+1, DefaultRoundSize, len(mid.Questions))
			}
		}
	}
	second, err := f.svc.Queue(ctx, "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if second.Round.Index != 2 {
		t.Fatalf("round index = %d after finishing the first, want 2", second.Round.Index)
	}
	seen := make(map[string]bool, len(first.Questions))
	for _, q := range first.Questions {
		seen[q.ID] = true
	}
	for _, q := range second.Questions {
		if seen[q.ID] {
			t.Errorf("question %s came back in the second round", q.ID)
		}
	}
}

func TestQueue_lastRoundSaysSo(t *testing.T) {
	t.Parallel()
	// A pool smaller than one round is the last round by definition.
	f := newFixture(t, func(f *fixture) {
		f.sweeper.people = []*sweep.Person{scannedPerson("subj1", 0.05, 0.30)}
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != 2 {
		t.Fatalf("round = %d questions, want the 2 the library holds", len(res.Questions))
	}
	if !res.Round.Last {
		t.Error("round not marked last, but nothing is queued behind it")
	}
	if res.Round.Size != 2 {
		t.Errorf("round size = %d, want 2 — a short pool makes a short round", res.Round.Size)
	}
}

func TestQueue_limitOverridesTheRoundSize(t *testing.T) {
	t.Parallel()
	f := newFixture(t, roundLibrary)
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 4)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != 4 || res.Round.Size != 4 {
		t.Errorf("round = %d questions (size %d), want the requested 4",
			len(res.Questions), res.Round.Size)
	}
}

func TestQueue_albumMembershipIsReadOncePerRebuild(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		roundLibrary(f)
		f.albums = &fakeAlbums{byPhoto: map[string][]string{}}
	})
	ctx := context.Background()
	for range 3 {
		if _, err := f.svc.Queue(ctx, "user", SourceBoth, 0); err != nil {
			t.Fatalf("Queue: %v", err)
		}
	}
	// Three fetches, one rebuild (the cache is warm and the round is untouched),
	// so one membership lookup: it belongs to the pool, not to the round.
	if f.albums.calls != 1 {
		t.Errorf("album lookups = %d, want 1 per rebuild", f.albums.calls)
	}
}

func TestQueue_albumLookupFailureStillServesARound(t *testing.T) {
	t.Parallel()
	// The album rule is the mixer's cheapest preference. Losing it must not cost
	// the round, which has already paid for the vector searches.
	f := newFixture(t, func(f *fixture) {
		roundLibrary(f)
		f.albums = &fakeAlbums{err: errors.New("albums are down")}
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != DefaultRoundSize {
		t.Errorf("round = %d questions, want a full round of %d",
			len(res.Questions), DefaultRoundSize)
	}
}

// breatherPhoto builds a rated/favourited photo for the breather fake.
func breatherPhoto(uid, title string, year int) photos.Photo {
	taken := time.Date(year, 5, 1, 12, 0, 0, 0, time.UTC)
	return photos.Photo{UID: uid, Title: title, FileName: uid + ".jpg", TakenAt: &taken}
}

// withBreathers scripts three breather candidates, one per era, and the photos
// behind them.
func withBreathers(f *fixture) {
	f.breathers = &fakeBreathers{
		picks: []BreatherPick{
			{PhotoUID: "b2015", Rating: 5},
			{PhotoUID: "b1998", Favorite: true},
			{PhotoUID: "b1972", Rating: 4},
		},
		photos: map[string]photos.Photo{
			"b2015": breatherPhoto("b2015", "Na chalupě", 2015),
			"b1998": breatherPhoto("b1998", "Svatba", 1998),
			"b1972": breatherPhoto("b1972", "", 1972),
		},
	}
}

func TestQueue_breatherCardsAreTypedAndCarryTitleAndYear(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		roundLibrary(f)
		withBreathers(f)
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Breathers) != breathersPerRound {
		t.Fatalf("breathers = %d, want %d", len(res.Breathers), breathersPerRound)
	}
	card := res.Breathers[0]
	if card.Kind != BreatherKind {
		t.Errorf("breather kind = %q, want %q — a client must not have to guess",
			card.Kind, BreatherKind)
	}
	if card.Photo.UID != "b2015" || card.Title != "Na chalupě" || card.Year != 2015 {
		t.Errorf("breather = %+v, want the newest era's card with its title and year", card)
	}
	if card.Reason != BreatherReasonRated {
		t.Errorf("breather reason = %q, want %q", card.Reason, BreatherReasonRated)
	}
	// And it is not a question: nothing in the round shares its id space.
	for _, q := range res.Questions {
		if q.Photo.UID == card.Photo.UID {
			t.Errorf("the breather photo %s is also a question", card.Photo.UID)
		}
	}
}

func TestQueue_breathersRotateThroughTheEras(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		roundLibrary(f)
		withBreathers(f)
	})
	ctx := context.Background()
	seen := make([]string, 0, 3)
	for range 3 {
		res, err := f.svc.Queue(ctx, "user", SourceBoth, 1)
		if err != nil {
			t.Fatalf("Queue: %v", err)
		}
		if len(res.Breathers) != 1 {
			t.Fatalf("breathers = %d, want 1", len(res.Breathers))
		}
		seen = append(seen, res.Breathers[0].Photo.UID)
		// Answer the one-question round through so the next fetch mints a new one.
		if _, err := f.svc.Answer(ctx, "user", res.Questions[0].ID, AnswerSkip, audit.Meta{}); err != nil {
			t.Fatalf("Answer: %v", err)
		}
	}
	if seen[0] == seen[1] || seen[1] == seen[2] {
		t.Errorf("consecutive rounds showed %v, want the breather to move between eras", seen)
	}
}

func TestQueue_breatherFallsBackToTheFileNameAndDegradesQuietly(t *testing.T) {
	t.Parallel()
	// An untitled photo still gets a caption, and a failing pick costs the round
	// nothing.
	f := newFixture(t, func(f *fixture) {
		roundLibrary(f)
		withBreathers(f)
	})
	ctx := context.Background()
	// The third round's turn is the untitled 1972 card.
	var card Breather
	for range 3 {
		res, err := f.svc.Queue(ctx, "user", SourceBoth, 1)
		if err != nil {
			t.Fatalf("Queue: %v", err)
		}
		card = res.Breathers[0]
		if _, err := f.svc.Answer(ctx, "user", res.Questions[0].ID, AnswerSkip, audit.Meta{}); err != nil {
			t.Fatalf("Answer: %v", err)
		}
	}
	if card.Photo.UID != "b1972" || card.Title != "b1972.jpg" {
		t.Errorf("breather = %+v, want the untitled card captioned with its file name", card)
	}

	broken := newFixture(t, func(f *fixture) {
		roundLibrary(f)
		f.breathers = &fakeBreathers{err: errors.New("no breathers today")}
	})
	res, err := broken.svc.Queue(ctx, "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Breathers) != 0 {
		t.Errorf("breathers = %d after a failed pick, want none", len(res.Breathers))
	}
	if len(res.Questions) != DefaultRoundSize {
		t.Errorf("round = %d questions, want the breather failure not to cost the round",
			len(res.Questions))
	}
}

func TestQueue_noBreatherSourceMeansNoBreathers(t *testing.T) {
	t.Parallel()
	f := newFixture(t, roundLibrary)
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if res.Breathers != nil {
		t.Errorf("breathers = %v with no source wired, want none", res.Breathers)
	}
}

// withStats scripts one subject's headline numbers behind the answer reveal.
func withStats(f *fixture) {
	f.stats = &fakeSubjectStats{stats: map[string]people.SubjectStats{
		"subj1": {UID: "subj1", Name: "Anna", PhotoCount: 42, OldestYear: 1961, NewestYear: 2019},
	}}
}

// withFace scripts the face a yes answer assigns.
func withFace(f *fixture) {
	f.faces.faces[vectors.FaceKey{PhotoUID: "photo1", FaceIndex: 0}] = vectors.Face{
		PhotoUID: "photo1", FaceIndex: 0, BBox: [4]float64{0.1, 0.2, 0.3, 0.4},
	}
}

func TestAnswer_confirmedFaceRevealsThePerson(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		withFace(f)
		withStats(f)
	})
	id := faceQuestionID("photo1", 0, "subj1")
	res, err := f.svc.Answer(context.Background(), "user", id, AnswerYes, audit.Meta{})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if res.Reveal == nil {
		t.Fatalf("no reveal on a confirmed assignment: %+v", res)
	}
	want := Reveal{
		SubjectUID: "subj1", Name: "Anna", PhotoCount: 42, OldestYear: 1961, NewestYear: 2019,
	}
	if *res.Reveal != want {
		t.Errorf("reveal = %+v, want %+v", *res.Reveal, want)
	}
}

func TestAnswer_noRevealOnAnythingButAConfirmedFace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name   string
		id     string
		answer Answer
	}{
		{"a rejected face", faceQuestionID("photo1", 0, "subj1"), AnswerNo},
		{"a skipped face", faceQuestionID("photo1", 0, "subj1"), AnswerSkip},
		{"a confirmed label", labelQuestionID("photo1", "lab1"), AnswerYes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, func(f *fixture) {
				withFace(f)
				withStats(f)
				f.organize.labels = []organize.LabelCount{labelCount("lab1", 1)}
			})
			res, err := f.svc.Answer(ctx, "user", tt.id, tt.answer, audit.Meta{})
			if err != nil {
				t.Fatalf("Answer: %v", err)
			}
			if res.Reveal != nil {
				t.Errorf("reveal = %+v, want none for %s", *res.Reveal, tt.name)
			}
		})
	}
}

func TestAnswer_revealFailureDoesNotFailTheAnswer(t *testing.T) {
	t.Parallel()
	// The write has already happened by the time the reveal is read. Failing the
	// request would tell the player their answer was lost when it was not.
	f := newFixture(t, func(f *fixture) {
		withFace(f)
		f.stats = &fakeSubjectStats{err: errors.New("stats are down")}
	})
	id := faceQuestionID("photo1", 0, "subj1")
	res, err := f.svc.Answer(context.Background(), "user", id, AnswerYes, audit.Meta{})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if res.Result != resultAssigned {
		t.Errorf("result = %q, want assigned", res.Result)
	}
	if res.Reveal != nil {
		t.Errorf("reveal = %+v, want none when it could not be read", *res.Reveal)
	}
}

func TestAnswer_noStatsReaderMeansNoReveal(t *testing.T) {
	t.Parallel()
	f := newFixture(t, withFace)
	id := faceQuestionID("photo1", 0, "subj1")
	res, err := f.svc.Answer(context.Background(), "user", id, AnswerYes, audit.Meta{})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if res.Reveal != nil {
		t.Errorf("reveal = %+v with no stats reader wired, want none", *res.Reveal)
	}
}
