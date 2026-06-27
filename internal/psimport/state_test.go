package psimport

import (
	"testing"
	"time"
)

// ts is a small helper building a deterministic timestamp at the given unix
// second.
func ts(sec int64) time.Time {
	return time.Unix(sec, 0).UTC()
}

// TestRunState_watermark covers the no-progress, all-success and failure-capped
// cases of the resume-watermark computation.
func TestRunState_watermark(t *testing.T) {
	t.Parallel()

	t.Run("nothing seen yields nil", func(t *testing.T) {
		t.Parallel()
		st := &runState{}
		if got := st.watermark(); got != nil {
			t.Errorf("watermark = %v, want nil", got)
		}
	})

	t.Run("all success advances to max", func(t *testing.T) {
		t.Parallel()
		st := &runState{}
		st.recordSuccess(ts(100))
		st.recordSuccess(ts(300))
		st.recordSuccess(ts(200))
		got := st.watermark()
		if got == nil || !got.Equal(ts(300)) {
			t.Errorf("watermark = %v, want 300", got)
		}
	})

	t.Run("failure caps below earliest failure", func(t *testing.T) {
		t.Parallel()
		st := &runState{}
		st.recordSuccess(ts(500))
		st.recordFailure(ts(200))
		got := st.watermark()
		want := ts(200).Add(-time.Nanosecond)
		if got == nil || !got.Equal(want) {
			t.Errorf("watermark = %v, want %v", got, want)
		}
		if st.counts.Failed != 1 {
			t.Errorf("failed count = %d, want 1", st.counts.Failed)
		}
	})

	t.Run("no forward progress keeps prior cursor", func(t *testing.T) {
		t.Parallel()
		st := &runState{since: ts(1000)}
		st.recordSuccess(ts(400)) // older than since
		got := st.watermark()
		if got == nil || !got.Equal(ts(1000)) {
			t.Errorf("watermark = %v, want 1000 (unchanged)", got)
		}
	})
}

// TestRunState_recordFailure tracks the earliest failure timestamp.
func TestRunState_recordFailure(t *testing.T) {
	t.Parallel()
	st := &runState{}
	st.recordFailure(ts(300))
	st.recordFailure(ts(100))
	st.recordFailure(ts(200))
	if !st.minFailed.Equal(ts(100)) {
		t.Errorf("minFailed = %v, want 100", st.minFailed)
	}
	if st.counts.Failed != 3 {
		t.Errorf("failed = %d, want 3", st.counts.Failed)
	}
}
