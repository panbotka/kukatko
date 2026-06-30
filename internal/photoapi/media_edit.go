package photoapi

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	// Image format decoders registered for image.Decode: originals may be JPEG,
	// PNG or WebP (HEIC/RAW are converted to a decodable JPEG by imgconvert first).
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"

	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/photoedit"
	"github.com/panbotka/kukatko/internal/photos"
)

// editedDownloadQuality is the JPEG quality used when re-encoding an edited
// image for download — high enough that the adjustment, not the codec, is the
// only visible change.
const editedDownloadQuality = 90

// maybeServeEdited renders and serves the photo's edited image when one applies,
// returning true when it has fully written the response. It declines (returns
// false, leaving the response untouched) when the caller asked for the original,
// the media is a video, no non-identity edit is stored, or rendering fails — so
// the caller falls back to streaming the original. The edited image is keyed by a
// distinct ETag so it never collides with the original in a cache.
func (a *API) maybeServeEdited(w http.ResponseWriter, r *http.Request, photo photos.Photo) bool {
	// A truthy ?original disables edit rendering so the explicit "download
	// original" action gets the untouched bytes; videos are never re-rendered.
	wantOriginal, _ := strconv.ParseBool(r.URL.Query().Get("original"))
	if wantOriginal || photo.MediaType == photos.MediaVideo {
		return false
	}
	edit, err := a.store.GetEdit(r.Context(), photo.UID)
	if err != nil || photoedit.IsIdentity(edit) {
		return false
	}
	data, err := a.renderEdited(r.Context(), photo, edit)
	if err != nil {
		// Best-effort: log and let the caller serve the unedited original.
		log.Printf("photoapi: rendering edited download for %s: %v", photo.UID, err)
		return false
	}

	etag := strconv.Quote(photo.FileHash + "-edit")
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", originalCacheControl)
	w.Header().Set("Content-Disposition", contentDisposition(editedFileName(photo.FileName)))
	streamMedia(w, r, bytes.NewReader(data), etag, int64(len(data)))
	return true
}

// renderEdited decodes the photo's original, orients it per its EXIF orientation,
// applies the non-destructive edit and re-encodes the result as a JPEG byte
// slice. HEIC/RAW originals are converted to a decodable form first. The whole
// image is held in memory, which is unavoidable for a transform.
func (a *API) renderEdited(ctx context.Context, photo photos.Photo, edit photos.Edit) ([]byte, error) {
	absPath := a.storage.AbsPath(photo.FilePath)
	decodable, cleanup, err := imgconvert.EnsureDecodable(ctx, absPath)
	if err != nil {
		return nil, fmt.Errorf("photoapi: ensuring decodable: %w", err)
	}
	defer cleanup()

	file, err := os.Open(decodable) //nolint:gosec // path is confined by storage.AbsPath.
	if err != nil {
		return nil, fmt.Errorf("photoapi: opening original: %w", err)
	}
	defer func() { _ = file.Close() }()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("photoapi: decoding original: %w", err)
	}

	oriented := photoedit.Orient(img, photo.FileOrientation)
	edited := photoedit.Apply(oriented, edit)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, edited, &jpeg.Options{Quality: editedDownloadQuality}); err != nil {
		return nil, fmt.Errorf("photoapi: encoding edited image: %w", err)
	}
	return buf.Bytes(), nil
}

// editedFileName derives the download filename for an edited image: the original
// base name with a .jpg extension, since the edited image is always re-encoded as
// JPEG regardless of the original format.
func editedFileName(name string) string {
	if name == "" {
		return "download.jpg"
	}
	if idx := strings.LastIndex(name, "."); idx > 0 {
		return name[:idx] + ".jpg"
	}
	return name + ".jpg"
}
