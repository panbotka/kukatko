package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ErrUnknownRebuild indicates a rebuild name outside RebuildSpecs, caught
// client-side so a typo costs no round trip.
var ErrUnknownRebuild = errors.New("ctl: unknown rebuild")

// The per-photo computations a rebuild can redo. The name is the CLI's, chosen to
// read as the thing being rebuilt rather than as the endpoint's verb; Path is the
// endpoint that does it.
const (
	// RebuildThumbnail re-renders every cached thumbnail size and the perceptual
	// hashes over the ones already on disk.
	RebuildThumbnail = "thumbnail"
	// RebuildEmbedding recomputes the CLIP image embedding behind semantic search
	// and "similar photos".
	RebuildEmbedding = "embedding"
	// RebuildFaces runs face detection again and replaces the stored faces.
	RebuildFaces = "faces"
	// RebuildPlace resolves the photo's coordinates again, replacing the cached
	// place. It costs a mapy.com credit.
	RebuildPlace = "place"
)

// RebuildStatusQueued is the status a rebuild answers with when its backing
// service was unreachable: nothing ran here, but the forced job is in the queue.
const RebuildStatusQueued = "queued"

// RebuildSpec describes one rebuild: what it redoes and the endpoint that does
// it. The four are listed here rather than derived, because the paths predate the
// idea of a set — `regenerate-thumbnail` was the first and the other three were
// named after it.
type RebuildSpec struct {
	// Name is the CLI's name for the computation (RebuildThumbnail and friends).
	Name string
	// Path is the endpoint's last path segment under /photos/{uid}/.
	Path string
	// Short is the one-line description of what rebuilding it does.
	Short string
}

// RebuildSpecs is every rebuild the CLI offers, in the order the pipeline reaches
// them: what the file renders as, what it means, who is on it, where it is.
var RebuildSpecs = []RebuildSpec{
	{
		Name:  RebuildThumbnail,
		Path:  "regenerate-thumbnail",
		Short: "Re-render every cached thumbnail size and the perceptual hashes",
	},
	{
		Name:  RebuildEmbedding,
		Path:  "reembed",
		Short: "Recompute the image embedding behind semantic search and similar photos",
	},
	{
		Name:  RebuildFaces,
		Path:  "redetect-faces",
		Short: "Run face detection again and replace the stored faces",
	},
	{
		Name:  RebuildPlace,
		Path:  "regeocode",
		Short: "Resolve the coordinates again (costs a mapy.com credit)",
	},
}

// RebuildSpecFor returns the spec named by name, or ok=false when there is no
// such rebuild.
func RebuildSpecFor(name string) (RebuildSpec, bool) {
	for _, spec := range RebuildSpecs {
		if spec.Name == name {
			return spec, true
		}
	}
	return RebuildSpec{}, false
}

// PhotoRebuild is what a rebuild endpoint answers with: which computation ran,
// how it ended, and whatever the computation itself produced — the regenerated
// thumbnail sizes, or how many faces the photo has now.
//
// The four endpoints do not share a body (regenerate-thumbnail predates the
// others and answers with sizes and its own status word), so this is the union of
// what they say, filled from whichever fields the response carried plus the
// requested step.
type PhotoRebuild struct {
	Step   string   `json:"step,omitempty"`
	Status string   `json:"status"`
	Faces  *int     `json:"faces,omitempty"`
	Sizes  []string `json:"sizes,omitempty"`
}

// RebuildPhoto redoes one per-photo computation for one photo and returns the
// server's raw JSON body, so `-o json` prints its own bytes. Decode it with
// DecodePhotoRebuild.
//
// This is the *rebuild*, not the repair: `ctl photos process` (and
// POST /photos/{uid}/process/{step}) schedules the ordinary job, which skips a
// photo that already has the data and therefore answers 200 having changed
// nothing at all. A rebuild discards what is stored and computes it again, which
// is what a photo whose derived data is wrong — rather than missing — needs. It
// needs the maintainer role.
//
// A rebuild whose backing service is offline answers with status "queued": the
// forced job is in the queue and will run when the box comes back.
func (c *Client) RebuildPhoto(ctx context.Context, photoUID, name string) (json.RawMessage, error) {
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	spec, ok := RebuildSpecFor(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownRebuild, name)
	}
	return c.send(ctx, http.MethodPost, "/photos/"+url.PathEscape(photoUID)+"/"+spec.Path, nil)
}

// DecodePhotoRebuild decodes a rebuild response and stamps name onto it, since
// the thumbnail endpoint — older than the idea — does not name the step it
// rebuilt.
func DecodePhotoRebuild(raw json.RawMessage, name string) (PhotoRebuild, error) {
	var rebuild PhotoRebuild
	if err := json.Unmarshal(raw, &rebuild); err != nil {
		return PhotoRebuild{}, fmt.Errorf("decoding the rebuild result: %w", err)
	}
	if rebuild.Step == "" {
		rebuild.Step = name
	}
	return rebuild, nil
}

// WritePhotoRebuild renders a rebuild as one line: what was rebuilt, what came of
// it, and the result the computation produced where it has one — the face count
// or the regenerated sizes. Reporting the result rather than a bare "ok" is the
// point: a rebuild exists because the previous answer was wrong, so the new one
// is what the operator came for.
func WritePhotoRebuild(w io.Writer, rebuild PhotoRebuild) error {
	return writeLine(w, rebuild.Step+": "+describeRebuild(rebuild))
}

// describeRebuild spells out a rebuild's outcome in prose.
func describeRebuild(rebuild PhotoRebuild) string {
	if rebuild.Status == RebuildStatusQueued {
		return "the service is offline; a forced job is queued and will run when it is back"
	}
	line := "rebuilt"
	if rebuild.Faces != nil {
		line += ", " + strconv.Itoa(*rebuild.Faces) + " " + pluralFaces(*rebuild.Faces) + " on the photo"
	}
	if len(rebuild.Sizes) > 0 {
		line += ", " + strconv.Itoa(len(rebuild.Sizes)) + " sizes: " + strings.Join(rebuild.Sizes, ", ")
	}
	return line
}

// pluralFaces picks the noun for a face count, so a one-face photo does not read
// as "1 faces".
func pluralFaces(count int) string {
	if count == 1 {
		return "face"
	}
	return "faces"
}
