package system

import (
	"context"
	"errors"
	"fmt"
)

// ProjectLibrary is one active project's library counts: what the Prometheus
// collector exports with a project label. Library carries the same raw counts
// and derived gaps the instance-wide LibraryStats does, minus the ones only
// the statistics page reads: the hidden and stacked-away photos, the photos
// with coordinates and the assigned faces stay zero.
type ProjectLibrary struct {
	// UID identifies the project.
	UID string
	// Slug is the project's URL name — the label value, never its name, because
	// /metrics is unauthenticated.
	Slug string
	// Library is the project's counts, derived values filled in.
	Library Library
	// VideosWithoutStreaming is how many of its browsable videos have no
	// encoded streaming rendition — the per-project share of Video.Missing.
	VideosWithoutStreaming int
}

// ProjectLibraryCounter supplies the per-project library counts. It is
// satisfied by *Store.
type ProjectLibraryCounter interface {
	// CountLibraryByProject returns the raw counts of every active project.
	CountLibraryByProject(ctx context.Context) ([]ProjectLibrary, error)
}

// errNoProjectLibrary is returned by LibraryByProject when no counter is wired.
var errNoProjectLibrary = errors.New("system: no per-project library counter configured")

// LibraryByProject returns the library counts of every active project, ordered
// by slug, with their derived gaps filled in. It is not memoised here: its one
// reader, the Prometheus collector, memoises the whole snapshot itself. A
// failure is an error, never an empty list — no projects and an unavailable
// catalogue must not look alike.
func (s *Service) LibraryByProject(ctx context.Context) ([]ProjectLibrary, error) {
	if s.projectLibrary == nil {
		return nil, errNoProjectLibrary
	}
	rows, err := s.projectLibrary.CountLibraryByProject(ctx)
	if err != nil {
		return nil, fmt.Errorf("system: counting the library per project: %w", err)
	}
	for i := range rows {
		rows[i].Library = rows[i].Library.derive()
	}
	return rows, nil
}

// countLibraryByProjectSQL is countLibrarySQL grouped by project, restricted to
// what the metrics export: one row per active project. Each table is grouped
// once in its own CTE and joined onto the projects, so a project with no rows
// in one of them reads zero there rather than vanishing. markers and
// photo_places carry no project of their own and take their photo's. The
// streaming backlog is a browsable video with no rendition, the same predicate
// as the dashboard's Video.Missing, so the per-project numbers add up to it.
const countLibraryByProjectSQL = `
WITH ph AS (
    SELECT p.project_id,
           count(*)                                                    AS photos,
           count(*) FILTER (WHERE p.media_type = 'video')              AS videos,
           count(*) FILTER (WHERE p.media_type = 'live')               AS live,
           count(*) FILTER (WHERE p.archived_at IS NOT NULL)           AS archived,
           count(*) FILTER (WHERE EXISTS (SELECT 1 FROM embeddings e WHERE e.photo_uid = p.uid))
                                                                       AS with_embedding,
           count(*) FILTER (WHERE EXISTS (SELECT 1 FROM faces f WHERE f.photo_uid = p.uid))
                                                                       AS with_faces,
           count(*) FILTER (WHERE p.archived_at IS NULL AND p.lat IS NOT NULL AND p.lng IS NOT NULL
                              AND NOT EXISTS (SELECT 1 FROM photo_places pp WHERE pp.photo_uid = p.uid))
                                                                       AS pending_geocode,
           count(*) FILTER (WHERE p.archived_at IS NULL AND p.media_type = 'video'
                              AND NOT EXISTS (SELECT 1 FROM photo_hls_renditions r WHERE r.photo_uid = p.uid))
                                                                       AS videos_no_stream
    FROM photos p
    GROUP BY p.project_id
), geocoded AS (
    SELECT p.project_id, count(*) AS n
    FROM photo_places pp JOIN photos p ON p.uid = pp.photo_uid
    WHERE pp.lat IS NOT NULL AND pp.lng IS NOT NULL
    GROUP BY p.project_id
), emb AS (
    SELECT project_id, count(*) AS n FROM embeddings GROUP BY project_id
), fc AS (
    SELECT project_id, count(*) AS n FROM faces GROUP BY project_id
), sb AS (
    SELECT project_id,
           count(*) FILTER (WHERE type = 'person') AS person,
           count(*) FILTER (WHERE type = 'pet')    AS pet,
           count(*) FILTER (WHERE type = 'other')  AS other
    FROM subjects GROUP BY project_id
), mk AS (
    SELECT p.project_id, count(*) AS total, count(*) FILTER (WHERE m.subject_uid IS NOT NULL) AS assigned
    FROM markers m JOIN photos p ON p.uid = m.photo_uid
    GROUP BY p.project_id
), al AS (
    SELECT project_id,
           count(*) FILTER (WHERE type = 'album')  AS manual,
           count(*) FILTER (WHERE type = 'folder') AS folder,
           count(*) FILTER (WHERE type = 'moment') AS moment,
           count(*) FILTER (WHERE type = 'state')  AS state,
           count(*) FILTER (WHERE type = 'month')  AS month
    FROM albums GROUP BY project_id
), lb AS (
    SELECT project_id, count(*) AS n FROM labels GROUP BY project_id
)
SELECT pr.uid, pr.slug,
       coalesce(ph.photos, 0), coalesce(ph.videos, 0), coalesce(ph.live, 0), coalesce(ph.archived, 0),
       coalesce(ph.with_embedding, 0), coalesce(ph.with_faces, 0), coalesce(geocoded.n, 0),
       coalesce(ph.pending_geocode, 0), coalesce(emb.n, 0), coalesce(fc.n, 0),
       coalesce(sb.person, 0), coalesce(sb.pet, 0), coalesce(sb.other, 0),
       coalesce(mk.total, 0), coalesce(mk.assigned, 0),
       coalesce(al.manual, 0), coalesce(al.folder, 0), coalesce(al.moment, 0), coalesce(al.state, 0),
       coalesce(al.month, 0), coalesce(lb.n, 0), coalesce(ph.videos_no_stream, 0)
FROM projects pr
LEFT JOIN ph       ON ph.project_id = pr.uid
LEFT JOIN geocoded ON geocoded.project_id = pr.uid
LEFT JOIN emb      ON emb.project_id = pr.uid
LEFT JOIN fc       ON fc.project_id = pr.uid
LEFT JOIN sb       ON sb.project_id = pr.uid
LEFT JOIN mk       ON mk.project_id = pr.uid
LEFT JOIN al       ON al.project_id = pr.uid
LEFT JOIN lb       ON lb.project_id = pr.uid
WHERE pr.active
ORDER BY pr.slug`

// CountLibraryByProject returns the raw counts of every active project in one
// query, ordered by slug. The derived values are left zero — Service fills them
// in — and so are the counts only the statistics page needs (hidden, stacked,
// with coordinates, assigned faces). An error means the query failed; callers
// must not treat it as an instance without projects.
func (s *Store) CountLibraryByProject(ctx context.Context) ([]ProjectLibrary, error) {
	rows, err := s.pool.Query(ctx, countLibraryByProjectSQL)
	if err != nil {
		return nil, fmt.Errorf("system: counting the library per project: %w", err)
	}
	defer rows.Close()
	var out []ProjectLibrary
	for rows.Next() {
		var p ProjectLibrary
		l := &p.Library
		if err := rows.Scan(&p.UID, &p.Slug,
			&l.Photos, &l.Videos, &l.LivePhotos, &l.PhotosArchived,
			&l.PhotosWithEmbedding, &l.PhotosWithFaces, &l.PhotosGeocoded,
			&l.PhotosPendingGeocode, &l.Embeddings, &l.Faces,
			&l.SubjectsPerson, &l.SubjectsPet, &l.SubjectsOther,
			&l.Markers, &l.MarkersAssigned,
			&l.AlbumsManual, &l.AlbumsFolder, &l.AlbumsMoment, &l.AlbumsState,
			&l.AlbumsMonth, &l.Labels, &p.VideosWithoutStreaming,
		); err != nil {
			return nil, fmt.Errorf("system: scanning a project's library counts: %w", err)
		}
		l.Subjects = l.SubjectsPerson + l.SubjectsPet + l.SubjectsOther
		l.Albums = l.AlbumsManual + l.AlbumsFolder + l.AlbumsMoment + l.AlbumsState + l.AlbumsMonth
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("system: reading the per-project library counts: %w", err)
	}
	return out, nil
}
