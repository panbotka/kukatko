package mcpapi

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
	"github.com/panbotka/kukatko/internal/vectors"
)

// queryLanguageHelp documents the search filter language inline in the tool
// description. It is long on purpose: the agent cannot discover the keys any
// other way, and a filter it does not know about becomes a free-text word that
// quietly matches nothing.
const queryLanguageHelp = `Free words match the title, description, notes and file name (Czech-aware and ` +
	`diacritics-insensitive, so "babicka" finds "babička"). key:value tokens filter; several tokens AND ` +
	`together; -word and -key:value exclude. Keys: person:, album:, label: (each by name or uid), ` +
	`year:, month:, day:, taken:, before:, after: (dates as YYYY, YYYY-MM or YYYY-MM-DD; year: and the ` +
	`other numbers take ranges like year:1960-1969), country:, city:, geo:yes|no, near:<photo-uid>, ` +
	`dist:<km>, camera:, lens:, iso:, f:, mm:, mp:, type:image|video|live, faces:yes|no or a count, ` +
	`face:new, favorite:yes|no, rating:0-5, flag:pick|reject|eye, archived:yes|no, private:yes|no, ` +
	`portrait:, landscape:, square:, panorama:, filename:, keywords:. ` +
	`favorite:, rating: and flag: mean the calling token's own user. ` +
	`Example: person:babicka year:1960-1969 -album:dovolena`

// searchPhotosIn is the search tool's argument set. It is deliberately small:
// Query carries the expressive power, and the uid scopes exist because an agent
// that already resolved a name to a uid wants an exact match rather than the
// query language's name-or-uid matching.
//
// The jsonschema tags below are what the agent actually reads to pick an
// argument, and a struct tag is one unwrappable token: trimming them to fit the
// column budget would degrade the interface to satisfy a formatter. Hence the
// exemption here and on the other argument structs.
//
//nolint:lll // jsonschema tags are unwrappable and are the agent-facing interface.
type searchPhotosIn struct {
	Query     string `json:"query,omitempty" jsonschema:"Search query: free text and/or key:value filters. Empty returns the whole library, newest first."`
	AlbumUID  string `json:"album_uid,omitempty" jsonschema:"Restrict to photos in this album (exact uid from list_albums)."`
	LabelUID  string `json:"label_uid,omitempty" jsonschema:"Restrict to photos carrying this label (exact uid from list_labels)."`
	PersonUID string `json:"person_uid,omitempty" jsonschema:"Restrict to photos showing this person or animal (exact uid from list_subjects)."`
	Sort      string `json:"sort,omitempty" jsonschema:"Order by: taken_at (capture time), added (when it entered the library), title, size or rating. Defaults to relevance when the query has free text, otherwise taken_at."`
	Order     string `json:"order,omitempty" jsonschema:"asc or desc. Defaults to desc (newest first)."`
	Limit     int    `json:"limit,omitempty" jsonschema:"How many photos to return. Keep it small; the default is usually right."`
	Offset    int    `json:"offset,omitempty" jsonschema:"Skip this many matches; use with the 'remaining' counter to page."`
}

// getPhotoIn identifies one photo.
type getPhotoIn struct {
	UID string `json:"uid" jsonschema:"The photo's uid, as returned by search_photos."`
}

// findSimilarIn identifies the photo to search around.
type findSimilarIn struct {
	UID   string `json:"uid" jsonschema:"The photo to find look-alikes of."`
	Limit int    `json:"limit,omitempty" jsonschema:"How many similar photos to return."`
}

// similarPhoto is a neighbour plus how close it is.
type similarPhoto struct {
	photoSummary
	// Distance is the cosine distance from the source photo: 0 is identical and
	// larger is less alike. Below ~0.15 the photos are usually near-duplicates.
	Distance float64 `json:"distance"`
}

// similarResult is what find_similar_photos returns.
type similarResult struct {
	Photos []similarPhoto `json:"photos"`
}

// registerSearchTools adds the tools that read photos: finding them, reading one,
// finding look-alikes, and counting the library.
func (a *API) registerSearchTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "search_photos",
		Description: "Find photos in the library. This is the main entry point: start here. " +
			"Returns a compact summary per photo (uid, title, capture date, thumbnail URL) plus " +
			"the total number of matches and how many remain after this page — it never returns " +
			"full metadata, so follow up with get_photo for the photos you actually care about.\n\n" +
			queryLanguageHelp,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, a.handleSearchPhotos)

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_photo",
		Description: "Read one photo in full: title, description, notes, capture date, location, " +
			"camera settings, the calling user's favourite/rating, and the albums, labels and people " +
			"it belongs to. Use it after search_photos, one photo at a time.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, a.handleGetPhoto)

	mcp.AddTool(s, &mcp.Tool{
		Name: "find_similar_photos",
		Description: "Find the photos that look most like a given photo, by visual similarity rather " +
			"than by metadata. Useful for spotting near-duplicates and for finding the rest of a scene " +
			"someone only tagged once. Returns each neighbour with its distance (0 = identical). " +
			"Requires image embeddings; on a library without them it says so.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, a.handleFindSimilar)

	mcp.AddTool(s, &mcp.Tool{
		Name: "library_stats",
		Description: "Count the library in one call: live photos, of which videos, archived photos, " +
			"photos with GPS, the calling user's favourites, and the number of albums, labels and " +
			"people. Use it to answer \"how many …\" instead of paging through search_photos.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, a.handleLibraryStats)
}

// handleSearchPhotos runs the search and returns one compact page.
func (a *API) handleSearchPhotos(
	ctx context.Context, _ *mcp.CallToolRequest, in searchPhotosIn,
) (*mcp.CallToolResult, photoPage, error) {
	c, err := callerFromContext(ctx)
	if err != nil {
		return nil, photoPage{}, err
	}
	params, ranked, err := a.searchParams(c, in)
	if err != nil {
		return nil, photoPage{}, err
	}

	total, err := a.photos.Count(ctx, params)
	if err != nil {
		return nil, photoPage{}, fmt.Errorf("counting matches: %w", err)
	}
	list, err := a.runSearch(ctx, params, ranked)
	if err != nil {
		return nil, photoPage{}, err
	}
	return nil, page(a.summarize(list), total, params.Offset), nil
}

// runSearch picks the ranked full-text path or the plain list path. Free text
// with no explicit sort ranks by relevance, which is what an agent asking a
// question wants; anything else keeps the requested order.
func (a *API) runSearch(ctx context.Context, params photos.ListParams, ranked bool) ([]photos.Photo, error) {
	var (
		list []photos.Photo
		err  error
	)
	if ranked {
		list, err = a.photos.Search(ctx, params)
	} else {
		list, err = a.photos.List(ctx, params)
	}
	if err != nil {
		return nil, fmt.Errorf("searching photos: %w", err)
	}
	return list, nil
}

// searchParams compiles the tool's arguments into store parameters, reporting
// whether the ranked full-text path applies.
func (a *API) searchParams(c caller, in searchPhotosIn) (photos.ListParams, bool, error) {
	// Cap the query the same way the HTTP list/search endpoints do, so this
	// equivalent path cannot pack tens of thousands of '|'-alternatives into one
	// token and force an arbitrarily expensive scan.
	if len(in.Query) > query.MaxLength {
		return photos.ListParams{}, false,
			fmt.Errorf("query is too long: %d characters exceed the limit of %d", len(in.Query), query.MaxLength)
	}
	parsed := query.Parse(in.Query)
	if n := parsed.Complexity(); n > query.MaxComplexity {
		return photos.ListParams{}, false,
			fmt.Errorf("query is too complex: %d conditions exceed the limit of %d", n, query.MaxComplexity)
	}
	params := photos.ListParams{
		QueryFilters: parsed.Filters,
		// Scope the query language's per-user filters (favorite:, rating:, flag:)
		// and the rating sort to the token's own user, exactly as the web UI does.
		RatedBy: &c.user.UID,
		Limit:   a.clampLimit(in.Limit),
		Offset:  max(in.Offset, 0),
	}
	appendUID(&params.AlbumUIDs, in.AlbumUID)
	appendUID(&params.LabelUIDs, in.LabelUID)
	appendUID(&params.SubjectUIDs, in.PersonUID)

	if err := applySort(&params, in.Sort, in.Order); err != nil {
		return photos.ListParams{}, false, err
	}

	// The free text becomes a full-text query, matched the same Czech-aware way
	// the web UI matches it. It only drives the ranking — and so only takes the
	// ranked path — when the caller has not asked for an order of their own.
	free := strings.TrimSpace(parsed.FreeText())
	if free == "" {
		return params, false, nil
	}
	params.FullText = free
	return params, in.Sort == "", nil
}

// appendUID adds uid to dst when it is set, so an unset scope adds no filter.
func appendUID(dst *[]string, uid string) {
	if uid = strings.TrimSpace(uid); uid != "" {
		*dst = append(*dst, uid)
	}
}

// sortFields maps the tool's sort argument onto the store's sort fields. The
// names are the agent's vocabulary, not the column names.
var sortFields = map[string]photos.SortField{
	"taken_at": photos.SortByTakenAt,
	"added":    photos.SortByCreatedAt,
	"title":    photos.SortByTitle,
	"size":     photos.SortBySize,
	"rating":   photos.SortByRating,
}

// applySort validates and applies the sort and order arguments. An unknown value
// is an error naming the alternatives rather than a silent fallback: an agent
// that gets sorting wrong reports the wrong answer with full confidence.
func applySort(params *photos.ListParams, sort, order string) error {
	if sort != "" {
		field, ok := sortFields[sort]
		if !ok {
			keys := slices.Sorted(maps.Keys(sortFields))
			return fmt.Errorf("unknown sort %q (want one of: %s)", sort, strings.Join(keys, ", "))
		}
		params.Sort = field
	}
	switch order {
	case "":
	case "asc":
		params.Order = photos.OrderAsc
	case "desc":
		params.Order = photos.OrderDesc
	default:
		return fmt.Errorf("unknown order %q (want asc or desc)", order)
	}
	return nil
}

// handleGetPhoto reads one photo with its collections.
func (a *API) handleGetPhoto(
	ctx context.Context, _ *mcp.CallToolRequest, in getPhotoIn,
) (*mcp.CallToolResult, photoDetail, error) {
	c, err := callerFromContext(ctx)
	if err != nil {
		return nil, photoDetail{}, err
	}
	uid := strings.TrimSpace(in.UID)
	photo, err := a.photos.GetByUID(ctx, uid)
	if err != nil {
		if errors.Is(err, photos.ErrPhotoNotFound) {
			return nil, photoDetail{}, fmt.Errorf("no photo with uid %q", uid)
		}
		return nil, photoDetail{}, fmt.Errorf("fetching photo: %w", err)
	}
	detail, err := a.describePhoto(ctx, c, photo)
	if err != nil {
		return nil, photoDetail{}, err
	}
	return nil, detail, nil
}

// describePhoto assembles a photo's detail shape: the curated columns, the
// caller's own opinion of it, and the collections it belongs to.
func (a *API) describePhoto(ctx context.Context, c caller, photo photos.Photo) (photoDetail, error) {
	albums, err := a.organize.AlbumsForPhoto(ctx, photo.UID)
	if err != nil {
		return photoDetail{}, fmt.Errorf("fetching the photo's albums: %w", err)
	}
	labels, err := a.organize.LabelsForPhoto(ctx, photo.UID)
	if err != nil {
		return photoDetail{}, fmt.Errorf("fetching the photo's labels: %w", err)
	}
	subjects, err := a.subjectsForPhoto(ctx, photo.UID)
	if err != nil {
		return photoDetail{}, err
	}
	favorite, err := a.organize.IsFavorite(ctx, c.user.UID, photo.UID)
	if err != nil {
		return photoDetail{}, fmt.Errorf("fetching the favourite flag: %w", err)
	}
	rating, err := a.organize.GetRating(ctx, c.user.UID, photo.UID)
	if err != nil {
		return photoDetail{}, fmt.Errorf("fetching the rating: %w", err)
	}
	a.media.DecorateOne(&photo)

	detail := toPhotoDetail(photo)
	detail.Favorite = favorite
	detail.Rating = rating.Rating
	detail.Flag = rating.Flag
	detail.Albums = albumRefs(albums)
	detail.Labels = labelRefs(labels)
	detail.People = subjects
	return detail, nil
}

// subjectsForPhoto resolves the photo's accepted face markers into the people
// they name. Rejected markers and unnamed faces are not people the photo shows,
// so they are left out.
func (a *API) subjectsForPhoto(ctx context.Context, photoUID string) ([]ref, error) {
	markers, err := a.people.ListMarkersByPhoto(ctx, photoUID)
	if err != nil {
		return nil, fmt.Errorf("fetching the photo's faces: %w", err)
	}
	out := make([]ref, 0, len(markers))
	seen := make(map[string]struct{}, len(markers))
	for _, m := range markers {
		if m.Invalid || m.SubjectUID == nil {
			continue
		}
		if _, dup := seen[*m.SubjectUID]; dup {
			continue
		}
		seen[*m.SubjectUID] = struct{}{}
		subj, err := a.people.GetSubjectByUID(ctx, *m.SubjectUID)
		if err != nil {
			if errors.Is(err, people.ErrSubjectNotFound) {
				continue
			}
			return nil, fmt.Errorf("fetching a subject on the photo: %w", err)
		}
		out = append(out, ref{UID: subj.UID, Slug: subj.Slug, Name: subj.Name})
	}
	return out, nil
}

// handleFindSimilar returns the nearest visual neighbours of a photo.
func (a *API) handleFindSimilar(
	ctx context.Context, _ *mcp.CallToolRequest, in findSimilarIn,
) (*mcp.CallToolResult, similarResult, error) {
	if a.similar == nil {
		return nil, similarResult{}, errors.New(
			"similarity search is not available on this library: it has no image embeddings",
		)
	}
	uid := strings.TrimSpace(in.UID)
	if _, err := a.photos.GetByUID(ctx, uid); err != nil {
		if errors.Is(err, photos.ErrPhotoNotFound) {
			return nil, similarResult{}, fmt.Errorf("no photo with uid %q", uid)
		}
		return nil, similarResult{}, fmt.Errorf("fetching photo: %w", err)
	}
	emb, err := a.similar.GetEmbedding(ctx, uid)
	if err != nil {
		if errors.Is(err, vectors.ErrEmbeddingNotFound) {
			// The photo is real but not yet embedded — an empty result is the
			// truth here, not an error.
			return nil, similarResult{Photos: []similarPhoto{}}, nil
		}
		return nil, similarResult{}, fmt.Errorf("fetching the photo's embedding: %w", err)
	}
	limit := a.clampLimit(in.Limit)
	// Ask for one extra: a photo is always its own nearest neighbour.
	matches, err := a.similar.FindSimilar(ctx, emb.Vector, limit+1, 0)
	if err != nil {
		return nil, similarResult{}, fmt.Errorf("searching for similar photos: %w", err)
	}
	out, err := a.resolveSimilar(ctx, uid, limit, matches)
	if err != nil {
		return nil, similarResult{}, err
	}
	return nil, similarResult{Photos: out}, nil
}

// resolveSimilar loads the matched photos and projects them onto the compact
// shape, dropping the source photo and keeping the distance order.
func (a *API) resolveSimilar(
	ctx context.Context, sourceUID string, limit int, matches []vectors.Match,
) ([]similarPhoto, error) {
	uids := make([]string, 0, len(matches))
	dist := make(map[string]float64, len(matches))
	for _, m := range matches {
		if m.PhotoUID == sourceUID || len(uids) >= limit {
			continue
		}
		uids = append(uids, m.PhotoUID)
		dist[m.PhotoUID] = m.Distance
	}
	if len(uids) == 0 {
		return []similarPhoto{}, nil
	}
	list, err := a.photos.ListByUIDs(ctx, uids)
	if err != nil {
		return nil, fmt.Errorf("loading similar photos: %w", err)
	}
	byUID := make(map[string]photoSummary, len(list))
	for _, sum := range a.summarize(list) {
		byUID[sum.UID] = sum
	}
	out := make([]similarPhoto, 0, len(uids))
	for _, uid := range uids {
		sum, ok := byUID[uid]
		if !ok {
			continue
		}
		out = append(out, similarPhoto{photoSummary: sum, Distance: dist[uid]})
	}
	return out, nil
}

// handleLibraryStats counts the library.
func (a *API) handleLibraryStats(
	ctx context.Context, _ *mcp.CallToolRequest, _ struct{},
) (*mcp.CallToolResult, libraryStats, error) {
	c, err := callerFromContext(ctx)
	if err != nil {
		return nil, libraryStats{}, err
	}
	stats, err := a.countPhotos(ctx, c)
	if err != nil {
		return nil, libraryStats{}, err
	}
	albums, err := a.organize.ListAlbums(ctx)
	if err != nil {
		return nil, libraryStats{}, fmt.Errorf("counting albums: %w", err)
	}
	labels, err := a.organize.ListLabels(ctx)
	if err != nil {
		return nil, libraryStats{}, fmt.Errorf("counting labels: %w", err)
	}
	subjects, err := a.people.ListSubjects(ctx)
	if err != nil {
		return nil, libraryStats{}, fmt.Errorf("counting people: %w", err)
	}
	stats.Albums = len(albums)
	stats.Labels = len(labels)
	stats.People = len(subjects)
	return nil, stats, nil
}

// countPhotos runs the photo-side counters of library_stats.
func (a *API) countPhotos(ctx context.Context, c caller) (libraryStats, error) {
	hasGPS := true
	counts := []struct {
		into   *int
		what   string
		params photos.ListParams
	}{
		{what: "photos", params: photos.ListParams{}},
		{what: "videos", params: photos.ListParams{QueryFilters: typeFilter("video")}},
		{what: "archived photos", params: photos.ListParams{OnlyArchived: true}},
		{what: "located photos", params: photos.ListParams{HasGPS: &hasGPS}},
		{what: "favourites", params: photos.ListParams{FavoriteOf: c.user.UID}},
	}
	var stats libraryStats
	counts[0].into = &stats.Photos
	counts[1].into = &stats.Videos
	counts[2].into = &stats.Archived
	counts[3].into = &stats.WithLocation
	counts[4].into = &stats.Favorites

	for _, counter := range counts {
		n, err := a.photos.Count(ctx, counter.params)
		if err != nil {
			return libraryStats{}, fmt.Errorf("counting %s: %w", counter.what, err)
		}
		*counter.into = n
	}
	return stats, nil
}

// typeFilter builds the media-type condition of the search query language, so the
// stats counter and a type:video search agree by construction.
func typeFilter(mediaType string) []query.Filter {
	return []query.Filter{{Key: query.KeyType, Values: []query.Value{{Text: mediaType}}}}
}

// clampLimit bounds a requested page size: unset falls back to the configured
// default, and anything larger than the configured cap is trimmed to it rather
// than refused, because a too-large page is the agent overreaching, not an error.
func (a *API) clampLimit(n int) int {
	if n <= 0 {
		return a.pageSize
	}
	return min(n, a.maxPageSize)
}

// toPhotoDetail projects a photo onto the detail shape. It is an allow-list, not
// a copy: the raw EXIF blob and the machine bookkeeping columns are left behind
// deliberately.
func toPhotoDetail(p photos.Photo) photoDetail {
	return photoDetail{
		UID:              p.UID,
		Title:            p.Title,
		Description:      p.Description,
		Notes:            p.Notes,
		Keywords:         p.Keywords,
		TakenAt:          formatTime(p.TakenAt),
		TakenAtEstimated: p.TakenAtEstimated,
		TakenAtNote:      p.TakenAtNote,
		MediaType:        string(p.MediaType),
		FileName:         p.FileName,
		FileSize:         p.FileSize,
		Width:            p.FileWidth,
		Height:           p.FileHeight,
		DurationMs:       p.DurationMs,
		Lat:              p.Lat,
		Lng:              p.Lng,
		LocationSource:   p.LocationSource,
		Camera:           strings.TrimSpace(p.CameraMake + " " + p.CameraModel),
		Lens:             p.LensModel,
		ISO:              p.ISO,
		Aperture:         p.Aperture,
		Exposure:         p.Exposure,
		FocalLength:      p.FocalLength,
		Archived:         p.ArchivedAt != nil,
		Private:          p.Private,
		ThumbURL:         p.ThumbURL,
		DownloadURL:      p.DownloadURL,
	}
}
