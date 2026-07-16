package mcpapi

import (
	"time"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// The shapes in this file are the whole point of the package's "compact by
// construction" rule. photos.Photo carries some sixty fields including the raw
// EXIF JSONB blob, and every one of them costs the calling agent context. So the
// tools never return it: a list returns photoSummary (four fields), and only
// get_photo returns photoDetail — a curated read of the fields a human would ask
// about, still without the EXIF blob, which is a machine artefact no agent can
// use better than the columns already extracted from it.

// photoSummary is what every list-style tool returns per photo: enough to decide
// which photos are worth a get_photo, and nothing else.
type photoSummary struct {
	// UID identifies the photo in every other tool.
	UID string `json:"uid"`
	// Title is the photo's title; absent when nobody has given it one.
	Title string `json:"title,omitempty"`
	// TakenAt is the capture time (RFC 3339); absent when unknown.
	TakenAt string `json:"taken_at,omitempty"`
	// MediaType is "image", "video" or "live".
	MediaType string `json:"media_type,omitempty"`
	// ThumbURL fetches the grid thumbnail.
	ThumbURL string `json:"thumb_url,omitempty"`
}

// photoPage is a page of photos plus the counters an agent needs to decide
// whether to ask for more: the tools never dump a whole library into a context
// window, so they must always say what they left behind.
type photoPage struct {
	// Photos is this page, in the requested order.
	Photos []photoSummary `json:"photos"`
	// Total is how many photos match in the whole library, ignoring paging.
	Total int `json:"total"`
	// Offset is where this page starts within Total.
	Offset int `json:"offset"`
	// Remaining is how many matches follow this page; raise offset to reach them.
	Remaining int `json:"remaining"`
}

// ref is a uid+name pointer to an album, label or person, used wherever a photo
// names its collections. The uid is what the other tools take; the name is what
// the human said.
type ref struct {
	UID  string `json:"uid"`
	Slug string `json:"slug,omitempty"`
	Name string `json:"name"`
}

// photoDetail is the single-photo read: the curated metadata, plus the albums,
// labels and people the photo belongs to. It never carries the raw EXIF blob.
type photoDetail struct {
	UID         string `json:"uid"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Notes       string `json:"notes,omitempty"`
	// Keywords are the verbatim IPTC keywords from the source file. They are not
	// Kukátko's labels — those are the library's own curated taxonomy.
	Keywords string `json:"keywords,omitempty"`

	// TakenAt is the capture time (RFC 3339); absent when unknown.
	TakenAt string `json:"taken_at,omitempty"`
	// TakenAtEstimated marks TakenAt as a guess rather than a fact, and
	// TakenAtNote records in the owner's own words what the guess rests on.
	TakenAtEstimated bool   `json:"taken_at_estimated,omitempty"`
	TakenAtNote      string `json:"taken_at_note,omitempty"`

	MediaType string `json:"media_type,omitempty"`
	FileName  string `json:"file_name,omitempty"`
	FileSize  int64  `json:"file_size,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	// DurationMs is a video's length in milliseconds, absent for images.
	DurationMs *int `json:"duration_ms,omitempty"`

	Lat *float64 `json:"lat,omitempty"`
	Lng *float64 `json:"lng,omitempty"`
	// LocationSource says where the coordinates came from: "exif", "manual",
	// "estimate" (inferred from photos taken nearby in time) or "" (unknown). An
	// estimate is a guess and must not be reported as a measured location.
	LocationSource string `json:"location_source,omitempty"`

	Camera      string   `json:"camera,omitempty"`
	Lens        string   `json:"lens,omitempty"`
	ISO         *int     `json:"iso,omitempty"`
	Aperture    *float64 `json:"aperture,omitempty"`
	Exposure    string   `json:"exposure,omitempty"`
	FocalLength *float64 `json:"focal_length,omitempty"`

	// Favorite, Rating and Flag are the calling token's own user's opinion of the
	// photo — they are per-user, not library-wide.
	Favorite bool   `json:"favorite"`
	Rating   int    `json:"rating"`
	Flag     string `json:"flag,omitempty"`

	// Archived marks a photo in the trash. Archived photos stay out of search
	// unless asked for, and nothing exposed here can delete them.
	Archived bool `json:"archived"`
	Private  bool `json:"private"`

	// Albums, Labels and People are the collections this photo belongs to.
	Albums []ref `json:"albums"`
	Labels []ref `json:"labels"`
	People []ref `json:"people"`

	ThumbURL    string `json:"thumb_url,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
}

// albumInfo is an album as the tools report it.
type albumInfo struct {
	UID         string `json:"uid"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	// Type is "album" (a hand-made album) or one of the generated kinds
	// ("folder", "moment", "state", "month").
	Type string `json:"type,omitempty"`
	// PhotoCount is how many photos the album holds.
	PhotoCount int  `json:"photo_count"`
	Private    bool `json:"private,omitempty"`
}

// labelInfo is a label as the tools report it.
type labelInfo struct {
	UID        string `json:"uid"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	PhotoCount int    `json:"photo_count"`
}

// subjectInfo is a subject — a person, an animal or another recurring
// character — as the tools report it. The two counters are different questions
// and only one is answered at a time: list_subjects reports FaceCount because
// that is what a listing can count cheaply, and get_subject reports PhotoCount
// because that is what someone asking about one person means. They are not
// interchangeable — one photo can hold several of a person's faces.
type subjectInfo struct {
	UID  string `json:"uid"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	// Type is "person", "animal" or "other".
	Type string `json:"type"`
	// FaceCount is how many recognised faces are assigned to this subject.
	FaceCount int `json:"face_count,omitempty"`
	// PhotoCount is how many photos this subject appears in.
	PhotoCount int    `json:"photo_count,omitempty"`
	Notes      string `json:"notes,omitempty"`
}

// libraryStats answers "how many …" without paging through anything.
type libraryStats struct {
	// Photos is the live library: archived photos are excluded, and the several
	// files of one shot count once.
	Photos int `json:"photos"`
	// Videos is the subset of Photos that are videos rather than stills.
	Videos int `json:"videos"`
	// Archived is how many photos sit in the trash, outside the counts above.
	Archived int `json:"archived"`
	// WithLocation is how many live photos have GPS coordinates.
	WithLocation int `json:"with_location"`
	// Favorites is how many live photos the calling token's own user has
	// favourited; favourites are per-user, not library-wide.
	Favorites int `json:"favorites"`
	Albums    int `json:"albums"`
	Labels    int `json:"labels"`
	// People counts subjects of every kind, animals included.
	People int `json:"people"`
}

// summarize projects photos onto the compact list shape, stamping thumbnail URLs
// on the way out.
func (a *API) summarize(list []photos.Photo) []photoSummary {
	a.media.Decorate(list)
	out := make([]photoSummary, 0, len(list))
	for i := range list {
		out = append(out, photoSummary{
			UID:       list[i].UID,
			Title:     list[i].Title,
			TakenAt:   formatTime(list[i].TakenAt),
			MediaType: string(list[i].MediaType),
			ThumbURL:  list[i].ThumbURL,
		})
	}
	return out
}

// page wraps a summarised result with its paging counters.
func page(list []photoSummary, total, offset int) photoPage {
	// Clamp: an offset past the end, or a count that moved under a concurrent
	// write, must not report a negative number of rows still to come.
	remaining := max(total-offset-len(list), 0)
	return photoPage{Photos: list, Total: total, Offset: offset, Remaining: remaining}
}

// albumRefs projects albums onto the uid+name shape.
func albumRefs(list []organize.Album) []ref {
	out := make([]ref, 0, len(list))
	for _, al := range list {
		out = append(out, ref{UID: al.UID, Slug: al.Slug, Name: al.Title})
	}
	return out
}

// labelRefs projects labels onto the uid+name shape.
func labelRefs(list []organize.Label) []ref {
	out := make([]ref, 0, len(list))
	for _, l := range list {
		out = append(out, ref{UID: l.UID, Slug: l.Slug, Name: l.Name})
	}
	return out
}

// toAlbumInfo projects an album and its photo count onto the reported shape.
func toAlbumInfo(al organize.Album, count int) albumInfo {
	return albumInfo{
		UID:         al.UID,
		Slug:        al.Slug,
		Title:       al.Title,
		Description: al.Description,
		Type:        string(al.Type),
		PhotoCount:  count,
		Private:     al.Private,
	}
}

// toLabelInfo projects a label and its photo count onto the reported shape.
func toLabelInfo(l organize.Label, count int) labelInfo {
	return labelInfo{UID: l.UID, Slug: l.Slug, Name: l.Name, PhotoCount: count}
}

// toSubjectInfo projects a subject onto the reported shape, leaving both
// counters to the caller: only the caller knows which one it can answer.
func toSubjectInfo(s people.Subject) subjectInfo {
	return subjectInfo{
		UID:   s.UID,
		Slug:  s.Slug,
		Name:  s.Name,
		Type:  string(s.Type),
		Notes: s.Notes,
	}
}

// formatTime renders an optional timestamp as RFC 3339, or the empty string when
// it is unknown — an agent reads a missing field more reliably than a zero date.
func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
