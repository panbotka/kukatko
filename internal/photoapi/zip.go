package photoapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strings"

	"github.com/panbotka/kukatko/internal/photos"
)

// maxZipFiles caps how many originals one bulk-download request may pack into a
// single ZIP. It bounds the work — and, on a publishing backend, the number of
// objects the application proxies — a single request can trigger, so an
// accidental "select everything" cannot ask the server to stream an unbounded
// archive. A request above it is rejected with 413 before any archive byte is
// written; a selection or album larger than this is downloaded in parts.
const maxZipFiles = 1000

// missingManifestName is the name of the text entry appended to the archive
// listing the originals that could not be included because they were missing
// from storage, so the user can see what was skipped.
const missingManifestName = "MISSING.txt"

// errOriginalMissing signals that a photo's stored original could not be opened
// (it is gone from storage, or otherwise unreadable). The archive skips and
// records such an entry rather than aborting the whole download.
var errOriginalMissing = errors.New("photoapi: original missing")

// zipDownloadRequest is the JSON body of the bulk ZIP download endpoint. A
// request names the originals to pack either explicitly by photo UID (a library
// selection), by album (expanded server-side to the album's live photos so the
// client need not enumerate them), or both — the two sets are merged and
// de-duplicated. Name and Date only influence the archive's own filename, never
// which photos are packed.
type zipDownloadRequest struct {
	// PhotoUIDs lists photos to include explicitly. A UID with no matching photo
	// is silently skipped, mirroring the single-photo download.
	PhotoUIDs []string `json:"photo_uids"`
	// AlbumUID, when set, expands to every live (non-archived) photo in that album
	// in display order.
	AlbumUID string `json:"album_uid"`
	// Name is the base archive filename (without extension), e.g. an album title.
	// When empty a dated default is used.
	Name string `json:"name"`
	// Date is an optional YYYY-MM-DD stamp the caller supplies for the default
	// archive name. The server avoids wall-clock on this path, so the date is the
	// client's; it is ignored when Name is set.
	Date string `json:"date"`
}

// handleDownloadZip streams the originals named by the request as a single ZIP,
// written straight onto the HTTP response so that neither a whole file nor the
// whole archive is ever buffered in memory (a large library on a CGO-free binary
// must not blow RAM). It authorises exactly like the single-photo download (the
// requireDownload guard); whoever may download one original may download many.
// A request naming no resolvable photo is answered with 400, one over the file
// cap with 413, both before any archive byte is written. An original that has
// gone missing from storage is skipped and reported in the archive rather than
// aborting the whole download.
func (a *API) handleDownloadZip(w http.ResponseWriter, r *http.Request) {
	var req zipDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// A cheap early rejection so an oversized selection never touches the database.
	if len(req.PhotoUIDs) > maxZipFiles {
		writeZipTooMany(w, len(req.PhotoUIDs))
		return
	}
	list, err := a.collectZipPhotos(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "collecting photos failed")
		return
	}
	if len(list) == 0 {
		writeError(w, http.StatusBadRequest, "no photos to download")
		return
	}
	if len(list) > maxZipFiles {
		writeZipTooMany(w, len(list))
		return
	}
	a.streamZip(r.Context(), w, list, zipArchiveName(req))
}

// collectZipPhotos resolves a request to the ordered, de-duplicated set of
// photos to pack: an album's live photos (in display order) followed by the
// explicitly named photos (in the client's selection order), with any photo that
// appears in both included once. The album expansion is capped at maxZipFiles+1
// rows so an over-cap album is detected by the caller without loading the whole
// library.
func (a *API) collectZipPhotos(ctx context.Context, req zipDownloadRequest) ([]photos.Photo, error) {
	var out []photos.Photo
	seen := make(map[string]struct{})

	if req.AlbumUID != "" {
		albumPhotos, err := a.store.List(ctx, photos.ListParams{
			AlbumUIDs: []string{req.AlbumUID},
			Sort:      photos.SortByChronology,
			Order:     photos.OrderAsc,
			Limit:     maxZipFiles + 1,
		})
		if err != nil {
			return nil, fmt.Errorf("photoapi: listing album photos: %w", err)
		}
		out = appendUnique(out, seen, albumPhotos)
	}

	if len(req.PhotoUIDs) > 0 {
		explicit, err := a.store.ListByUIDs(ctx, req.PhotoUIDs)
		if err != nil {
			return nil, fmt.Errorf("photoapi: listing photos by uid: %w", err)
		}
		out = appendUnique(out, seen, orderByUIDs(req.PhotoUIDs, explicit))
	}

	return out, nil
}

// appendUnique appends each photo in add to out unless its UID is already in
// seen, recording every newly appended UID so a photo named by both the album
// and the explicit list is packed once.
func appendUnique(out []photos.Photo, seen map[string]struct{}, add []photos.Photo) []photos.Photo {
	for _, p := range add {
		if _, ok := seen[p.UID]; ok {
			continue
		}
		seen[p.UID] = struct{}{}
		out = append(out, p)
	}
	return out
}

// orderByUIDs returns list reordered to match the sequence of uids (the client's
// selection order); a UID with no matching photo is dropped. ListByUIDs returns
// rows in an unspecified order, so this restores the requested order for the
// archive.
func orderByUIDs(uids []string, list []photos.Photo) []photos.Photo {
	byUID := make(map[string]photos.Photo, len(list))
	for _, p := range list {
		byUID[p.UID] = p
	}
	out := make([]photos.Photo, 0, len(uids))
	for _, uid := range uids {
		if p, ok := byUID[uid]; ok {
			out = append(out, p)
		}
	}
	return out
}

// streamZip writes list to w as a ZIP archive named archiveName, one entry per
// photo, streaming each original straight from storage into the archive writer
// so nothing is buffered whole. A photo whose original is missing is skipped and
// recorded in a trailing manifest entry rather than aborting the archive; any
// other write failure means the response is already corrupt and the stream is
// abandoned (there is no status line left to change).
func (a *API) streamZip(ctx context.Context, w http.ResponseWriter, list []photos.Photo, archiveName string) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition(archiveName))

	zw := zip.NewWriter(w)
	used := make(map[string]struct{})
	var missing []string
	for _, photo := range list {
		err := a.writeZipEntry(ctx, zw, photo, used)
		if err == nil {
			continue
		}
		if errors.Is(err, errOriginalMissing) {
			log.Printf("photoapi: zip skipping %s: %v", photo.UID, err)
			missing = append(missing, photo.FileName)
			continue
		}
		log.Printf("photoapi: zip writing entry %s: %v", photo.UID, err)
		return
	}
	writeMissingManifest(zw, missing)
	if err := zw.Close(); err != nil {
		log.Printf("photoapi: closing zip: %v", err)
	}
}

// writeZipEntry opens photo's original and copies it into zw under a collision-
// free entry name (see uniqueEntryName). It returns errOriginalMissing when the
// original cannot be opened, so the caller skips and records it; any other error
// means the archive stream is already corrupt and the caller must abandon it.
// Originals are already compressed, so the Store method copies them at IO speed
// instead of wasting CPU deflating incompressible bytes. The reader is always
// closed before returning.
func (a *API) writeZipEntry(
	ctx context.Context, zw *zip.Writer, photo photos.Photo, used map[string]struct{},
) error {
	reader, err := a.storage.Open(ctx, photo.FilePath)
	if err != nil {
		// Wrap both the sentinel (so the caller detects the skip via errors.Is)
		// and the underlying open error (so a missing vs. unreadable original
		// stays diagnosable in the log).
		return fmt.Errorf("%w: %w", errOriginalMissing, err)
	}
	defer func() { _ = reader.Close() }()

	header := &zip.FileHeader{
		Name:   uniqueEntryName(sanitizeEntryName(photo.FileName), used),
		Method: zip.Store,
	}
	if photo.TakenAt != nil {
		header.Modified = *photo.TakenAt
	}
	entry, err := zw.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("photoapi: creating zip header: %w", err)
	}
	if _, err := io.Copy(entry, reader); err != nil {
		return fmt.Errorf("photoapi: copying original into zip: %w", err)
	}
	return nil
}

// writeMissingManifest appends missingManifestName listing the originals skipped
// because they were missing from storage, so a user who asked for "all these
// photos" can see which ones are absent. It writes nothing when none were
// skipped and never fails the archive: a manifest that cannot be written is only
// logged.
func writeMissingManifest(zw *zip.Writer, missing []string) {
	if len(missing) == 0 {
		return
	}
	entry, err := zw.Create(missingManifestName)
	if err != nil {
		log.Printf("photoapi: creating missing manifest: %v", err)
		return
	}
	header := fmt.Sprintf("%d file(s) could not be included because their original was missing:\n\n", len(missing))
	if _, err := io.WriteString(entry, header); err != nil {
		log.Printf("photoapi: writing missing manifest: %v", err)
		return
	}
	for _, name := range missing {
		if _, err := fmt.Fprintln(entry, name); err != nil {
			log.Printf("photoapi: writing missing manifest: %v", err)
			return
		}
	}
}

// sanitizeEntryName reduces a photo's original file name to a safe ZIP entry
// name: just the base name (directory components stripped, so a crafted name can
// neither create folders nor escape the archive) with control characters
// removed. It falls back to "file" when nothing usable remains.
func sanitizeEntryName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}

// uniqueEntryName returns name when it has not been used yet, otherwise the first
// free variant with a " (2)", " (3)", … suffix inserted before the extension (so
// "IMG.jpg" collides to "IMG (2).jpg"), the way a desktop resolves a name clash.
// The chosen name is recorded in used so the next call sees it. name is assumed
// already sanitized and non-empty.
func uniqueEntryName(name string, used map[string]struct{}) string {
	if _, taken := used[name]; !taken {
		used[name] = struct{}{}
		return name
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, taken := used[candidate]; !taken {
			used[candidate] = struct{}{}
			return candidate
		}
	}
}

// zipArchiveName derives the archive's own filename from the request: the
// caller-supplied Name (an album title, say) sanitised and given a .zip
// extension, or a dated default kukatko-photos-<date>.zip built from the
// caller-supplied Date (the server avoids wall-clock here, so the date is the
// client's). It never returns an empty name.
func zipArchiveName(req zipDownloadRequest) string {
	if base := sanitizeArchiveBase(req.Name); base != "" {
		return base + ".zip"
	}
	if date := sanitizeArchiveBase(req.Date); date != "" {
		return "kukatko-photos-" + date + ".zip"
	}
	return "kukatko-photos.zip"
}

// sanitizeArchiveBase trims a caller-supplied archive base name to a single safe
// path segment (directory separators and control characters removed), returning
// "" when nothing usable remains so the caller can fall back to a default.
// contentDisposition strips the remaining quoting-hostile characters.
func sanitizeArchiveBase(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// writeZipTooMany answers a request that would pack more than maxZipFiles
// originals with 413 and a message naming the cap, so the client can tell the
// user to download in smaller batches.
func writeZipTooMany(w http.ResponseWriter, count int) {
	writeError(w, http.StatusRequestEntityTooLarge,
		fmt.Sprintf("too many photos: %d requested, at most %d per download", count, maxZipFiles))
}
