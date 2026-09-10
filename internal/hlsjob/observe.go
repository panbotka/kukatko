package hlsjob

import "time"

// Outcome label values one rendition's encode is reported under. They are stable
// label values, so a dashboard or an alert may be written against them.
const (
	// OutcomeSuccess marks a rendition that was encoded, published and recorded.
	OutcomeSuccess = "success"
	// OutcomeError marks a rendition whose encode, upload or write failed; the
	// run left nothing behind, so the queue's retry will do it again.
	OutcomeError = "error"
)

// Observer receives what one encode cost, so the most expensive job in the queue
// can be measured without this package knowing anything about Prometheus. It is
// satisfied by *metrics.Registry; tests use a fake. Implementations must be safe
// for concurrent use, and must not block: they are called on the encode path.
//
// The grain is deliberately the rendition and not the job. A job encodes one
// video into every configured quality, and those qualities cost wildly different
// amounts of time and bytes, so a per-job number would average away the very
// thing an operator is trying to see.
type Observer interface {
	// ObserveRenditionEncode records that one rendition finished: which rendition
	// it was, whether it succeeded (one of OutcomeSuccess, OutcomeError), how long
	// it took wall-clock, and how many bytes of segments it published. Nothing
	// identifying the video is passed — /metrics is unauthenticated, so a photo
	// uid or a file name must never reach a label.
	ObserveRenditionEncode(rendition, outcome string, d time.Duration, written int64)
	// ObserveEncodedSource records that a clip of length d has been through the
	// encoder, once per video rather than once per rendition, so that dividing
	// the summed encode duration by it gives what a minute of footage costs to
	// put through the whole pipeline.
	ObserveEncodedSource(d time.Duration)
}

// nopObserver is the default Observer when none is configured; it does nothing,
// which is what makes the encode behave identically on an instance with metrics
// switched off.
type nopObserver struct{}

// ObserveRenditionEncode does nothing.
func (nopObserver) ObserveRenditionEncode(string, string, time.Duration, int64) {}

// ObserveEncodedSource does nothing.
func (nopObserver) ObserveEncodedSource(time.Duration) {}

// outcomeFor classifies one rendition's result into an Observer outcome label.
func outcomeFor(err error) string {
	if err != nil {
		return OutcomeError
	}
	return OutcomeSuccess
}

// clipLength returns the source clip's length as the catalogue recorded it, or
// zero when it does not know one — an unprobed container, or a video ingested
// before durations were stored. Zero is not reported: footage of no length would
// pull the per-minute cost towards infinity, and a gap in the counter is the
// honest answer to a length nobody measured.
func clipLength(durationMs *int) time.Duration {
	if durationMs == nil || *durationMs <= 0 {
		return 0
	}
	return time.Duration(*durationMs) * time.Millisecond
}
