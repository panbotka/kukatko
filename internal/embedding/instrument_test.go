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
	ocrErr    error
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

// ImageOCR returns a fixed reading and the configured error.
func (c *fakeClient) ImageOCR(context.Context, io.Reader, float64) (OCRResult, error) {
	return OCRResult{Text: "t", Model: "m"}, c.ocrErr
}

// Healthy returns the configured health.
func (c *fakeClient) Healthy(context.Context) bool { return c.healthy }

// recordingObserver captures the calls Instrument forwards to it.
type recordingObserver struct {
	op     string
	err    error
	calls  int
	up     bool
	upSet  bool
	target string
}

// ObserveEmbeddingCall records the latest call's operation and error.
func (o *recordingObserver) ObserveEmbeddingCall(operation string, _ time.Duration, err error) {
	o.op = operation
	o.err = err
	o.calls++
}

// SetEmbeddingUp records the latest up signal and the target it was for.
func (o *recordingObserver) SetEmbeddingUp(target string, up bool) {
	o.target = target
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
	if !obs.up || obs.target != TargetBox {
		t.Errorf("up=%v target=%q, want the box marked up after a successful call", obs.up, obs.target)
	}
}

// TestInstrument_recordsOCRCall verifies text recognition reports under its own
// operation label and passes the inner result through untouched.
func TestInstrument_recordsOCRCall(t *testing.T) {
	t.Parallel()

	obs := &recordingObserver{}
	c := Instrument(&fakeClient{}, obs)

	result, err := c.ImageOCR(context.Background(), strings.NewReader("x"), 0.5)
	if err != nil {
		t.Fatalf("ImageOCR: %v", err)
	}
	if result.Text != "t" || result.Model != "m" {
		t.Errorf("result = %+v, want the inner result unchanged", result)
	}
	if obs.op != OpOCR || obs.err != nil || obs.calls != 1 {
		t.Errorf("observed op=%q err=%v calls=%d, want ocr/nil/1", obs.op, obs.err, obs.calls)
	}
	if !obs.up {
		t.Error("expected sidecar marked up after a successful call")
	}
}

// TestInstrument_ocrUnavailableMarksDown verifies an offline box seen through the
// OCR call moves the same up gauge every other operation does.
func TestInstrument_ocrUnavailableMarksDown(t *testing.T) {
	t.Parallel()

	obs := &recordingObserver{}
	c := Instrument(&fakeClient{ocrErr: ErrUnavailable}, obs)
	_, _ = c.ImageOCR(context.Background(), strings.NewReader("x"), 0.5)
	if !obs.upSet || obs.up || obs.target != TargetBox {
		t.Errorf("up = %v (set %v, target %q), want the box marked down", obs.up, obs.upSet, obs.target)
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
			if obs.target != TargetText {
				t.Errorf("target = %q, want %q: a text call lands on the text host", obs.target, TargetText)
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
	if !obs.upSet || !obs.up || obs.target != TargetBox {
		t.Errorf("up not set true for the box by Healthy: set=%v up=%v target=%q", obs.upSet, obs.up, obs.target)
	}
}

// TestInstrumentProbe_recordsTarget verifies a standalone probe reports its
// result under the target it was built for, whichever way the probe goes.
func TestInstrumentProbe_recordsTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		target  string
		healthy bool
	}{
		{name: "text host up", target: TargetText, healthy: true},
		{name: "text host down", target: TargetText, healthy: false},
		{name: "box down", target: TargetBox, healthy: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			obs := &recordingObserver{}
			p := InstrumentProbe(&fakeClient{healthy: tt.healthy}, obs, tt.target)
			if got := p.Healthy(context.Background()); got != tt.healthy {
				t.Errorf("Healthy = %v, want the inner result %v", got, tt.healthy)
			}
			if !obs.upSet || obs.up != tt.healthy || obs.target != tt.target {
				t.Errorf("observed set=%v up=%v target=%q, want up=%v for %q",
					obs.upSet, obs.up, obs.target, tt.healthy, tt.target)
			}
		})
	}
}

// TestInstrumentProbe_nilObserverReturnsInner verifies a nil observer leaves the
// probe unwrapped.
func TestInstrumentProbe_nilObserverReturnsInner(t *testing.T) {
	t.Parallel()

	inner := &fakeClient{}
	if got := InstrumentProbe(inner, nil, TargetBox); got != inner {
		t.Errorf("InstrumentProbe(inner, nil) = %v, want the inner prober unchanged", got)
	}
}

// TestOperations_coversEveryInstrumentedCall verifies the operation list names
// each call the decorator reports, so a pre-created series never misses one.
func TestOperations_coversEveryInstrumentedCall(t *testing.T) {
	t.Parallel()

	obs := &recordingObserver{}
	c := Instrument(&fakeClient{}, obs)
	ctx := context.Background()
	seen := map[string]bool{}
	_, _, _, _ = c.ImageEmbedding(ctx, strings.NewReader("x"))
	seen[obs.op] = true
	_, _, _, _ = c.TextEmbedding(ctx, "q")
	seen[obs.op] = true
	_, _, _ = c.FaceEmbeddings(ctx, strings.NewReader("x"))
	seen[obs.op] = true
	_, _ = c.ImageOCR(ctx, strings.NewReader("x"), 0.5)
	seen[obs.op] = true

	ops := Operations()
	if len(ops) != len(seen) {
		t.Errorf("Operations() = %v, want exactly the reported operations %v", ops, seen)
	}
	for _, op := range ops {
		if !seen[op] {
			t.Errorf("Operations() lists %q, which no instrumented call reports", op)
		}
	}
}
