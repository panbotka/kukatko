//go:build integration

package review_test

// "I don't know", driven end to end over a real database.
//
// These are the assertions the in-memory session could never carry: every one of
// them starts a *fresh* service between the skips and the queue, which is what a
// restart looks like to a player. A memory that only lived in the session would
// pass none of them.
//
// The last two are the ones that keep the feature honest. A mute is a pause, not
// a verdict — a photo the library gained afterwards is still asked about, and
// after the cooling-off period the game tries once more on a face the player has
// never been shown — and it is strictly personal: one player's "I don't know"
// must never quiet the game for anybody else.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/review"
	"github.com/panbotka/kukatko/internal/vectors"
)

// skipHarness is a review harness with one named subject and a bag of unnamed
// faces that all resemble them, i.e. the exact material the face questions are
// built from.
type skipHarness struct {
	*reviewHarness
	// subject is the person every candidate looks like.
	subject string
	// photos are the candidate photos, in the order they were created.
	photos []string
}

// exemplarVec is the subject's own face; candidateVec sits at a cosine distance
// of 0.4 from it, comfortably inside the uncertainty band rather than on either
// tier's edge, where halfvec rounding decides which side of a threshold a
// candidate lands on.
func exemplarVec() []float32  { return vec(map[int]float32{0: 1}) }
func candidateVec() []float32 { return bandFace() }

// newSkipHarness seeds one subject and count candidate faces that resemble them.
func newSkipHarness(t *testing.T, count int) *skipHarness {
	t.Helper()
	h := newReviewHarness(t)
	subject := h.namedSubject(t, "Anna", "anna-exemplar", exemplarVec())
	skips := &skipHarness{reviewHarness: h, subject: subject.UID}
	for i := range count {
		skips.photos = append(skips.photos, skips.candidate(t, fmt.Sprintf("cand-%d", i)))
	}
	return skips
}

// candidate adds one more unnamed face that resembles the subject.
func (h *skipHarness) candidate(t *testing.T, hash string) string {
	t.Helper()
	return h.face(t, hash, vectors.Face{FaceIndex: 0, Vector: candidateVec(), DetScore: 0.95})
}

// skipEverything answers "don't know" to every face question the queue serves,
// up to limit answers, and returns the photo uids it was asked about.
func (h *skipHarness) skipEverything(t *testing.T, svc *review.Service, user string, limit int) []string {
	t.Helper()
	ctx := context.Background()
	asked := make([]string, 0, limit)
	for len(asked) < limit {
		res, err := svc.Queue(ctx, user, review.SourcePeople, 100)
		if err != nil {
			t.Fatalf("Queue: %v", err)
		}
		if len(res.Questions) == 0 {
			break
		}
		for _, q := range res.Questions {
			if len(asked) >= limit {
				break
			}
			meta := audit.Meta{ActorUID: user}
			if _, err := svc.Answer(ctx, user, q.ID, review.AnswerSkip, meta); err != nil {
				t.Fatalf("Answer(skip): %v", err)
			}
			asked = append(asked, q.Photo.UID)
		}
	}
	return asked
}

// faceQuestions returns the face questions a fresh service serves the user, so
// every read starts from a cold session — the restart the memory exists for.
func (h *skipHarness) faceQuestions(t *testing.T, user string) []review.Question {
	t.Helper()
	res, err := h.service().Queue(context.Background(), user, review.SourcePeople, 100)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	return res.Questions
}

func TestSkip_threeAboutOnePersonMutesThem(t *testing.T) {
	h := newSkipHarness(t, 6)
	user := h.user(t, "usr_skip_mute")

	// Two skips are forgiven: an unclear photo is not an unknown face, and a
	// person silenced by a single bad crop would be a bug, not a feature.
	asked := h.skipEverything(t, h.service(), user, 2)
	if len(asked) != 2 {
		t.Fatalf("skipped %d questions, want 2", len(asked))
	}
	if got := h.faceQuestions(t, user); len(got) == 0 {
		t.Fatal("no questions after two skips, want the person still in the game — " +
			"two skips are forgiven")
	}

	// The third is the one that means "I do not know this person".
	h.skipEverything(t, h.service(), user, 1)
	if got := h.faceQuestions(t, user); len(got) != 0 {
		t.Errorf("%d questions after three skips, want the person muted: %v",
			len(got), questionPhotos(got))
	}
}

func TestSkip_muteSurvivesARestartAndIsPerUser(t *testing.T) {
	h := newSkipHarness(t, 6)
	muted := h.user(t, "usr_skip_muted")
	other := h.user(t, "usr_skip_other")

	h.skipEverything(t, h.service(), muted, 3)

	// A fresh service is a restart: the session, and with it every shelved
	// question, is gone. The memory is not.
	if got := h.faceQuestions(t, muted); len(got) != 0 {
		t.Errorf("%d questions after a restart, want the mute to have survived it: %v",
			len(got), questionPhotos(got))
	}
	// And the other player was never asked anything. A skip says "don't ask *me*
	// this"; making it quiet the game for everybody would turn one player's
	// uncertainty into an instance-wide fact.
	if got := h.faceQuestions(t, other); len(got) == 0 {
		t.Error("no questions for the other player, want another player's skips to " +
			"leave their queue untouched")
	}
}

func TestSkip_isNotARejection(t *testing.T) {
	h := newSkipHarness(t, 6)
	user := h.user(t, "usr_skip_notreject")
	skipped := h.skipEverything(t, h.service(), user, 3)

	// Nothing about a skip may reach the catalogue. The face rejections are what
	// the candidate search, the sweep and the negative-exemplar rule all read, so
	// a skip leaking into them would poison recognition for everybody.
	var rejections int
	if err := h.db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM face_rejections`).Scan(&rejections); err != nil {
		t.Fatalf("counting face rejections: %v", err)
	}
	if rejections != 0 {
		t.Errorf("face_rejections = %d after three skips, want none — a skip is not a rejection",
			rejections)
	}
	// Nor may it appear in the audit trail: the audit records what happened to
	// the library, and nothing here happened to the library.
	var entries int
	if err := h.db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log`).Scan(&entries); err != nil {
		t.Fatalf("counting audit entries: %v", err)
	}
	if entries != 0 {
		t.Errorf("audit_log = %d entries after three skips, want none", entries)
	}
	// The identities themselves are untouched: the faces are still unassigned.
	for _, photoUID := range skipped {
		faces, err := h.vectors.FacesByKeys(context.Background(),
			[]vectors.FaceKey{{PhotoUID: photoUID, FaceIndex: 0}})
		if err != nil {
			t.Fatalf("FacesByKeys: %v", err)
		}
		if len(faces) != 1 || faces[0].SubjectUID != nil {
			t.Errorf("face on %s carries a subject after a skip, want it left alone", photoUID)
		}
	}
}

func TestSkip_aPhotoAddedAfterTheMuteIsStillAsked(t *testing.T) {
	h := newSkipHarness(t, 3)
	user := h.user(t, "usr_skip_newphoto")
	h.skipEverything(t, h.service(), user, 3)
	if got := h.faceQuestions(t, user); len(got) != 0 {
		t.Fatalf("%d questions with everything skipped, want the person muted", len(got))
	}

	// A face the library gained after the mute is exactly what might be
	// recognisable — the mute is a pause on the questions already asked, not a
	// verdict on the person.
	fresh := h.candidate(t, "cand-after-mute")
	got := h.faceQuestions(t, user)
	if len(got) == 0 {
		t.Fatal("no questions after a new photo arrived, want the new face asked about")
	}
	for _, q := range got {
		if q.Photo.UID != fresh {
			t.Errorf("asked about %s, want only the newly imported %s", q.Photo.UID, fresh)
		}
	}
}

func TestSkip_afterTheCoolingOffPeriodOneUnseenPhotoIsAsked(t *testing.T) {
	h := newSkipHarness(t, 6)
	user := h.user(t, "usr_skip_cooldown")
	skipped := h.skipEverything(t, h.service(), user, 3)
	if got := h.faceQuestions(t, user); len(got) != 0 {
		t.Fatalf("%d questions during the mute, want silence", len(got))
	}

	// Past the cooling-off period the game tries once more — but only on faces
	// the player has never been shown. Re-asking the very photos they gave up on
	// would be the game not listening at all.
	later := time.Now().Add(review.DefaultSkipMuteCooldown + time.Hour)
	svc := h.serviceAt(func() time.Time { return later })
	res, err := svc.Queue(context.Background(), user, review.SourcePeople, 100)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) == 0 {
		t.Fatalf("no questions after the cooling-off period (reason %q), want the person "+
			"back in the game", res.Reason)
	}
	for _, q := range res.Questions {
		for _, seen := range skipped {
			if q.Photo.UID == seen {
				t.Errorf("asked about %s again, want only photos never asked about before",
					q.Photo.UID)
			}
		}
	}
}

func TestSkip_anotherSkipRemutesForLonger(t *testing.T) {
	h := newSkipHarness(t, 8)
	user := h.user(t, "usr_skip_remute")
	h.skipEverything(t, h.service(), user, 3)

	// One skip past the threshold, taken after the first pause expired.
	later := time.Now().Add(review.DefaultSkipMuteCooldown + time.Hour)
	clock := func() time.Time { return later }
	h.skipEverything(t, h.serviceAt(clock), user, 1)

	// The same wait again would leave the person muted; the point is that the
	// pause grew, so a player who keeps saying "I don't know" is asked ever less
	// often rather than every cooldown for ever.
	sameWaitAgain := later.Add(review.DefaultSkipMuteCooldown + time.Hour)
	res, err := h.serviceAt(func() time.Time { return sameWaitAgain }).
		Queue(context.Background(), user, review.SourcePeople, 100)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != 0 {
		t.Errorf("%d questions one cooldown after the fourth skip, want the pause to have "+
			"doubled: %v", len(res.Questions), questionPhotos(res.Questions))
	}
}

// questionPhotos renders the photo uids of a batch, for failure messages.
func questionPhotos(questions []review.Question) []string {
	uids := make([]string, 0, len(questions))
	for _, q := range questions {
		uids = append(uids, q.Photo.UID)
	}
	return uids
}
