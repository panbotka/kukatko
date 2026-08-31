package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Sentinel errors raised before an upload is attempted, so a mistyped path costs
// no connection and no half-sent request.
var (
	// ErrNoUploadFiles indicates an upload command given no files at all.
	ErrNoUploadFiles = errors.New("ctl: at least one file to upload is required")
	// ErrNotRegularFile indicates a path that is a directory, a device or
	// anything else that is not a file whose bytes can be streamed. Importing a
	// whole directory is `kukatko import dir`, which runs beside the library.
	ErrNotRegularFile = errors.New("ctl: not a regular file")
)

const (
	// uploadPath is the ingest endpoint, the one door into the catalogue.
	uploadPath = "/upload"
	// uploadField is the multipart field name every file part carries. The server
	// reads whichever parts have a filename and ignores the field name, so this is
	// only what a human reading a capture sees.
	uploadField = "files"
)

// Upload outcomes, mirroring ingest.Outcome. They are spelled out here rather
// than imported so ctl keeps linking none of the server's domain packages; the
// server is the one that decides which of them a file gets.
const (
	// UploadCreated means a new photo was catalogued.
	UploadCreated = "created"
	// UploadDuplicate means the file's bytes were already in the library, matched
	// by their SHA256; nothing new was created.
	UploadDuplicate = "duplicate"
	// UploadFailed means the file could not be ingested at all.
	UploadFailed = "error"
)

// UploadWarning is a non-fatal condition the server reported about a file it
// still catalogued — a near-duplicate match, or a side effect (thumbnails,
// perceptual hashes, the follow-up jobs) that can be regenerated later.
type UploadWarning struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	PhotoUID string `json:"photo_uid,omitempty"`
}

// UploadResult is what became of one uploaded file. Status carries the per-file
// HTTP-style code the server assigns (201 created, 409 duplicate, 413/500 error),
// so a batch reports each file's fate without failing as a whole.
type UploadResult struct {
	Filename string          `json:"filename"`
	Status   int             `json:"status"`
	Outcome  string          `json:"outcome"`
	PhotoUID string          `json:"photo_uid,omitempty"`
	Error    string          `json:"error,omitempty"`
	Warnings []UploadWarning `json:"warnings,omitempty"`
}

// UploadReport is the whole answer of POST /upload: one result per file, in the
// order the files were sent.
type UploadReport struct {
	Results []UploadResult `json:"results"`
}

// UploadCounts is how a report reads at a glance.
type UploadCounts struct {
	Total     int
	Created   int
	Duplicate int
	Failed    int
}

// Counts tallies the report's outcomes. Anything the server classified as
// neither created nor duplicate counts as a failure, so an outcome added later
// is reported as a problem rather than silently ignored.
func (r UploadReport) Counts() UploadCounts {
	counts := UploadCounts{Total: len(r.Results)}
	for _, result := range r.Results {
		switch result.Outcome {
		case UploadCreated:
			counts.Created++
		case UploadDuplicate:
			counts.Duplicate++
		default:
			counts.Failed++
		}
	}
	return counts
}

// uploadFile is one file resolved for upload: where to read it and the name the
// server is told, which is always the bare base name.
type uploadFile struct {
	path string
	name string
}

// UploadPhotos sends the given files to POST /upload, the ordinary ingest path:
// the server streams each one, deduplicates it on its SHA256, extracts its
// metadata, renders its thumbnails and queues the follow-up jobs. It returns the
// raw JSON body so `-o json` prints the server's own bytes; decode it with
// DecodeUploadReport.
//
// The bytes never pass through memory: the multipart body is written into a pipe
// as the request is sent, so a hundred-megabyte original costs one buffer, not a
// hundred megabytes. It uses Client.stream — the client with no timeout —
// because a large upload is slow on purpose and only the caller's context should
// be able to end it.
//
// Every path is checked to be an existing regular file first, so a typo fails
// before a single byte is sent rather than half way through the batch.
func (c *Client) UploadPhotos(ctx context.Context, paths []string) (json.RawMessage, error) {
	files, err := resolveUploadFiles(paths)
	if err != nil {
		return nil, err
	}
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	go func() {
		_ = writer.CloseWithError(writeUploadBody(form, files))
	}()

	req, err := c.newStreamRequest(ctx, http.MethodPost, uploadPath, nil, form.FormDataContentType(), reader)
	if err != nil {
		// Unblock the writer goroutine, which is otherwise waiting for a reader
		// that will never come.
		_ = reader.CloseWithError(err)
		return nil, err
	}
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting %s: %w", uploadPath, err)
	}
	defer func() { _ = resp.Body.Close() }()

	return c.readBody(resp, uploadPath)
}

// resolveUploadFiles checks every path and pairs it with the name the server is
// told. It returns ErrNoUploadFiles for an empty set and ErrNotRegularFile for
// anything that is not a file — a directory included, since walking one is
// `kukatko import dir`, which runs next to the library and not down a pipe.
func resolveUploadFiles(paths []string) ([]uploadFile, error) {
	if len(paths) == 0 {
		return nil, ErrNoUploadFiles
	}
	files := make([]uploadFile, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: %s", ErrNotRegularFile, path)
		}
		files = append(files, uploadFile{path: path, name: filepath.Base(path)})
	}
	if len(files) == 0 {
		return nil, ErrNoUploadFiles
	}
	return files, nil
}

// writeUploadBody streams every file into the multipart writer and closes it,
// returning the first failure. The returned error travels to the request body's
// reader through the pipe, which aborts the request instead of sending a
// truncated file the server would catalogue as a whole one.
func writeUploadBody(form *multipart.Writer, files []uploadFile) error {
	for _, file := range files {
		if err := writeUploadPart(form, file); err != nil {
			return err
		}
	}
	if err := form.Close(); err != nil {
		return fmt.Errorf("finishing the upload body: %w", err)
	}
	return nil
}

// writeUploadPart copies one file into its own multipart part.
func writeUploadPart(form *multipart.Writer, file uploadFile) error {
	source, err := os.Open(file.path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", file.path, err)
	}
	part, err := form.CreateFormFile(uploadField, file.name)
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("starting the upload of %s: %w", file.path, err)
	}
	_, copyErr := io.Copy(part, source)
	closeErr := source.Close()
	if joined := errors.Join(copyErr, closeErr); joined != nil {
		return fmt.Errorf("streaming %s: %w", file.path, joined)
	}
	return nil
}

// DecodeUploadReport decodes the {"results":[…]} answer of POST /upload.
func DecodeUploadReport(raw json.RawMessage) (UploadReport, error) {
	var report UploadReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return UploadReport{}, fmt.Errorf("decoding the upload result: %w", err)
	}
	return report, nil
}

// WriteUploadReport renders one row per uploaded file — what it became and,
// for a new photo, the uid the rest of ctl takes — followed by a summary line.
//
// A duplicate is not an error and is not printed as one: the library already
// holds those bytes, which is the whole point of hashing them, and the row says
// so with the uid of the photo that already carries them when the server named
// it.
func WriteUploadReport(w io.Writer, report UploadReport) error {
	if len(report.Results) == 0 {
		return writeLine(w, "nothing was uploaded")
	}
	rows := make([][]string, 0, len(report.Results))
	for _, result := range report.Results {
		rows = append(rows, []string{
			elide(dash(result.Filename), fileWidth),
			dash(result.Outcome),
			dash(result.PhotoUID),
			dash(uploadNote(result)),
		})
	}
	if err := writeTable(w, []string{"FILE", "OUTCOME", "UID", "NOTE"}, rows); err != nil {
		return err
	}
	return writeLine(w, "\n"+uploadSummary(report.Counts()))
}

// uploadNote renders what else is worth knowing about one file: the reason it
// failed, or the warning codes attached to a photo that was still catalogued.
func uploadNote(result UploadResult) string {
	if result.Error != "" {
		return collapseLines(result.Error)
	}
	codes := make([]string, 0, len(result.Warnings))
	for _, warning := range result.Warnings {
		codes = append(codes, warning.Code)
	}
	return strings.Join(codes, ", ")
}

// uploadSummary builds the one-line footer of an upload.
func uploadSummary(counts UploadCounts) string {
	return strings.Join([]string{
		strconv.Itoa(counts.Total) + " files",
		strconv.Itoa(counts.Created) + " created",
		strconv.Itoa(counts.Duplicate) + " already in the library",
		strconv.Itoa(counts.Failed) + " failed",
	}, " · ")
}
