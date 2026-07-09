// Package mediaurl builds the addresses at which a client fetches a photo's
// media — its grid thumbnail and its original bytes — and stamps them onto the
// photo payloads the HTTP API returns.
//
// There is exactly one decision here, and the storage backend makes it. A
// backend that publishes its objects (storage.R2) returns a short-lived signed
// URL from URL(), pointing at the edge Worker that fronts the private bucket;
// the application then transfers no image bytes at all. A backend whose objects
// no browser can reach (storage.FS) returns the empty string, and this package
// falls back to the application's own media routes, which stream the file. Above
// this package nothing knows which of the two it is looking at.
//
// # Authorization
//
// Authorization gates *discovery*: a URL is minted only into a response the
// caller was already authorized to receive, so a user who may not see a photo
// never learns a URL for it. The object itself is gated by the signature the
// Worker verifies, and a signature only ever comes from such a response.
//
// This is worth stating plainly because an earlier draft of this design put the
// objects in a *public* bucket and reasoned that an unguessable key was enough.
// Under that design photos.private and the archive were presentation filters and
// nothing more: anyone holding a key could read the object forever, whatever the
// catalogue said. That is no longer true and must not be reintroduced. **The
// private flag and the archive are real security boundaries.** A handler that
// hands out a media URL for a photo the caller may not see has published the
// photo, and no TTL makes that acceptable.
package mediaurl

import (
	"net/url"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// photosPath is the API path prefix under which this application serves photo
// media itself. It is the fallback target for a backend that mints no URLs, and
// it mirrors the base path the server mounts the API on (/api/v1).
const photosPath = "/api/v1/photos/"

// Builder mints the client-facing media addresses for photos read out of the
// catalogue. The zero value is not usable; call NewBuilder. A nil *Builder is
// valid and behaves like a builder over a backend that publishes nothing — it
// yields the application's own media routes — so a caller with no storage wired
// (a test, an API constructed without one) still produces working payloads.
type Builder struct {
	// store is the storage backend whose URL method decides whether media is
	// served from the edge or from this application. Nil means the latter.
	store storage.Storage
}

// NewBuilder returns a Builder over store. A nil store yields a builder that
// always falls back to the application's media routes.
func NewBuilder(store storage.Storage) *Builder {
	return &Builder{store: store}
}

// Object returns the signed, client-fetchable URL of the stored object at
// relPath, or the empty string when the backend publishes no such address and
// the application must serve the bytes itself. It is the raw backend answer: use
// it to decide whether a media route should redirect or stream.
func (b *Builder) Object(relPath string) string {
	if b == nil || b.store == nil {
		return ""
	}
	return b.store.URL(relPath)
}

// ThumbObject returns the signed URL of the cached thumbnail object for the
// given file hash and size, or the empty string when the backend publishes no
// URLs or the hash/size pair is not a valid cache key.
func (b *Builder) ThumbObject(fileHash, size string) string {
	rel, err := thumb.RelPath(fileHash, size)
	if err != nil {
		return ""
	}
	return b.Object(rel)
}

// Thumb returns where a client fetches the photo's thumbnail at size: the signed
// edge URL when the backend publishes one, otherwise this application's thumb
// route for uid, which streams the bytes (generating them on a cache miss).
func (b *Builder) Thumb(uid, fileHash, size string) string {
	if signed := b.ThumbObject(fileHash, size); signed != "" {
		return signed
	}
	return photosPath + url.PathEscape(uid) + "/thumb/" + url.PathEscape(size)
}

// Download returns where a client fetches the photo's untouched original bytes:
// the signed edge URL of the stored object when the backend publishes one,
// otherwise this application's download route for uid. The route is asked for
// ?original=true so both answers mean the same thing — the stored original,
// never a rendering of a non-destructive edit, which only the application can
// produce and which the plain download route would serve instead.
func (b *Builder) Download(uid, filePath string) string {
	if signed := b.Object(filePath); signed != "" {
		return signed
	}
	return photosPath + url.PathEscape(uid) + "/download?original=true"
}

// Decorate fills in ThumbURL and DownloadURL on every photo in list, in place.
// Call it on any page of photos on its way into a JSON response: a payload
// without these is a photo the client cannot render.
func (b *Builder) Decorate(list []photos.Photo) {
	for i := range list {
		b.DecorateOne(&list[i])
	}
}

// DecorateOne fills in ThumbURL and DownloadURL on a single photo, in place.
func (b *Builder) DecorateOne(photo *photos.Photo) {
	photo.ThumbURL = b.Thumb(photo.UID, photo.FileHash, thumb.GridSize)
	photo.DownloadURL = b.Download(photo.UID, photo.FilePath)
}
