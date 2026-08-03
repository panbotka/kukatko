package photoprism

import "context"

// LibraryCounts are the totals PhotoPrism maintains for its own UI and serves in
// the "count" object of GET /api/v1/config. They come from aggregate queries over
// the photos table (photoprism/internal/config/client_config.go), a completely
// different code path from the photo search — which is exactly what makes them
// useful: they are the only way for a caller to tell that the listing it just
// walked is narrower than the library it walked it from.
//
// The population behind All is "indexed, not hidden (photo_quality > -1), not
// archived (deleted_at IS NULL), whose primary file is neither missing nor in
// error", minus private pictures when the session hides them and minus pictures
// in review when that feature is on. So All can legitimately sit BELOW the number
// of photos a full listing yields; a listing that yields FEWER photos than All is
// the direction that means something is wrong.
type LibraryCounts struct {
	// All is Photos + Media + Documents: every picture PhotoPrism counts as part of
	// the library.
	All int `json:"all"`
	// Photos is the still-image count (everything that is not animated, video,
	// live, audio or document).
	Photos int `json:"photos"`
	// Media is Animated + Live + Videos + Audio.
	Media int `json:"media"`
	// Animated, Live, Videos, Audio and Documents are the non-still buckets.
	Animated  int `json:"animated"`
	Live      int `json:"live"`
	Videos    int `json:"videos"`
	Audio     int `json:"audio"`
	Documents int `json:"documents"`
	// Hidden is the number of pictures with photo_quality = -1; they are excluded
	// from All and from the listing alike.
	Hidden int `json:"hidden"`
	// Archived is the number of pictures with deleted_at set. They are excluded
	// from All and are not served by a default listing either.
	Archived int `json:"archived"`
	// Private is the number of private pictures.
	Private int `json:"private"`
	// Review is the number of pictures of low quality (photo_quality 0..2). They
	// are served by the listing, but PhotoPrism subtracts them from All when its
	// review feature is enabled — another reason All is a lower bound.
	Review int `json:"review"`
	// Files is the number of indexed files (a photo may hold several).
	Files int `json:"files"`
}

// Counts returns PhotoPrism's own library totals by reading GET /api/v1/config
// and keeping only its "count" object; the rest of the client configuration
// (including the download and preview tokens it carries) is discarded.
//
// It costs one request and is meant to be called once per reconciliation pass, as
// an independent check on a listing walk — never per page.
func (c *HTTPClient) Counts(ctx context.Context) (LibraryCounts, error) {
	reqURL := c.endpoint("config")
	var payload struct {
		Count LibraryCounts `json:"count"`
	}
	if err := c.getJSON(ctx, reqURL.String(), "config", &payload); err != nil {
		return LibraryCounts{}, err
	}
	return payload.Count, nil
}
