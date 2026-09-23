package embedding

import (
	"context"
	"io"
	"time"
)

// Embedding operation names recorded by an Observer. They are stable label
// values, so callers can build dashboards against them.
const (
	// OpImage is the image-embedding operation.
	OpImage = "image"
	// OpText is the text-embedding operation.
	OpText = "text"
	// OpFace is the face-detection/embedding operation.
	OpFace = "face"
	// OpOCR is the text-recognition operation.
	OpOCR = "ocr"
)

// Operations returns every operation name an instrumented Client reports, so a
// metrics surface can pre-create its series before the first call.
func Operations() []string {
	return []string{OpImage, OpText, OpFace, OpOCR}
}

// Reachability targets an Observer is told about. The sidecar may be split over
// two hosts (see Config.TextBaseURL), and "is it up?" has a different answer for
// each, so the signal names the role of the host — never its URL, which is
// configuration and has no business in a label.
const (
	// TargetBox is the host that answers images, faces, OCR and the health
	// probe: embedding.url, normally the GPU box.
	TargetBox = "box"
	// TargetText is the host that answers /embed/text: embedding.text_url when
	// one is configured, otherwise the box itself.
	TargetText = "text"
)

// targetOf returns the reachability target an operation's call lands on.
func targetOf(operation string) string {
	if operation == OpText {
		return TargetText
	}
	return TargetBox
}

// Observer receives latency and availability signals from an instrumented
// Client. It is satisfied by the metrics registry; tests use a fake. Methods
// must be safe for concurrent use.
type Observer interface {
	// ObserveEmbeddingCall records that an embedding operation took d and ended
	// with err (nil means success).
	ObserveEmbeddingCall(operation string, d time.Duration, err error)
	// SetEmbeddingUp records the reachability of target (TargetBox or
	// TargetText) after a call or probe: true when the host responded, false
	// when it was offline.
	SetEmbeddingUp(target string, up bool)
}

// Instrument wraps c so every call reports its latency and outcome to obs and
// keeps the sidecar-up signal current (a transport-level ErrUnavailable marks
// the box down; any other result, error or not, marks it up because the box
// answered). A nil obs returns c unchanged so wiring metrics stays optional.
func Instrument(c Client, obs Observer) Client {
	if obs == nil {
		return c
	}
	return &instrumentedClient{inner: c, obs: obs}
}

// instrumentedClient decorates a Client with Observer callbacks.
type instrumentedClient struct {
	inner Client
	obs   Observer
}

// ImageEmbedding times the wrapped call and reports its outcome before
// returning the inner result unchanged.
func (i *instrumentedClient) ImageEmbedding(
	ctx context.Context, img io.Reader,
) (embedding []float32, model, pretrained string, err error) {
	start := time.Now()
	embedding, model, pretrained, err = i.inner.ImageEmbedding(ctx, img)
	i.record(OpImage, time.Since(start), err)
	return embedding, model, pretrained, err //nolint:wrapcheck // decorator returns inner error verbatim
}

// TextEmbedding times the wrapped call and reports its outcome before returning
// the inner result unchanged.
func (i *instrumentedClient) TextEmbedding(
	ctx context.Context, text string,
) (embedding []float32, model, pretrained string, err error) {
	start := time.Now()
	embedding, model, pretrained, err = i.inner.TextEmbedding(ctx, text)
	i.record(OpText, time.Since(start), err)
	return embedding, model, pretrained, err //nolint:wrapcheck // decorator returns inner error verbatim
}

// FaceEmbeddings times the wrapped call and reports its outcome before
// returning the inner result unchanged.
func (i *instrumentedClient) FaceEmbeddings(
	ctx context.Context, img io.Reader,
) (faces []Face, model string, err error) {
	start := time.Now()
	faces, model, err = i.inner.FaceEmbeddings(ctx, img)
	i.record(OpFace, time.Since(start), err)
	return faces, model, err //nolint:wrapcheck // decorator returns inner error verbatim
}

// ImageOCR times the wrapped call and reports its outcome before returning the
// inner result unchanged.
func (i *instrumentedClient) ImageOCR(
	ctx context.Context, img io.Reader, minConfidence float64,
) (OCRResult, error) {
	start := time.Now()
	result, err := i.inner.ImageOCR(ctx, img, minConfidence)
	i.record(OpOCR, time.Since(start), err)
	return result, err //nolint:wrapcheck // decorator returns inner error verbatim
}

// Healthy probes the wrapped client and mirrors the result onto the box's up
// signal: HTTPClient.Healthy always probes the box, never the text host.
func (i *instrumentedClient) Healthy(ctx context.Context) bool {
	up := i.inner.Healthy(ctx)
	i.obs.SetEmbeddingUp(TargetBox, up)
	return up
}

// record reports a call's latency and outcome and updates the up signal of the
// host the call went to: a transport-level unavailability marks it down; any
// other outcome (success or a well-formed error response) means the host
// answered, so it is up.
func (i *instrumentedClient) record(operation string, d time.Duration, err error) {
	i.obs.ObserveEmbeddingCall(operation, d, err)
	i.obs.SetEmbeddingUp(targetOf(operation), !IsUnavailable(err))
}

// Prober is anything with a cheap health probe — a Client, or a client built
// only to be probed. It is the shape the background reachability loops need.
type Prober interface {
	// Healthy reports whether the probed host answered a fresh probe.
	Healthy(ctx context.Context) bool
}

// InstrumentProbe wraps p so every probe reports its result to obs as target's
// reachability. It exists for the background loops (auto-wake, the
// semantic-search capability) that probe on their own clock with a client of
// their own: those probes are the ones that know whether a host is up while no
// work is running, so they, not the last embed call, must drive the signal. A
// nil obs returns p unchanged.
func InstrumentProbe(p Prober, obs Observer, target string) Prober {
	if obs == nil {
		return p
	}
	return &instrumentedProbe{inner: p, obs: obs, target: target}
}

// instrumentedProbe decorates a Prober with the Observer's up signal.
type instrumentedProbe struct {
	inner  Prober
	obs    Observer
	target string
}

// Healthy probes the wrapped Prober and records the result for the target.
func (p *instrumentedProbe) Healthy(ctx context.Context) bool {
	up := p.inner.Healthy(ctx)
	p.obs.SetEmbeddingUp(p.target, up)
	return up
}
