package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"strconv"
	"strings"

	"github.com/panbotka/kukatko/internal/project"
)

// CodeFileTooLarge is the stable FileResult.Code (and the request-level error
// body's "error") of a file refused for its size — over the smallest of the
// project's, the instance's and an upload link's per-file caps.
const CodeFileTooLarge = "file_too_large"

// CodeQuotaExceeded is the stable FileResult.Code (and the request-level error
// body's "error") of a file refused because storing it would take the project's
// originals past their quota.
const CodeQuotaExceeded = "quota_exceeded"

// ErrQuotaExceeded indicates storing a file would take its project's originals
// past the project's quota (projects.max_total_bytes). Nothing is stored and
// nothing already stored is touched.
var ErrQuotaExceeded = errors.New("ingest: project storage quota exceeded")

// SizeField is the multipart form field a client may send immediately before a
// file part to declare that file's size in bytes, so an oversized file — or one
// the project's quota cannot take — is refused before its body is read. A part's
// own Content-Length header serves the same purpose and wins over the field.
// The declaration is a hint for refusing early, never a promise: the byte count
// is enforced again while the file streams.
const SizeField = "size"

// maxSizeFieldBytes bounds the read of a SizeField value; a decimal int64 fits.
const maxSizeFieldBytes = 32

// LimitError is the refusal of a file by one of the project limits. It doubles
// as the request-level 413 body ({"error":"file_too_large","limit_bytes":N} or
// {"error":"quota_exceeded","limit_bytes":N,"used_bytes":M}) and unwraps to
// ErrFileTooLarge or ErrQuotaExceeded.
type LimitError struct {
	// Code is CodeFileTooLarge or CodeQuotaExceeded.
	Code string `json:"error"`
	// LimitBytes is the cap that refused the file: the effective per-file cap,
	// or the project's quota.
	LimitBytes int64 `json:"limit_bytes"`
	// UsedBytes is the project's stored originals at the time of the refusal;
	// set for a quota refusal only.
	UsedBytes *int64 `json:"used_bytes,omitempty"`
}

// Error describes the refusal with its numbers.
func (e *LimitError) Error() string {
	if e.Code == CodeQuotaExceeded {
		var used int64
		if e.UsedBytes != nil {
			used = *e.UsedBytes
		}
		return fmt.Sprintf("%s: %d of %d bytes already stored", ErrQuotaExceeded, used, e.LimitBytes)
	}
	return fmt.Sprintf("%s of %d bytes", ErrFileTooLarge, e.LimitBytes)
}

// Unwrap returns the sentinel the refusal stands for, so errors.Is works.
func (e *LimitError) Unwrap() error {
	if e.Code == CodeQuotaExceeded {
		return ErrQuotaExceeded
	}
	return ErrFileTooLarge
}

// tooLarge is the per-file cap's refusal.
func tooLarge(limit int64) *LimitError {
	return &LimitError{Code: CodeFileTooLarge, LimitBytes: limit}
}

// UsageReader measures a project's stored originals. It is satisfied by
// *project.Store.
type UsageReader interface {
	// Usage returns the project's photo count and the summed size of its
	// originals.
	Usage(ctx context.Context, uid string) (project.Usage, error)
}

// fileCap is the effective per-file cap of req in bytes: the smallest non-zero
// of the project's max_file_bytes, the instance's upload.max_file_size_mb and
// the caller's own cap (an upload link's). 0 means no cap at all.
func (s *Service) fileCap(req Request) int64 {
	caps := []int64{s.maxFileSize, req.MaxFileSize}
	if req.project != nil && req.project.MaxFileBytes != nil {
		caps = append(caps, *req.project.MaxFileBytes)
	}
	return smallestPositive(caps...)
}

// smallestPositive returns the smallest value above zero, or 0 when there is none.
func smallestPositive(values ...int64) int64 {
	var smallest int64
	for _, v := range values {
		if v > 0 && (smallest == 0 || v < smallest) {
			smallest = v
		}
	}
	return smallest
}

// CheckQuota reports whether the project proj can take incoming more bytes of
// originals: it returns a *LimitError (CodeQuotaExceeded) when the project's
// stored originals plus incoming would exceed its max_total_bytes, nil when they
// would not or the project has no quota. An incoming of 0 or less stands for "a
// file of unknown size", which needs at least one byte — so it asks whether the
// quota is already used up, which is what a handler checks before reading a
// request's body.
//
// The usage is read without a lock: two uploads checked at the same moment may
// both pass and overshoot the quota by up to one file each. That is accepted —
// the quota is a guard against a library growing without bound, not an exact
// ledger — and nothing already stored is ever deleted to make room. A Service
// without a UsageReader enforces no quota. A failed usage read is returned as
// an error (the file is refused, not let past an unknown total).
func (s *Service) CheckQuota(ctx context.Context, proj *project.Project, incoming int64) error {
	if proj == nil || proj.MaxTotalBytes == nil || s.usage == nil {
		return nil
	}
	usage, err := s.usage.Usage(ctx, proj.UID)
	if err != nil {
		return fmt.Errorf("ingest: measuring the project's usage: %w", err)
	}
	limit, used := *proj.MaxTotalBytes, usage.OriginalsBytes
	if incoming < 1 {
		incoming = 1
	}
	if used+incoming > limit {
		return &LimitError{Code: CodeQuotaExceeded, LimitBytes: limit, UsedBytes: &used}
	}
	return nil
}

// preflight refuses req before a byte of its body is read when its declared
// size alone already settles it: over the effective per-file cap, or more than
// the project's quota has left (with no declared size: a quota already used up).
func (s *Service) preflight(ctx context.Context, req Request) error {
	if limit := s.fileCap(req); limit > 0 && req.DeclaredSize > limit {
		return tooLarge(limit)
	}
	return s.CheckQuota(ctx, req.project, req.DeclaredSize)
}

// PartSize returns the size a multipart file part declares for itself: its own
// Content-Length header when it carries a valid one, otherwise declared (the
// value of a preceding SizeField, 0 for none). It never reads the part.
func PartSize(part *multipart.Part, declared int64) int64 {
	if v := part.Header.Get("Content-Length"); v != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n >= 0 {
			return n
		}
	}
	return declared
}

// ReadSizeField reads a SizeField part's value as a byte count, 0 when it is
// missing, malformed or negative — an unusable declaration is simply no
// declaration, never a refusal.
func ReadSizeField(part io.Reader) int64 {
	raw, err := io.ReadAll(io.LimitReader(part, maxSizeFieldBytes))
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// limitFields copies a limit refusal's numbers onto its per-file result.
func limitFields(res *FileResult, err error) {
	var limitErr *LimitError
	if !errors.As(err, &limitErr) {
		if errors.Is(err, ErrFileTooLarge) {
			res.Code = CodeFileTooLarge
		}
		return
	}
	res.Code = limitErr.Code
	res.LimitBytes = limitErr.LimitBytes
	res.UsedBytes = limitErr.UsedBytes
}
