// Package embedding is Kukátko's HTTP client to the external embeddings service
// (the inference sidecar that runs on the GPU box). Image and text embeddings
// share a 1152-dimensional SigLIP 2 space and face detection returns
// 512-dimensional ArcFace embeddings with pixel bounding boxes.
//
// The box is frequently offline, so the client is built to fail gracefully:
// transport failures and gateway-style HTTP statuses are reported as the
// retryable ErrUnavailable sentinel (distinct from ErrBadResponse for malformed
// answers and ErrDimMismatch for wrong vector sizes). Callers — primarily the
// job queue — use Healthy to skip work while the box sleeps and IsUnavailable to
// decide whether to requeue without burning a retry attempt.
//
// Everything sits behind the Client interface so tests can substitute a fake
// without any real network or box, and uploads stream through io.Pipe so a whole
// image is never buffered in memory.
//
// One call can be sent somewhere else: /embed/text serves interactive search and
// may be pointed at a second, always-on instance of the same service via
// Config.TextBaseURL, while image, face, OCR and health traffic stays on the box
// (see Config.TextBaseURL and Healthy for why the split stops there).
package embedding

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

// Default dimensions and endpoints for the shared SigLIP/ArcFace sidecar contract.
const (
	// DefaultImageDim is the dimensionality of SigLIP 2 image and text embeddings.
	// It moved from CLIP ViT-L-14's 768 together with the stored column width (see
	// migration 0057) and vectors.ImageDim; the three only make sense in step.
	DefaultImageDim = 1152
	// DefaultFaceDim is the dimensionality of ArcFace face embeddings. Faces come
	// from a different model and did not change with the image tower.
	DefaultFaceDim = 512

	// DefaultRequestTimeout bounds a single image/face embedding request; GPU
	// inference can be slow, especially on a cold box, so this is generous. It
	// only ever delays queue work, never a user-facing request.
	DefaultRequestTimeout = 60 * time.Second
	// DefaultTextTimeout bounds a text embedding, which serves an interactive
	// search request and must therefore fail fast: a semantic search that cannot
	// be answered in a few seconds degrades to full-text, and waiting longer only
	// leaves the reader staring at a spinner for results that were ready all along.
	DefaultTextTimeout = 5 * time.Second
	// DefaultDialTimeout bounds establishing the TCP connection to the sidecar.
	// It is deliberately far below the request timeouts because an offline box —
	// the normal state here — shows up as a dial that never completes; Go's
	// default dialer would wait 30 s before giving up.
	DefaultDialTimeout = 3 * time.Second
	// DefaultHealthTimeout bounds the cheap availability probe so an offline box
	// is detected quickly instead of stalling the worker.
	DefaultHealthTimeout = 5 * time.Second
	// DefaultHealthPath is the route probed by Healthy.
	DefaultHealthPath = "/health"

	endpointImage = "/embed/image"
	endpointText  = "/embed/text"
	endpointFace  = "/embed/face"
	endpointOCR   = "/ocr/image"
)

var (
	// ErrUnavailable indicates the sidecar could not be reached (box offline,
	// connection refused, timeout, DNS failure) or replied with a gateway-style
	// status (502/503/504). It is retryable: callers should requeue and wait for
	// the box rather than counting it as a failed attempt.
	ErrUnavailable = errors.New("embedding: service unavailable")
	// ErrBadResponse indicates the sidecar was reachable but returned an
	// unexpected status or an unparseable body. This is not transient.
	ErrBadResponse = errors.New("embedding: bad response")
	// ErrDimMismatch indicates a returned vector did not have the expected
	// dimensionality. This signals a model/config mismatch and is not transient.
	ErrDimMismatch = errors.New("embedding: dimension mismatch")
	// ErrInvalidURL indicates the configured base URL is not a usable HTTP(S) URL.
	ErrInvalidURL = errors.New("embedding: invalid base URL")
)

// Face is a single detected face with its embedding and detection metadata. The
// bounding box is expressed in source pixels as [x1, y1, x2, y2]; conversion to
// normalized coordinates happens at the storage layer using the image's real
// dimensions and EXIF orientation.
type Face struct {
	Index     int
	Dim       int
	Embedding []float32
	BBox      [4]float64
	DetScore  float64
}

// Client is the embeddings sidecar contract. It is an interface so callers can
// substitute a fake in tests without any real network or box.
type Client interface {
	// ImageEmbedding computes the image embedding of an image streamed from img.
	// It returns the vector together with the model and pretrained tags reported
	// by the service.
	ImageEmbedding(ctx context.Context, img io.Reader) (embedding []float32, model, pretrained string, err error)
	// TextEmbedding computes the embedding of a text query in the same shared
	// space as image embeddings.
	TextEmbedding(ctx context.Context, text string) (embedding []float32, model, pretrained string, err error)
	// FaceEmbeddings detects faces in the image streamed from img and returns one
	// Face per detection along with the model tag.
	FaceEmbeddings(ctx context.Context, img io.Reader) (faces []Face, model string, err error)
	// ImageOCR reads the text printed in the image streamed from img. A
	// non-positive minConfidence leaves the service's own default in place.
	ImageOCR(ctx context.Context, img io.Reader, minConfidence float64) (OCRResult, error)
	// Healthy reports whether the sidecar is currently reachable. It is a cheap
	// probe used to decide whether to attempt embedding jobs at all.
	Healthy(ctx context.Context) bool
}

// Config configures an HTTPClient. Only BaseURL is required; the remaining
// fields fall back to the package defaults when left zero.
type Config struct {
	// BaseURL is the root URL of the sidecar, e.g. "http://box:8000".
	BaseURL string
	// TextBaseURL optionally routes /embed/text to a second instance of the
	// service, leaving images, faces, OCR and the health probe on BaseURL. It
	// exists because those two groups have opposite needs: image and face work is
	// queued and can wait for the GPU box to be woken, while embedding a search
	// query is the one call a reader waits on, so it wants a host that is always
	// up (a CPU text-only instance next to the app). An empty value means "same
	// host as everything else" and is the state every deployment starts in.
	TextBaseURL string
	// ImageDim is the expected image/text embedding size (default DefaultImageDim).
	ImageDim int
	// FaceDim is the expected face embedding size (default DefaultFaceDim).
	FaceDim int
	// RequestTimeout bounds a single image/face embedding request (default
	// DefaultRequestTimeout).
	RequestTimeout time.Duration
	// TextTimeout bounds a single text embedding request — the interactive search
	// path (default DefaultTextTimeout).
	TextTimeout time.Duration
	// DialTimeout bounds connecting to the sidecar (default DefaultDialTimeout).
	// Ignored when HTTPClient is supplied, whose transport the caller owns.
	DialTimeout time.Duration
	// HealthTimeout bounds the Healthy probe (default DefaultHealthTimeout).
	HealthTimeout time.Duration
	// HealthPath is the route probed by Healthy (default DefaultHealthPath).
	HealthPath string
	// HTTPClient lets callers inject a custom client (default an http.Client with
	// a dial-timeout-bounded transport; the overall deadlines are enforced per
	// request via context).
	HTTPClient *http.Client
}

// HTTPClient is the production Client backed by the sidecar's HTTP API.
type HTTPClient struct {
	baseURL        *url.URL
	textURL        *url.URL
	imageDim       int
	faceDim        int
	requestTimeout time.Duration
	textTimeout    time.Duration
	healthTimeout  time.Duration
	healthPath     string
	client         *http.Client
}

// compile-time assertion that HTTPClient satisfies Client.
var _ Client = (*HTTPClient)(nil)

// New builds an HTTPClient from cfg. It returns ErrInvalidURL if BaseURL — or a
// non-empty TextBaseURL — is not a valid HTTP(S) URL with a host.
func New(cfg Config) (*HTTPClient, error) {
	parsed, err := parseServiceURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	textURL := parsed
	if cfg.TextBaseURL != "" {
		if textURL, err = parseServiceURL(cfg.TextBaseURL); err != nil {
			return nil, fmt.Errorf("text base url: %w", err)
		}
	}
	return &HTTPClient{
		baseURL:        parsed,
		textURL:        textURL,
		imageDim:       orDefaultInt(cfg.ImageDim, DefaultImageDim),
		faceDim:        orDefaultInt(cfg.FaceDim, DefaultFaceDim),
		requestTimeout: orDefaultDuration(cfg.RequestTimeout, DefaultRequestTimeout),
		textTimeout:    orDefaultDuration(cfg.TextTimeout, DefaultTextTimeout),
		healthTimeout:  orDefaultDuration(cfg.HealthTimeout, DefaultHealthTimeout),
		healthPath:     orDefaultString(cfg.HealthPath, DefaultHealthPath),
		client: orDefaultHTTPClient(
			cfg.HTTPClient, orDefaultDuration(cfg.DialTimeout, DefaultDialTimeout)),
	}, nil
}

// parseServiceURL parses raw as the root URL of a sidecar instance, tolerating a
// trailing slash. It returns ErrInvalidURL unless the result is an HTTP(S) URL
// carrying a host.
func parseServiceURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSuffix(raw, "/"))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%w: scheme %q must be http or https", ErrInvalidURL, parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("%w: missing host", ErrInvalidURL)
	}
	return parsed, nil
}

// endpointURL resolves endpoint against the host that serves it. Only /embed/text
// may live elsewhere (see Config.TextBaseURL); everything else — images, faces,
// OCR — must stay on baseURL, because a text-only instance answers those with a
// 503. New endpoints therefore land on baseURL by default, which is the safe
// side of this decision.
func (c *HTTPClient) endpointURL(endpoint string) *url.URL {
	if endpoint == endpointText {
		return c.textURL.JoinPath(endpoint)
	}
	return c.baseURL.JoinPath(endpoint)
}

// orDefaultInt returns v when positive, otherwise def.
func orDefaultInt(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

// orDefaultDuration returns v when positive, otherwise def.
func orDefaultDuration(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}

// orDefaultString returns v when non-empty, otherwise def.
func orDefaultString(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// orDefaultHTTPClient returns c when non-nil, otherwise an http.Client whose
// transport gives up on an unanswered connection after dialTimeout. Bounding the
// dial matters more than it looks: the sidecar's box is usually powered off, and
// the stock transport would spend 30 s on a connection that will never open —
// long enough to make an interactive search look broken. The overall per-request
// deadlines are applied via context, so the client itself carries no Timeout.
func orDefaultHTTPClient(c *http.Client, dialTimeout time.Duration) *http.Client {
	if c != nil {
		return c
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		// Unreachable with the stdlib default; fall back to a plain client rather
		// than panicking if something replaced it.
		return &http.Client{}
	}
	transport = transport.Clone()
	transport.DialContext = (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext
	return &http.Client{Transport: transport}
}

// embeddingResponse is the shared image/text endpoint response body.
type embeddingResponse struct {
	Dim        int       `json:"dim"`
	Embedding  []float32 `json:"embedding"`
	Model      string    `json:"model"`
	Pretrained string    `json:"pretrained"`
}

// faceItem is a single face entry in faceEnvelope.
type faceItem struct {
	FaceIndex int       `json:"face_index"`
	Dim       int       `json:"dim"`
	Embedding []float32 `json:"embedding"`
	BBox      []float64 `json:"bbox"`
	DetScore  float64   `json:"det_score"`
}

// faceEnvelope is the /embed/face response: a count, the model tag, and the
// per-face detections.
type faceEnvelope struct {
	FacesCount int        `json:"faces_count"`
	Model      string     `json:"model"`
	Faces      []faceItem `json:"faces"`
}

// ImageEmbedding computes the image embedding of the image streamed from img by
// POSTing a multipart "file" part to /embed/image. It validates that the
// returned vector matches the configured image dimensionality.
func (c *HTTPClient) ImageEmbedding(
	ctx context.Context, img io.Reader,
) (embedding []float32, model, pretrained string, err error) {
	body, err := c.postMultipart(ctx, endpointImage, img)
	if err != nil {
		return nil, "", "", err
	}
	return c.parseEmbedding(body, c.imageDim, endpointImage)
}

// TextEmbedding computes the embedding of text by POSTing JSON {"text":...}
// to /embed/text. It validates the returned dimensionality. Unlike the image and
// face endpoints this one answers an interactive search, so it is bounded by the
// much shorter text timeout: the caller degrades to full-text on failure, and a
// long wait is strictly worse than results it could already have shown. For the
// same reason it is the only call that may be routed to a second host — see
// Config.TextBaseURL.
func (c *HTTPClient) TextEmbedding(
	ctx context.Context, text string,
) (embedding []float32, model, pretrained string, err error) {
	body, err := c.postJSON(ctx, endpointText, map[string]string{"text": text}, c.textTimeout)
	if err != nil {
		return nil, "", "", err
	}
	return c.parseEmbedding(body, c.imageDim, endpointText)
}

// parseEmbedding unmarshals an embeddingResponse, validates it is non-empty and
// matches wantDim, and returns the vector with its metadata. endpoint is used
// only for error context.
func (c *HTTPClient) parseEmbedding(
	body []byte, wantDim int, endpoint string,
) (embedding []float32, model, pretrained string, err error) {
	var resp embeddingResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, "", "", fmt.Errorf("%s: %w: %w", endpoint, ErrBadResponse, err)
	}
	if len(resp.Embedding) == 0 {
		return nil, "", "", fmt.Errorf("%s: %w: empty embedding", endpoint, ErrBadResponse)
	}
	if len(resp.Embedding) != wantDim {
		return nil, "", "", fmt.Errorf(
			"%s: %w: got %d, want %d", endpoint, ErrDimMismatch, len(resp.Embedding), wantDim)
	}
	return resp.Embedding, resp.Model, resp.Pretrained, nil
}

// FaceEmbeddings detects faces in the image streamed from img by POSTing a
// multipart "file" part to /embed/face. Each returned face embedding is
// validated against the configured face dimensionality.
func (c *HTTPClient) FaceEmbeddings(
	ctx context.Context, img io.Reader,
) (faces []Face, model string, err error) {
	body, err := c.postMultipart(ctx, endpointFace, img)
	if err != nil {
		return nil, "", err
	}
	var resp faceEnvelope
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, "", fmt.Errorf("%s: %w: %w", endpointFace, ErrBadResponse, err)
	}
	out := make([]Face, 0, len(resp.Faces))
	for _, f := range resp.Faces {
		face, convErr := c.toFace(f)
		if convErr != nil {
			return nil, "", convErr
		}
		out = append(out, face)
	}
	return out, resp.Model, nil
}

// toFace validates a faceItem and converts it to a Face. It returns ErrDimMismatch
// if the embedding size is wrong or ErrBadResponse if the bounding box is malformed.
func (c *HTTPClient) toFace(f faceItem) (Face, error) {
	if len(f.Embedding) != c.faceDim {
		return Face{}, fmt.Errorf(
			"%s: %w: face %d got %d, want %d", endpointFace, ErrDimMismatch, f.FaceIndex, len(f.Embedding), c.faceDim)
	}
	if len(f.BBox) != 4 {
		return Face{}, fmt.Errorf(
			"%s: %w: face %d bbox has %d values, want 4", endpointFace, ErrBadResponse, f.FaceIndex, len(f.BBox))
	}
	return Face{
		Index:     f.FaceIndex,
		Dim:       f.Dim,
		Embedding: f.Embedding,
		BBox:      [4]float64{f.BBox[0], f.BBox[1], f.BBox[2], f.BBox[3]},
		DetScore:  f.DetScore,
	}, nil
}

// Healthy reports whether the sidecar answers a GET on the configured health
// path within the health timeout. Any HTTP response (even a non-2xx one) counts
// as reachable, since the goal is to detect whether the box is online; only a
// transport failure or timeout is treated as offline.
//
// It probes BaseURL even when a separate TextBaseURL is configured, and must
// keep doing so: this is the probe internal/wake reads to decide whether to send
// a magic packet. Pointed at an always-on text instance it would report a green
// light forever, no packet would ever be sent, and the image_embed/face_detect
// queue would wait behind it indefinitely.
func (c *HTTPClient) Healthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, c.healthTimeout)
	defer cancel()

	reqURL := c.baseURL.JoinPath(c.healthPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return false
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// SidecarHealth is the part of the sidecar's GET /health answer Kukátko acts on:
// which image model is loaded and how wide the vectors it returns are. The
// service publishes it precisely so a model swap can be noticed — the swap from
// CLIP ViT-L-14 to SigLIP 2 changed the width from 768 to 1152, and a Kukátko
// still configured for the old width meets it as every image_embed job failing
// with the non-transient ErrDimMismatch and semantic search silently answering
// from full text.
type SidecarHealth struct {
	// Model and Pretrained are the open_clip tags of the loaded image tower, e.g.
	// "ViT-SO400M-14-SigLIP2-378" and "webli".
	Model, Pretrained string
	// Dim is the width of the vectors /embed/image and /embed/text return. It is
	// zero when the sidecar reports no dimension (an older build), which callers
	// must read as "unknown" rather than as a mismatch.
	Dim int
	// Precision is the inference precision tag, e.g. "fp16". Informational only.
	Precision string
}

// healthResponse is the GET /health body. Only the image-tower block is decoded;
// the sidecar still names that block "clip" although the model behind it is now
// SigLIP 2, and the face block is a separate model this check does not police.
type healthResponse struct {
	Clip struct {
		Model      string `json:"model"`
		Pretrained string `json:"pretrained"`
		Dim        int    `json:"dim"`
		Precision  string `json:"precision"`
	} `json:"clip"`
}

// Health fetches GET /health and returns the image tower the sidecar currently
// serves. It is the richer sibling of Healthy: same route, same short timeout,
// but the body is parsed instead of discarded. Errors are classified exactly as
// for the embedding endpoints — ErrUnavailable for an offline box (the normal
// state here), ErrBadResponse for an answer that is not the expected JSON — so a
// caller can tell "cannot check" from "checked, and it disagrees". Like Healthy
// it asks BaseURL: the model it reports on is the image tower, which lives there.
func (c *HTTPClient) Health(ctx context.Context) (SidecarHealth, error) {
	ctx, cancel := context.WithTimeout(ctx, c.healthTimeout)
	defer cancel()

	reqURL := c.baseURL.JoinPath(c.healthPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return SidecarHealth{}, fmt.Errorf("%s: build request: %w", c.healthPath, err)
	}
	body, err := c.do(req, c.healthPath)
	if err != nil {
		return SidecarHealth{}, err
	}
	var resp healthResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return SidecarHealth{}, fmt.Errorf("%s: %w: %w", c.healthPath, ErrBadResponse, err)
	}
	return SidecarHealth{
		Model:      resp.Clip.Model,
		Pretrained: resp.Clip.Pretrained,
		Dim:        resp.Clip.Dim,
		Precision:  resp.Clip.Precision,
	}, nil
}

// postJSON marshals payload as JSON and POSTs it to endpoint within timeout,
// returning the response body or a classified error.
func (c *HTTPClient) postJSON(
	ctx context.Context, endpoint string, payload any, timeout time.Duration,
) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", endpoint, err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	reqURL := c.endpointURL(endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, endpoint)
}

// postMultipart streams img as a multipart "file" part to endpoint and returns
// the response body or a classified error. The body is streamed through an
// io.Pipe so the image is never buffered whole in memory.
func (c *HTTPClient) postMultipart(ctx context.Context, endpoint string, img io.Reader) ([]byte, error) {
	return c.postMultipartFields(ctx, endpoint, img, nil)
}

// postMultipartFields is postMultipart with extra scalar form fields (the OCR
// endpoint's min_confidence). They are written before the file part so the
// service has them in hand by the time the image starts arriving; a nil or empty
// map produces exactly the body postMultipart sends.
func (c *HTTPClient) postMultipartFields(
	ctx context.Context, endpoint string, img io.Reader, fields map[string]string,
) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	buffered := bufio.NewReader(img)
	head, _ := buffered.Peek(512) // short reads (EOF) are fine; DetectContentType handles them.
	contentType := http.DetectContentType(head)

	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)
	go streamFilePart(pipeWriter, writer, buffered, contentType, fields)

	reqURL := c.endpointURL(endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), pipeReader)
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		return nil, fmt.Errorf("%s: build request: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return c.do(req, endpoint)
}

// streamFilePart writes the scalar fields (if any) followed by a single "file"
// multipart part with the given content type, copying src into it, then closes
// the writer and pipe. Any error is propagated to the HTTP request via the pipe.
func streamFilePart(
	pw *io.PipeWriter, writer *multipart.Writer, src io.Reader, contentType string, fields map[string]string,
) {
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="image.jpg"`)
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		_ = pw.CloseWithError(err)
		return
	}
	if _, err := io.Copy(part, src); err != nil {
		_ = pw.CloseWithError(err)
		return
	}
	_ = pw.CloseWithError(writer.Close())
}

// do executes req, reads the body, and classifies the outcome. Transport errors
// and gateway-style statuses map to ErrUnavailable; other non-200 statuses map
// to ErrBadResponse. endpoint is used only for error context.
func (c *HTTPClient) do(req *http.Request, endpoint string) ([]byte, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, transportError(req.Context(), endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w: read body: %w", endpoint, ErrUnavailable, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, statusError(endpoint, resp.StatusCode, body)
	}
	return body, nil
}

// transportError classifies an http.Client.Do failure. A cancelled or expired
// caller context is surfaced as-is; everything else (refused, reset, DNS, our
// own timeout) means the box is unreachable and maps to ErrUnavailable.
func transportError(ctx context.Context, endpoint string, err error) error {
	if cerr := ctx.Err(); cerr != nil && errors.Is(cerr, context.Canceled) {
		return fmt.Errorf("%s: %w", endpoint, cerr)
	}
	return fmt.Errorf("%s: %w: %w", endpoint, ErrUnavailable, err)
}

// statusError maps a non-200 status to a sentinel. 502/503/504 mean the box is
// down or restarting (retryable ErrUnavailable); anything else is ErrBadResponse.
// The body is truncated to keep error messages bounded.
func statusError(endpoint string, code int, body []byte) error {
	switch code {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Errorf("%s: %w: status %d", endpoint, ErrUnavailable, code)
	default:
		return fmt.Errorf("%s: %w: status %d: %s", endpoint, ErrBadResponse, code, truncate(body, 256))
	}
}

// truncate returns at most maxLen bytes of body as a string, appending an
// ellipsis when it was shortened.
func truncate(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "…"
}

// IsUnavailable reports whether err indicates the sidecar was unreachable and
// the operation should be retried later without burning a job attempt.
func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}
