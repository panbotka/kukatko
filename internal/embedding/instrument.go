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
)

// Observer receives latency and availability signals from an instrumented
// Client. It is satisfied by the metrics registry; tests use a fake. Methods
// must be safe for concurrent use.
type Observer interface {
	// ObserveEmbeddingCall records that an embedding operation took d and ended
	// with err (nil means success).
	ObserveEmbeddingCall(operation string, d time.Duration, err error)
	// SetEmbeddingUp records the sidecar's reachability after a call or probe:
	// true when the box responded, false when it was offline.
	SetEmbeddingUp(up bool)
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

// Healthy probes the wrapped client and mirrors the result onto the up gauge.
func (i *instrumentedClient) Healthy(ctx context.Context) bool {
	up := i.inner.Healthy(ctx)
	i.obs.SetEmbeddingUp(up)
	return up
}

// record reports a call's latency and outcome and updates the up gauge: a
// transport-level unavailability marks the box down; any other outcome (success
// or a well-formed error response) means the box answered, so it is up.
func (i *instrumentedClient) record(operation string, d time.Duration, err error) {
	i.obs.ObserveEmbeddingCall(operation, d, err)
	i.obs.SetEmbeddingUp(!IsUnavailable(err))
}
