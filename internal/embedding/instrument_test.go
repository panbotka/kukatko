package embedding

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeClient is a stub Client whose method results are configured per test.
type fakeClient struct {
	imageErr  error
	textErr   error
	faceErr   error
	healthy   bool
	lastImage bool
}

// ImageEmbedding returns a fixed vector and the configured error.
func (c *fakeClient) ImageEmbedding(context.Context, io.Reader) ([]float32, string, string, error) {
	c.lastImage = true
	return []float32{1}, "m", "p", c.imageErr
}

// TextEmbedding returns a fixed vector and the configured error.
func (c *fakeClient) TextEmbedding(context.Context, string) ([]float32, string, string, error) {
	return []float32{1}, "m", "p", c.textErr
}

// FaceEmbeddings returns no faces and the configured error.
func (c *fakeClient) FaceEmbeddings(context.Context, io.Reader) ([]Face, string, error) {
	return nil, "m", c.faceErr
}

// Healthy returns the configured health.
func (c *fakeClient) Healthy(context.Context) bool { return c.healthy }

// recordingObserver captures the calls Instrument forwards to it.
type recordingObserver struct {
	op    string
	err   error
	calls int
	up    bool
	upSet bool
}

// ObserveEmbeddingCall records the latest call's operation and error.
func (o *recordingObserver) ObserveEmbeddingCall(operation string, _ time.Duration, err error) {
	o.op = operation
	o.err = err
	o.calls++
}

// SetEmbeddingUp records the latest up signal.
func (o *recordingObserver) SetEmbeddingUp(up bool) {
	o.up = up
	o.upSet = true
}

// TestInstrument_nilObserverReturnsInner verifies a nil observer leaves the
// client unwrapped, so wiring metrics stays optional.
func TestInstrument_nilObserverReturnsInner(t *testing.T) {
	t.Parallel()

	inner := &fakeClient{}
	if got := Instrument(inner, nil); got != inner {
		t.Errorf("Instrument(inner, nil) = %v, want the inner client unchanged", got)
	}
}

// TestInstrument_recordsImageCall verifies a successful image call reports its
// operation and marks the sidecar up.
func TestInstrument_recordsImageCall(t *testing.T) {
	t.Parallel()

	obs := &recordingObserver{}
	c := Instrument(&fakeClient{}, obs)

	if _, _, _, err := c.ImageEmbedding(context.Background(), strings.NewReader("x")); err != nil {
		t.Fatalf("ImageEmbedding: %v", err)
	}
	if obs.op != OpImage || obs.err != nil || obs.calls != 1 {
		t.Errorf("observed op=%q err=%v calls=%d, want image/nil/1", obs.op, obs.err, obs.calls)
	}
	if !obs.up {
		t.Error("expected sidecar marked up after a successful call")
	}
}

// TestInstrument_unavailableMarksDown verifies a transport-level unavailability
// marks the sidecar down while a well-formed error response leaves it up.
func TestInstrument_unavailableMarksDown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		wantUp bool
	}{
		{name: "unavailable marks down", err: ErrUnavailable, wantUp: false},
		{name: "other error stays up", err: errors.New("bad response"), wantUp: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			obs := &recordingObserver{}
			c := Instrument(&fakeClient{textErr: tt.err}, obs)
			_, _, _, _ = c.TextEmbedding(context.Background(), "q")
			if !obs.upSet {
				t.Fatal("SetEmbeddingUp was not called")
			}
			if obs.up != tt.wantUp {
				t.Errorf("up = %v, want %v", obs.up, tt.wantUp)
			}
		})
	}
}

// TestInstrument_healthyMirrorsProbe verifies Healthy mirrors the probe result
// onto the up gauge.
func TestInstrument_healthyMirrorsProbe(t *testing.T) {
	t.Parallel()

	obs := &recordingObserver{}
	c := Instrument(&fakeClient{healthy: true}, obs)
	if !c.Healthy(context.Background()) {
		t.Error("Healthy = false, want true")
	}
	if !obs.upSet || !obs.up {
		t.Errorf("up not set true by Healthy: set=%v up=%v", obs.upSet, obs.up)
	}
}
