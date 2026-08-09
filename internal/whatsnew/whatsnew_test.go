package whatsnew

import (
	"testing"
	"time"
)

// TestCountsEmpty covers the rule that decides whether a panel appears at all:
// only an all-zero digest is empty, and a single change of any kind is news.
func TestCountsEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   counts
		want bool
	}{
		{name: "nothing happened", in: counts{}, want: true},
		{name: "only photos", in: counts{photos: 3}, want: false},
		{name: "only comments", in: counts{comments: 1}, want: false},
		{name: "only albums", in: counts{albums: 1}, want: false},
		{name: "only people", in: counts{people: 1}, want: false},
		{name: "everything", in: counts{photos: 2, comments: 2, albums: 2, people: 2}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.in.empty(); got != tt.want {
				t.Errorf("counts%+v.empty() = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestNewSummary_empty: an all-zero digest assembles to the zero Summary, so the
// client sees has_news false and renders nothing — even if the lists were
// somehow non-empty.
func TestNewSummary_empty(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	got := newSummary(since, counts{}, []Album{{UID: "a", Title: "x"}}, nil)
	if got.HasNews {
		t.Errorf("newSummary(empty counts).HasNews = true, want false")
	}
	if !got.Since.IsZero() {
		t.Errorf("newSummary(empty counts).Since = %v, want zero", got.Since)
	}
	if got.Albums != nil {
		t.Errorf("newSummary(empty counts).Albums = %v, want nil", got.Albums)
	}
}

// TestNewSummary_counts: the totals come from the counts, not from the length of
// the (capped) link lists, so a digest can name six albums while reporting nine.
func TestNewSummary_counts(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	albums := []Album{{UID: "al1", Title: "Léto"}}
	people := []Person{{UID: "su1", Name: "Anna"}}
	got := newSummary(since, counts{photos: 12, comments: 4, albums: 9, people: 3}, albums, people)

	if !got.HasNews {
		t.Fatalf("HasNews = false, want true")
	}
	if !got.Since.Equal(since) {
		t.Errorf("Since = %v, want %v", got.Since, since)
	}
	if got.Photos != 12 || got.Comments != 4 {
		t.Errorf("Photos/Comments = %d/%d, want 12/4", got.Photos, got.Comments)
	}
	if got.AlbumCount != 9 || len(got.Albums) != 1 {
		t.Errorf("AlbumCount/len(Albums) = %d/%d, want 9/1", got.AlbumCount, len(got.Albums))
	}
	if got.PersonCount != 3 || len(got.People) != 1 {
		t.Errorf("PersonCount/len(People) = %d/%d, want 3/1", got.PersonCount, len(got.People))
	}
}

// TestVisitGap pins the requirement that a new visit begins only after at least
// six hours of inactivity; shortening it would silently break the panel's
// "a reload must not reset the reference point" contract.
func TestVisitGap(t *testing.T) {
	t.Parallel()

	if VisitGap < 6*time.Hour {
		t.Errorf("VisitGap = %v, want at least 6h", VisitGap)
	}
}

// TestWithGap: WithGap yields an independent Store carrying the test gap and
// leaves the original untouched.
func TestWithGap(t *testing.T) {
	t.Parallel()

	base := &Store{gap: VisitGap}
	short := base.WithGap(time.Minute)
	if short.gap != time.Minute {
		t.Errorf("WithGap(1m).gap = %v, want 1m", short.gap)
	}
	if base.gap != VisitGap {
		t.Errorf("base gap mutated to %v, want %v", base.gap, VisitGap)
	}
}
