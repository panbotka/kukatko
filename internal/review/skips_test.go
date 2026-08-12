package review

import (
	"context"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// skipsAt builds a skip history of count photos, the newest recorded at last.
func skipsAt(last time.Time, photoUIDs ...string) SubjectSkips {
	set := make(map[string]struct{}, len(photoUIDs))
	for _, uid := range photoUIDs {
		set[uid] = struct{}{}
	}
	return SubjectSkips{Photos: set, LastAt: last}
}

// testPolicy is the shipped mute policy with a round cooldown.
func testPolicy() mutePolicy {
	return mutePolicy{threshold: DefaultSkipMuteThreshold, cooldown: 24 * time.Hour}
}

func TestMutePolicy_twoSkipsAreForgiven(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	for _, photoUIDs := range [][]string{nil, {"p1"}, {"p1", "p2"}} {
		if _, muted := testPolicy().muteFor(skipsAt(now, photoUIDs...)); muted {
			t.Errorf("%d skips muted the person, want two forgiven — an unclear photo is "+
				"not an unknown face", len(photoUIDs))
		}
	}
}

func TestMutePolicy_thirdSkipMutesForTheCooldown(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	policy := testPolicy()
	silence, muted := policy.muteFor(skipsAt(now, "p1", "p2", "p3"))
	if !muted {
		t.Fatal("three skips did not mute the person")
	}
	if !silence.since.Equal(now) {
		t.Errorf("mute began at %v, want the newest skip at %v", silence.since, now)
	}
	if want := now.Add(policy.cooldown); !silence.until.Equal(want) {
		t.Errorf("mute lasts until %v, want %v", silence.until, want)
	}
}

func TestMutePolicy_everyFurtherSkipDoublesTheWait(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	policy := testPolicy()
	photoUIDs := make([]string, 0, 7)
	photoUIDs = append(photoUIDs, "p1", "p2", "p3")
	want := policy.cooldown
	for range 4 {
		silence, muted := policy.muteFor(skipsAt(now, photoUIDs...))
		if !muted {
			t.Fatalf("%d skips did not mute the person", len(photoUIDs))
		}
		if got := silence.until.Sub(now); got != want {
			t.Errorf("%d skips mute for %v, want %v — the pause has to grow, or a person "+
				"the player will never place is asked about every cooldown for ever",
				len(photoUIDs), got, want)
		}
		photoUIDs = append(photoUIDs, string(rune('a'+len(photoUIDs))))
		want *= 2
	}
}

func TestMutePolicy_theGrowingWaitIsCapped(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	policy := testPolicy()
	// Sixty skips would be a wait measured in millennia if the doubling ran free,
	// which is a rejection in all but name — and a skip must never become one.
	photoUIDs := make([]string, 0, 60)
	for i := range 60 {
		photoUIDs = append(photoUIDs, string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	silence, muted := policy.muteFor(skipsAt(now, photoUIDs...))
	if !muted {
		t.Fatal("sixty skips did not mute the person")
	}
	if got := silence.until.Sub(now); got != maxSkipMuteCooldown {
		t.Errorf("mute lasts %v, want it capped at %v", got, maxSkipMuteCooldown)
	}
}

// skipQuestion builds the face question the memory is asked about.
func skipQuestion(kind Kind, subjectUID, photoUID string, created time.Time) Question {
	subject := people.Subject{UID: subjectUID, Name: subjectUID}
	return Question{
		ID:      faceQuestionID(photoUID, 0, subjectUID),
		Kind:    kind,
		Subject: &subject,
		Photo:   photos.Photo{UID: photoUID, CreatedAt: created},
	}
}

func TestSkipMemory_silencesTheSkippedPhotoForGood(t *testing.T) {
	t.Parallel()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	memory := SkipMemory{"anna": skipsAt(old, "p1")}
	// One skip is well below the mute threshold, yet the photo it was about is
	// still not asked again: that is the "a photo the player has not been asked
	// about before" rule the post-mute retry needs.
	if !memory.silences(skipQuestion(KindFace, "anna", "p1", old), testPolicy(), old.Add(time.Hour)) {
		t.Error("the skipped photo came back, want it silent for this player")
	}
	if memory.silences(skipQuestion(KindFace, "anna", "p2", old), testPolicy(), old.Add(time.Hour)) {
		t.Error("another photo of the same person was silenced by a single skip")
	}
}

func TestSkipMemory_muteCoversTheLibraryAsItWas(t *testing.T) {
	t.Parallel()
	muted := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	now := muted.Add(time.Hour)
	memory := SkipMemory{"anna": skipsAt(muted, "p1", "p2", "p3")}
	policy := testPolicy()

	older := skipQuestion(KindFace, "anna", "p4", muted.Add(-24*time.Hour))
	if !memory.silences(older, policy, now) {
		t.Error("a photo the library already had is asked about during the mute")
	}
	fresh := skipQuestion(KindFace, "anna", "p5", muted.Add(time.Minute))
	if memory.silences(fresh, policy, now) {
		t.Error("a photo imported after the mute is suppressed, want it asked about — " +
			"a new face of that person is exactly what might be recognisable")
	}
	after := now.Add(policy.cooldown)
	if memory.silences(older, policy, after) {
		t.Error("the mute outlived its cooling-off period, want a pause rather than a verdict")
	}
}

func TestSkipMemory_silencesOnlyTheKindsThatNameAPerson(t *testing.T) {
	t.Parallel()
	muted := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	memory := SkipMemory{"anna": skipsAt(muted, "p1", "p2", "p3")}
	// "Is this really Anna?" is as much a question about Anna as "is this Anna?",
	// so the mute covers it.
	outlier := skipQuestion(KindOutlier, "anna", "p9", muted.Add(-time.Hour))
	if !memory.silences(outlier, testPolicy(), muted.Add(time.Hour)) {
		t.Error("an outlier question about a muted person was asked")
	}
	// A label question is not about a person at all, and could not be silenced by
	// one even in principle.
	label := Question{ID: "label:p1:lab", Kind: KindLabel, Photo: photos.Photo{UID: "p1"}}
	if memory.silences(label, testPolicy(), muted.Add(time.Hour)) {
		t.Error("a label question was silenced by a skip about a person")
	}
}

func TestAnswer_skipRemembersOnlyTheKindsThatNameAPerson(t *testing.T) {
	t.Parallel()
	skips := &fakeSkips{memory: map[string]SkipMemory{}}
	f := newFixture(t, func(f *fixture) { f.skips = skips })
	ctx := context.Background()
	ids := []string{
		faceQuestionID("ph1", 0, "anna"),
		outlierQuestionID("ph2", 1, "bara"),
		labelQuestionID("ph3", "lab"),
		placeQuestionID("ph4"),
		duplicateQuestionID("ph5", "ph6"),
	}
	for _, id := range ids {
		if _, err := f.svc.Answer(ctx, "user", id, AnswerSkip, audit.Meta{}); err != nil {
			t.Fatalf("Answer(skip, %s): %v", id, err)
		}
	}
	want := [][3]string{{"user", "anna", "ph1"}, {"user", "bara", "ph2"}}
	if len(skips.recorded) != len(want) {
		t.Fatalf("recorded %v, want only the two questions about a person (%v)",
			skips.recorded, want)
	}
	for i, got := range skips.recorded {
		if got != want[i] {
			t.Errorf("recorded[%d] = %v, want %v", i, got, want[i])
		}
	}
}

func TestAnswer_skipStillCountsWhenTheMemoryIsUnavailable(t *testing.T) {
	t.Parallel()
	// The memory is a convenience, not a write path: a game answered at one
	// keypress per second must not fail because a row could not be written.
	f := newFixture(t, func(f *fixture) {
		f.skips = &fakeSkips{err: context.DeadlineExceeded}
	})
	res, err := f.svc.Answer(context.Background(), "user",
		faceQuestionID("ph1", 0, "anna"), AnswerSkip, audit.Meta{})
	if err != nil {
		t.Fatalf("Answer(skip): %v", err)
	}
	if res.Result != resultSkipped {
		t.Errorf("result = %q, want %q", res.Result, resultSkipped)
	}
}
