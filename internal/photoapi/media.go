package photoapi

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/thumb"
)

// thumbCacheControl is the caching policy for thumbnails: they are immutable per
// (file hash, size), so a client may cache them for a year. The response is
// marked private because it is served only to authenticated callers.
const thumbCacheControl = "private, max-age=31536000, immutable"

// originalCacheControl is the caching policy for original downloads: immutable
// per content hash but kept private to the authenticated caller.
const originalCacheControl = "private, max-age=31536000, immutable"

// handleThumb streams a cached thumbnail for the photo named in the path,
// generating it on a cache miss. An unknown size is answered with 400, a missing
// photo with 404. The JPEG is streamed (never buffered whole) with an ETag so
// repeat fetches can be answered 304.
func (a *API) handleThumb(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	size := chi.URLParam(r, "size")
	if !thumb.IsValidSize(size) {
		writeError(w, http.StatusBadRequest, "unknown thumbnail size")
		return
	}

	photo, err := a.store.GetByUID(r.Context(), uid)
	if err != nil {
		writePhotoError(w, err, "fetching photo failed")
		return
	}

	reader, err := a.openThumb(r, photo, size)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "thumbnail unavailable")
		return
	}
	defer func() { _ = reader.Close() }()

	etag := strconv.Quote(photo.FileHash + "-" + size)
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", thumbCacheControl)
	streamMedia(w, r, reader, etag, 0)
}

// openThumb returns a reader for the photo's thumbnail at size, generating the
// thumbnail when it is not yet cached.
func (a *API) openThumb(r *http.Request, photo photos.Photo, size string) (io.ReadCloser, error) {
	reader, err := a.thumbnailer.Open(photo.FileHash, size)
	if err == nil {
		return reader, nil
	}
	if !errors.Is(err, thumb.ErrNotCached) {
		return nil, fmt.Errorf("photoapi: opening thumbnail: %w", err)
	}
	if _, genErr := a.thumbnailer.Generate(r.Context(), photo, size); genErr != nil {
		return nil, fmt.Errorf("photoapi: generating thumbnail: %w", genErr)
	}
	reader, err = a.thumbnailer.Open(photo.FileHash, size)
	if err != nil {
		return nil, fmt.Errorf("photoapi: opening generated thumbnail: %w", err)
	}
	return reader, nil
}

// handleDownload streams a photo's original file as an attachment. A missing
// photo or a file gone from storage is answered with 404. The file is streamed
// chunk by chunk (never buffered whole in memory) with its content type, length,
// an ETag and a download filename.
func (a *API) handleDownload(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	photo, err := a.store.GetByUID(r.Context(), uid)
	if err != nil {
		writePhotoError(w, err, "fetching photo failed")
		return
	}

	reader, err := a.storage.Open(r.Context(), photo.FilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "original file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "opening original failed")
		return
	}
	defer func() { _ = reader.Close() }()

	contentType := photo.FileMime
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	etag := strconv.Quote(photo.FileHash)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", originalCacheControl)
	w.Header().Set("Content-Disposition", contentDisposition(photo.FileName))
	streamMedia(w, r, reader, etag, photo.FileSize)
}

// contentDisposition builds an attachment Content-Disposition header for name,
// falling back to a generic filename when name is empty and stripping characters
// that would break the quoted form.
func contentDisposition(name string) string {
	clean := strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r < 0x20 {
			return -1
		}
		return r
	}, name)
	if clean == "" {
		clean = "download"
	}
	return fmt.Sprintf("attachment; filename=%q", clean)
}

// streamMedia writes reader to the response, honouring conditional requests via
// etag (answering 304 when it matches If-None-Match) and advertising size as the
// Content-Length when it is positive. The body is copied with io.Copy, which
// streams in fixed-size chunks rather than buffering the whole file.
func streamMedia(w http.ResponseWriter, r *http.Request, reader io.Reader, etag string, size int64) {
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	if _, err := io.Copy(w, reader); err != nil {
		// The status line is already sent; nothing to do but log.
		log.Printf("photoapi: streaming media: %v", err)
	}
}
