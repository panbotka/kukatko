package ctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/panbotka/kukatko/internal/thumb"
)

// RenditionOriginal asks for the stored original instead of a cached thumbnail —
// the whole file, at whatever size and in whatever format it was catalogued in.
const RenditionOriginal = "original"

// ErrInvalidRendition indicates a --size that is neither a registered thumbnail
// size nor the original.
var ErrInvalidRendition = errors.New("ctl: unknown rendition size")

// defaultRenditionMIME is what a rendition is assumed to be when the server sends
// no usable Content-Type: every cached thumbnail is a JPEG.
const defaultRenditionMIME = "image/jpeg"

// DefaultRenditionSize is what `photos image` saves when --size is not given: the
// largest thumbnail that is still a modest file, big enough to read a face or a
// sign on and small enough to fetch over a slow link.
const DefaultRenditionSize = "fit_720"

// RenditionSizes returns the accepted --size values: the thumbnail registry's own
// names plus RenditionOriginal.
func RenditionSizes() []string {
	return append(thumb.SizeNames(), RenditionOriginal)
}

// ValidRendition reports whether size names a rendition the server can serve.
func ValidRendition(size string) bool {
	return size == RenditionOriginal || thumb.IsValidSize(size)
}

// Rendition describes the file SaveRendition wrote.
type Rendition struct {
	// Path is where the bytes were written.
	Path string `json:"path"`
	// Bytes is how many were written.
	Bytes int64 `json:"bytes"`
	// MediaType is the response's content type, without parameters.
	MediaType string `json:"media_type"`
}

// SaveRendition streams one rendition of a photo to disk and reports what it
// wrote. size is a thumbnail size name or RenditionOriginal; path is the file to
// write, and an empty path means "into the working directory, under a name
// derived from the response".
//
// The bytes are copied straight from the socket into the file: an original can be
// a hundred-megabyte video and is never held in memory, which is also why this
// does not go through Client.do (which buffers a bounded body for the renderers)
// and not through the shared 30-second client (a big download is slow on purpose,
// and only the caller's context should be able to end it).
//
// The download lands on a temporary file beside its destination and is renamed
// into place only once it is complete, so an interrupted transfer cannot leave a
// half-written file that looks like a photo.
func (c *Client) SaveRendition(ctx context.Context, uid, size, path string) (Rendition, error) {
	if err := requireUID("photo", uid); err != nil {
		return Rendition{}, err
	}
	if !ValidRendition(size) {
		return Rendition{}, fmt.Errorf("%w: %q", ErrInvalidRendition, size)
	}
	resp, err := c.openRendition(ctx, uid, size)
	if err != nil {
		return Rendition{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	mediaType := responseMediaType(resp)
	if strings.TrimSpace(path) == "" {
		path = renditionName(resp, uid, size, mediaType)
	}
	written, err := writeStream(path, resp.Body)
	if err != nil {
		return Rendition{}, err
	}
	return Rendition{Path: path, Bytes: written, MediaType: mediaType}, nil
}

// openRendition issues the authenticated GET for one rendition and returns the
// live response, whose body the caller must close. A storage backend that
// publishes signed URLs answers these routes with a redirect, which the HTTP
// client follows on its own.
func (c *Client) openRendition(ctx context.Context, uid, size string) (*http.Response, error) {
	path := "/photos/" + url.PathEscape(uid) + "/download"
	if size != RenditionOriginal {
		path = "/photos/" + url.PathEscape(uid) + "/thumb/" + url.PathEscape(size)
	}
	req, err := c.newRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	// These routes answer with image or video bytes, never JSON; asking for JSON
	// would be a lie, and a proxy in between is entitled to act on it.
	req.Header.Set("Accept", "*/*")
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting %s: %w", path, err)
	}
	if err := c.renditionError(resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// renditionError maps a non-2xx rendition response onto the CLI's typed errors,
// reading only a bounded snippet of the body — an error body is JSON, but a
// misconfigured proxy can answer a media route with a whole HTML page.
func (c *Client) renditionError(resp *http.Response) error {
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	snippet, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorSnippet))
	if err != nil {
		snippet = nil
	}
	return c.statusError(resp.StatusCode, snippet)
}

// writeStream copies r into path through a temporary file in the same directory,
// renaming it into place only on success so a failed transfer leaves nothing
// behind that looks finished.
func writeStream(path string, r io.Reader) (int64, error) {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return 0, fmt.Errorf("creating a temporary file in %s: %w", dir, err)
	}
	tempPath := temp.Name()
	written, copyErr := io.Copy(temp, r)
	closeErr := temp.Close()
	if joined := errors.Join(copyErr, closeErr); joined != nil {
		_ = os.Remove(tempPath)
		return 0, fmt.Errorf("writing %s: %w", path, joined)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return 0, fmt.Errorf("saving %s: %w", path, err)
	}
	return written, nil
}

// responseMediaType returns the response's content type without its parameters,
// falling back to JPEG when the server sent none or sent something unparseable.
func responseMediaType(resp *http.Response) string {
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		return defaultRenditionMIME
	}
	return mediaType
}

// renditionName picks the default file name for a saved rendition.
//
// The download route names the original in its Content-Disposition, so an
// original keeps the name the library knows it by; a thumbnail (and a redirect
// that dropped the header) falls back to "<uid>_<size><ext>". The name is always
// reduced to its base, so a hostile or careless header cannot steer the write out
// of the working directory.
func renditionName(resp *http.Response, uid, size, mediaType string) string {
	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	if err == nil {
		if base := filepath.Base(strings.TrimSpace(params["filename"])); isSafeFileName(base) {
			return base
		}
	}
	return uid + "_" + size + renditionExt(mediaType)
}

// isSafeFileName reports whether a name taken from a response header may be used
// as a file name on its own: a real name, not a path element that would climb out
// of the working directory.
func isSafeFileName(name string) bool {
	return name != "" && name != "." && name != ".." && name != string(filepath.Separator)
}

// renditionExtensions spells out the extension for the media types the library
// actually stores, because the standard table answers some of them with a list
// whose first entry is not the familiar spelling (image/jpeg → .jpe).
var renditionExtensions = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"image/heic":      ".heic",
	"image/gif":       ".gif",
	"image/tiff":      ".tiff",
	"video/mp4":       ".mp4",
	"video/quicktime": ".mov",
}

// renditionExt returns the file extension for a media type, falling back to the
// system table and then to .bin for a type nothing recognises.
func renditionExt(mediaType string) string {
	if ext, ok := renditionExtensions[mediaType]; ok {
		return ext
	}
	exts, err := mime.ExtensionsByType(mediaType)
	if err != nil || len(exts) == 0 {
		return ".bin"
	}
	return strings.ToLower(exts[0])
}
