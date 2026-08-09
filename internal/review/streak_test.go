package review

// currentStreak is the whole of the streak feature that can get the answer
// wrong, and every way it can is a calendar question: which day an instant falls
// on, whether an unfinished today breaks a run, where a day ends in a zone that
// is not UTC. So it is tested directly, with a fixed clock, and the SQL around
// it only has to hand it timestamps.

import (
	"testing"
	"time"
)

// prague is a real zone an hour or two ahead of UTC, which is what makes the
// day-boundary cases in this file mean anything: an answer at 23:30 UTC is
// already tomorrow there.
var prague = time.FixedZone("CET", 1*60*60)

// at builds an instant in UTC, which is how the audit log stores them.
func at(year int, month time.Month, day, hour int) time.Time {
	return time.Date(year, month, day, hour, 0, 0, 0, time.UTC)
}

func TestCurrentStreak(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		active []time.Time
		now    time.Time
		want   int
	}{
		{
			name: "never played",
			now:  now,
		},
		{
			name:   "today only",
			active: []time.Time{at(2026, 8, 9, 9)},
			now:    now,
			want:   1,
		},
		{
			name:   "three days up to today",
			active: []time.Time{at(2026, 8, 9, 9), at(2026, 8, 8, 20), at(2026, 8, 7, 6)},
			now:    now,
			want:   3,
		},
		{
			name: "several answers on one day count once",
			active: []time.Time{
				at(2026, 8, 9, 9), at(2026, 8, 9, 10), at(2026, 8, 9, 11),
				at(2026, 8, 8, 20),
			},
			now:  now,
			want: 2,
		},
		{
			name:   "a run that ended yesterday is still alive",
			active: []time.Time{at(2026, 8, 8, 9), at(2026, 8, 7, 9)},
			now:    now,
			want:   2,
		},
		{
			name:   "a run that ended the day before yesterday is over",
			active: []time.Time{at(2026, 8, 7, 9), at(2026, 8, 6, 9)},
			now:    now,
			want:   0,
		},
		{
			name: "only the run touching now counts",
			active: []time.Time{
				at(2026, 8, 9, 9), at(2026, 8, 8, 9),
				at(2026, 8, 4, 9), at(2026, 8, 3, 9), at(2026, 8, 2, 9),
			},
			now:  now,
			want: 2,
		},
		{
			name:   "unsorted input",
			active: []time.Time{at(2026, 8, 7, 6), at(2026, 8, 9, 9), at(2026, 8, 8, 20)},
			now:    now,
			want:   3,
		},
		{
			name:   "a run crossing a month boundary",
			active: []time.Time{at(2026, 8, 1, 9), at(2026, 7, 31, 9), at(2026, 7, 30, 9)},
			now:    time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
			want:   3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := currentStreak(tt.active, tt.now); got != tt.want {
				t.Errorf("currentStreak = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCurrentStreak_daysAreCountedInTheClocksZone(t *testing.T) {
	t.Parallel()
	// A late-evening session on the 6th (23:30 UTC) is already the 7th in Prague,
	// and the next answers come on the 8th. Counted in UTC the days are the 6th
	// and the 8th — a gap, so the streak is one day. Counted in Prague they are
	// the 7th and the 8th, which is a live two-day run. Which of the two the
	// player sees is decided by the clock the store was built with, and that is
	// the whole point of doing the day arithmetic here rather than in SQL.
	active := []time.Time{
		time.Date(2026, 8, 6, 23, 30, 0, 0, time.UTC),
		time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC),
	}

	utcNow := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	if got := currentStreak(active, utcNow); got != 1 {
		t.Errorf("streak in UTC = %d, want 1 — the 7th has no answers there", got)
	}
	pragueNow := utcNow.In(prague)
	if got := currentStreak(active, pragueNow); got != 2 {
		t.Errorf("streak in Prague = %d, want 2 — the late session lands on the 7th there", got)
	}
}

func TestDistinctDays_foldsInstantsIntoLocalDaysNewestFirst(t *testing.T) {
	t.Parallel()
	active := []time.Time{at(2026, 8, 7, 6), at(2026, 8, 9, 9), at(2026, 8, 9, 23), at(2026, 8, 8, 1)}
	days := distinctDays(active, time.UTC)
	want := []string{"2026-08-09", "2026-08-08", "2026-08-07"}
	if len(days) != len(want) {
		t.Fatalf("days = %v, want %v", days, want)
	}
	for i, day := range days {
		if got := day.Format("2006-01-02"); got != want[i] {
			t.Errorf("day %d = %s, want %s", i, got, want[i])
		}
		if day.Hour() != 0 || day.Minute() != 0 {
			t.Errorf("day %d = %v, want midnight", i, day)
		}
	}
}

func TestStartOfDay_isMidnightInTheInstantsOwnZone(t *testing.T) {
	t.Parallel()
	// time.Truncate would answer midnight UTC here, which is 01:00 in Prague and
	// therefore the wrong day for anything before it.
	instant := time.Date(2026, 8, 9, 0, 30, 0, 0, prague)
	got := startOfDay(instant)
	if got.Year() != 2026 || got.Month() != time.August || got.Day() != 9 {
		t.Errorf("startOfDay = %v, want 2026-08-09 in the instant's own zone", got)
	}
	if got.Hour() != 0 || !got.Equal(got.Truncate(time.Second)) {
		t.Errorf("startOfDay = %v, want midnight exactly", got)
	}
	if got.Location() != prague {
		t.Errorf("startOfDay location = %v, want the instant's own", got.Location())
	}
}
