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

// redirectCacheControl keeps a redirect to a signed URL out of every cache. The
// media it points at is immutable, but the signature that authorizes the fetch
// expires, so a cached redirect would eventually send the client to a 403.
const redirectCacheControl = "private, no-store"

// redirectToMedia sends the client to the signed, short-lived URL at which the
// edge Worker serves the object, transferring no media bytes through this
// application. It is only ever reached after the caller has been authorized to
// see the photo — minting the URL is exactly the act of granting access to the
// object, since the Worker will serve anyone who presents the signature. The
// private flag and the archive are therefore real security boundaries here, not
// presentation filters: see the mediaurl package doc.
func redirectToMedia(w http.ResponseWriter, r *http.Request, target string) {
	w.Header().Set("Cache-Control", redirectCacheControl)
	http.Redirect(w, r, target, http.StatusFound)
}

// handleThumb streams a cached thumbnail for the photo named in the path,
// generating it on a cache miss. An unknown size is answered with 400, a missing
// photo with 404. The JPEG is streamed (never buffered whole) with an ETag so
// repeat fetches can be answered 304.
//
// When the storage backend publishes its objects the route instead redirects to
// the thumbnail's signed URL, so old links and bookmarks keep working without
// this application ever touching the bytes. On that path the route neither
// generates the thumbnail nor uploads it: it assumes the object already sits in
// the bucket under thumb.RelPath's key.
//
// Two writers keep the object there. thumb.Thumbnailer uploads every size it
// generates to the bucket under thumb.RelPath's key whenever the backend
// publishes URLs (storage.Storage's Put, the method that writes an object at a
// caller-chosen key which Store cannot), so a freshly ingested photo has both its
// original (Store wrote it on ingest, as it does the video below) and its
// thumbnails in the bucket. And `kukatko storage migrate-to-r2` copies each
// photo's original and its cached thumbnails into the bucket the same way, which
// backfills every photo whose thumbnails predate that upload-on-generate
// behaviour. So on a publishing backend the object this route redirects to is
// there.
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

	if signed := a.media.ThumbObject(photo.FileHash, size); signed != "" {
		redirectToMedia(w, r, signed)
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

// handleDownload streams a photo's file as an attachment. When the photo carries
// a non-destructive edit (and the caller did not ask for ?original=true) the
// edited image is rendered on the fly and served instead, so a download honours
// the edits while the stored original is never modified. A missing photo or a
// file gone from storage is answered with 404. The original is streamed chunk by
// chunk (never buffered whole); an edited image is rendered into memory because a
// transform cannot be streamed.
func (a *API) handleDownload(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	photo, err := a.store.GetByUID(r.Context(), uid)
	if err != nil {
		writePhotoError(w, err, "fetching photo failed")
		return
	}

	if a.maybeServeEdited(w, r, photo) {
		return
	}
	a.serveOriginal(w, r, photo)
}

// serveOriginal streams the photo's stored original file as an attachment,
// answering 404 when the file is gone from storage and 500 on any other open
// error. When the storage backend publishes its objects it redirects to the
// original's signed URL instead, and the bytes never pass through here.
func (a *API) serveOriginal(w http.ResponseWriter, r *http.Request, photo photos.Photo) {
	if signed := a.media.Object(photo.FilePath); signed != "" {
		redirectToMedia(w, r, signed)
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
