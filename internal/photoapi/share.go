package photoapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/photos"
)

// maxShareFiles caps how many photos one share manifest may describe. It is
// deliberately the ZIP download's cap: both answer the same question — "how much
// may one gesture ask for?" — and a selection over it is told so before anything
// is prepared. The client then hands the files to the phone in batches of its own
// (a share sheet cannot take a thousand files at once), so this cap is the outer
// bound on a whole share, not on one handoff.
const maxShareFiles = maxZipFiles

// shareManifestRequest is the JSON body of the share-manifest endpoint: the
// photos, in the client's selection order, that are about to be handed to the
// phone's share sheet. A UID with no matching photo is silently skipped, as with
// the single download and the ZIP.
type shareManifestRequest struct {
	PhotoUIDs []string `json:"photo_uids"`
}

// shareManifestFile describes one file the client is to fetch and hand over. It
// is deliberately not a URL: the client already knows how to address a photo's
// original and its cached previews (and must be able to retry a preview at a
// smaller size), so the manifest states the *facts* of the file and leaves the
// addressing to the caller.
type shareManifestFile struct {
	// UID identifies the photo whose bytes to fetch.
	UID string `json:"uid"`
	// Name is the file name to hand over, de-duplicated within one manifest so two
	// photos called IMG_0001.jpg do not arrive under one name. A preview carries a
	// .jpg name, because that is what its bytes are.
	Name string `json:"name"`
	// Mime is the type to label the file with — the original's stored type, or
	// image/jpeg for a preview.
	Mime string `json:"mime"`
	// Size is how many bytes to budget for this file when splitting a selection
	// into batches a phone can hold. For a preview it is the *original's* size,
	// which is an upper bound (a JPEG preview is always smaller than the RAW it
	// came from): over-estimating only makes a batch smaller, while
	// under-estimating would let a batch outgrow the phone's memory.
	Size int64 `json:"size"`
	// Preview asks the client to fetch the photo's largest cached JPEG preview
	// instead of the original. Set for a RAW original: a phone library handles a
	// CR2 badly or not at all, and the point of sharing is a picture that lands in
	// Fotky.
	Preview bool `json:"preview"`
}

// shareManifestResponse is the JSON body of the share-manifest endpoint.
type shareManifestResponse struct {
	Files []shareManifestFile `json:"files"`
}

// handleShareManifest answers what a selection looks like as files, so the page
// can fetch them and hand them to the phone's own share sheet (whence iOS offers
// "Save Images" into Apple Photos and Android offers Google Photos).
//
// It exists because the page holds only UIDs — a selection survives scrolling, so
// the photo rows behind it may long have been evicted from the grid's window —
// while the batching it must do needs each file's name, type and size. One request
// answers for the whole selection instead of one detail fetch per photo.
//
// It authorises exactly like the single-photo download (the requireDownload
// guard): naming a file is a step towards fetching it, and the fetch itself goes
// through that same guard. A request naming no resolvable photo is answered with
// 400, one over maxShareFiles with 413, neither having read anything.
func (a *API) handleShareManifest(w http.ResponseWriter, r *http.Request) {
	var req shareManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.PhotoUIDs) > maxShareFiles {
		writeShareTooMany(w, len(req.PhotoUIDs))
		return
	}
	if len(req.PhotoUIDs) == 0 {
		writeError(w, http.StatusBadRequest, "no photos to share")
		return
	}
	list, err := a.store.ListByUIDs(r.Context(), req.PhotoUIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "collecting photos failed")
		return
	}
	ordered := orderByUIDs(req.PhotoUIDs, list)
	if len(ordered) == 0 {
		writeError(w, http.StatusBadRequest, "no photos to share")
		return
	}
	writeJSON(w, http.StatusOK, shareManifestResponse{Files: shareManifestFiles(ordered)})
}

// shareManifestFiles maps photos to their manifest entries, in order, giving each
// one a name no other entry in the same manifest carries. The names go through the
// ZIP's own sanitising and collision rules — a shared file lands in someone's
// photo library under this name, so it must be a plain base name, and two files
// arriving as one name is exactly the confusion " (2)" prevents.
func shareManifestFiles(list []photos.Photo) []shareManifestFile {
	out := make([]shareManifestFile, 0, len(list))
	used := make(map[string]struct{}, len(list))
	for _, photo := range list {
		preview := imgconvert.IsRAWName(photo.FileName)
		name := sanitizeEntryName(photo.FileName)
		if preview {
			name = jpegFileName(name)
		}
		out = append(out, shareManifestFile{
			UID:     photo.UID,
			Name:    uniqueEntryName(name, used),
			Mime:    shareMime(photo, preview),
			Size:    photo.FileSize,
			Preview: preview,
		})
	}
	return out
}

// shareMime is the type to label a shared file with: image/jpeg for a preview
// (whatever the RAW original's stored type says), the photo's own type otherwise,
// falling back to the generic binary type for a row that never recorded one — a
// File with an empty type is one a share sheet may refuse outright.
func shareMime(photo photos.Photo, preview bool) string {
	if preview {
		return "image/jpeg"
	}
	if photo.FileMime == "" {
		return "application/octet-stream"
	}
	return photo.FileMime
}

// writeShareTooMany answers a selection larger than maxShareFiles with 413 and a
// message naming the cap, so the client can tell the user to share fewer at once.
func writeShareTooMany(w http.ResponseWriter, count int) {
	writeError(w, http.StatusRequestEntityTooLarge,
		fmt.Sprintf("too many photos: %d requested, at most %d per share", count, maxShareFiles))
}
