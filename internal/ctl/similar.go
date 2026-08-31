package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
)

// ErrInvalidSimilarLimit indicates a neighbourhood size outside what the API
// accepts.
var ErrInvalidSimilarLimit = errors.New("ctl: similar limit must be between 1 and 100")

// maxSimilarLimit is the largest neighbourhood GET /photos/{uid}/similar serves.
// The server clamps silently; the client refuses, so a caller asking for 500
// learns it got 100 rather than believing it saw everything.
const maxSimilarLimit = 100

// SimilarPhoto is one visual neighbour of a photo: the photo row, plus how far it
// is from the source in the embedding space. Distance is a cosine distance, so 0
// is the same image and larger is less alike.
type SimilarPhoto struct {
	Photo
	Distance float64 `json:"distance"`
}

// ListSimilar fetches GET /photos/{uid}/similar and returns the raw JSON body:
// the photos nearest the given one in the embedding space, nearest first, the
// source itself excluded. limit 0 leaves the server's own default.
//
// It is deliberately empty-friendly: a photo the box has not embedded yet — and
// an instance with no embeddings backend at all — answers with an empty list and
// 200, not an error. An empty result therefore means "nothing to compare with"
// as often as it means "nothing alike". A missing photo yields a *StatusError
// with status 404. Decode it with DecodeSimilar.
func (c *Client) ListSimilar(ctx context.Context, photoUID string, limit int) (json.RawMessage, error) {
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	if limit < 0 || limit > maxSimilarLimit {
		return nil, fmt.Errorf("%w: got %d", ErrInvalidSimilarLimit, limit)
	}
	var q url.Values
	if limit > 0 {
		q = url.Values{"limit": []string{strconv.Itoa(limit)}}
	}
	return c.get(ctx, "/photos/"+url.PathEscape(photoUID)+"/similar", q)
}

// DecodeSimilar decodes the bare {"similar": […]} envelope.
func DecodeSimilar(raw json.RawMessage) ([]SimilarPhoto, error) {
	var payload struct {
		Similar []SimilarPhoto `json:"similar"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decoding the similar photos: %w", err)
	}
	return payload.Similar, nil
}

// WriteSimilar renders a photo's visual neighbours as a compact table, nearest
// first, each with its cosine distance — without the distance a neighbour list is
// just a list, and "how alike" is the whole question.
func WriteSimilar(w io.Writer, similar []SimilarPhoto) error {
	if len(similar) == 0 {
		return writeLine(w,
			"no similar photos found (the photo may simply not be embedded yet)")
	}
	rows := make([][]string, 0, len(similar))
	for _, neighbour := range similar {
		rows = append(rows, []string{
			neighbour.UID,
			formatDistance(neighbour.Distance),
			formatTime(neighbour.TakenAt),
			elide(dash(neighbour.Title), titleWidth),
			elide(dash(neighbour.FileName), fileWidth),
		})
	}
	return writeTable(w, []string{"UID", "DISTANCE", "TAKEN", "TITLE", "FILE"}, rows)
}
