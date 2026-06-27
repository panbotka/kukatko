package psimport

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photosorter"
)

// mappings translates photo-sorter catalogue UIDs onto their find-or-created
// Kukátko counterparts, so per-photo satellites (faces, markers, album/label
// membership) can reference Kukátko's own identifiers.
type mappings struct {
	// subjects maps a photo-sorter subject uid to a Kukátko subject uid.
	subjects map[string]string
	// albums maps a photo-sorter album uid to a Kukátko album uid.
	albums map[string]string
	// labels maps a photo-sorter label uid to a Kukátko label uid.
	labels map[string]string
}

// buildMappings find-or-creates a Kukátko subject, album and label for every
// photo-sorter one (by slug derived from the name/title) and returns the UID
// translation maps. It is run once per migration pass before the photo loop. A
// catalogue listing or creation failure aborts the migration (the maps must be
// complete for membership to map correctly).
func (s *Service) buildMappings(ctx context.Context) (mappings, error) {
	subjects, err := s.mapSubjects(ctx)
	if err != nil {
		return mappings{}, err
	}
	albums, err := s.mapAlbums(ctx)
	if err != nil {
		return mappings{}, err
	}
	labels, err := s.mapLabels(ctx)
	if err != nil {
		return mappings{}, err
	}
	return mappings{subjects: subjects, albums: albums, labels: labels}, nil
}

// mapPaged pages a photo-sorter catalogue and find-or-creates a Kukátko
// counterpart for each item, returning the ps-uid → kk-uid map. list fetches one
// page at the given offset; handle returns the (psUID, kkUID) pair for one item
// (an empty kkUID is skipped). It is the shared paging skeleton behind the
// subject, album and label mappings.
func mapPaged[T any](
	pageSize int,
	list func(offset int) ([]T, error),
	handle func(item T) (string, string, error),
) (map[string]string, error) {
	out := make(map[string]string)
	for offset := 0; ; {
		page, err := list(offset)
		if err != nil {
			return nil, err
		}
		for i := range page {
			psUID, kkUID, err := handle(page[i])
			if err != nil {
				return nil, err
			}
			if kkUID != "" {
				out[psUID] = kkUID
			}
		}
		if len(page) < pageSize {
			return out, nil
		}
		offset += len(page)
	}
}

// mapSubjects pages photo-sorter subjects and find-or-creates each as a Kukátko
// subject, returning the ps-uid → kk-uid map.
func (s *Service) mapSubjects(ctx context.Context) (map[string]string, error) {
	return mapPaged(s.pageSize,
		func(offset int) ([]photosorter.Subject, error) {
			page, err := s.src.ListSubjects(ctx, photosorter.ListParams{Limit: s.pageSize, Offset: offset})
			if err != nil {
				return nil, fmt.Errorf("psimport: listing photo-sorter subjects: %w", err)
			}
			return page, nil
		},
		func(ps photosorter.Subject) (string, string, error) {
			subj, err := s.findOrCreateSubject(ctx, ps)
			if err != nil {
				return "", "", err
			}
			return ps.UID, subj.UID, nil
		})
}

// findOrCreateSubject returns the existing Kukátko subject whose slug matches the
// photo-sorter subject's name, or creates a new one preserving its type and
// flags.
func (s *Service) findOrCreateSubject(ctx context.Context, ps photosorter.Subject) (people.Subject, error) {
	slug := people.Slugify(ps.Name)
	subject, err := s.people.GetSubjectBySlug(ctx, slug)
	if err == nil {
		return subject, nil
	}
	if !errors.Is(err, people.ErrSubjectNotFound) {
		return people.Subject{}, fmt.Errorf("psimport: looking up subject %q: %w", ps.Name, err)
	}
	created, err := s.people.CreateSubject(ctx, people.Subject{
		Name:     ps.Name,
		Type:     mapSubjectType(ps.Type),
		Favorite: ps.Favorite,
		Private:  ps.Private,
		Notes:    ps.Notes,
	})
	if err != nil {
		return people.Subject{}, fmt.Errorf("psimport: creating subject %q: %w", ps.Name, err)
	}
	return created, nil
}

// mapCatalogue builds the ps-uid → kk-uid map for a catalogue (albums or
// labels): it indexes the existing Kukátko entries by their match key, then
// pages the photo-sorter side find-or-creating a Kukátko counterpart for each
// item. label names the catalogue in error messages; listExisting loads the
// already-present Kukátko entries; keyUID extracts (matchKey, kkUID) from one;
// listPS is the photo-sorter pager; psUID and findOrCreate handle one item. It
// is the shared skeleton behind mapAlbums and mapLabels, parameterised over
// their differing row types so neither carries duplicated paging/indexing logic.
func mapCatalogue[E, P any](
	ctx context.Context,
	pageSize int,
	label string,
	listExisting func(context.Context) ([]E, error),
	keyUID func(E) (string, string),
	listPS func(context.Context, photosorter.ListParams) ([]P, error),
	psUID func(P) string,
	findOrCreate func(context.Context, P, map[string]string) (string, error),
) (map[string]string, error) {
	existing, err := listExisting(ctx)
	if err != nil {
		return nil, fmt.Errorf("psimport: listing kukatko %s: %w", label, err)
	}
	index := make(map[string]string, len(existing))
	for i := range existing {
		key, uid := keyUID(existing[i])
		index[key] = uid
	}
	page := func(offset int) ([]P, error) {
		items, listErr := listPS(ctx, photosorter.ListParams{Limit: pageSize, Offset: offset})
		if listErr != nil {
			return nil, fmt.Errorf("psimport: listing photo-sorter %s: %w", label, listErr)
		}
		return items, nil
	}
	return mapPaged(pageSize, page, func(ps P) (string, string, error) {
		uid, createErr := findOrCreate(ctx, ps, index)
		return psUID(ps), uid, createErr
	})
}

// mapAlbums find-or-creates a Kukátko album (by title) for every photo-sorter
// album and returns the ps-uid → kk-uid map. Existing Kukátko albums are matched
// by title so a re-run reuses them instead of duplicating.
func (s *Service) mapAlbums(ctx context.Context) (map[string]string, error) {
	return mapCatalogue(ctx, s.pageSize, "albums",
		s.albums.ListAlbums,
		func(a organize.AlbumCount) (string, string) { return a.Title, a.UID },
		s.src.ListAlbums,
		func(ps photosorter.Album) string { return ps.UID },
		s.findOrCreateAlbum)
}

// findOrCreateAlbum returns the Kukátko album uid for a photo-sorter album,
// creating it (and caching it in byTitle) when absent. A blank title maps to no
// album.
func (s *Service) findOrCreateAlbum(
	ctx context.Context, ps photosorter.Album, byTitle map[string]string,
) (string, error) {
	title := strings.TrimSpace(ps.Title)
	if title == "" {
		return "", nil
	}
	if uid, ok := byTitle[title]; ok {
		return uid, nil
	}
	created, err := s.albums.CreateAlbum(ctx, organize.Album{
		Title:       title,
		Description: ps.Description,
		Type:        mapAlbumType(ps.Type),
		Private:     ps.Private,
	})
	if err != nil {
		return "", fmt.Errorf("psimport: creating album %q: %w", title, err)
	}
	byTitle[title] = created.UID
	return created.UID, nil
}

// mapLabels find-or-creates a Kukátko label (by name) for every photo-sorter
// label and returns the ps-uid → kk-uid map.
func (s *Service) mapLabels(ctx context.Context) (map[string]string, error) {
	return mapCatalogue(ctx, s.pageSize, "labels",
		s.labels.ListLabels,
		func(l organize.LabelCount) (string, string) { return l.Name, l.UID },
		s.src.ListLabels,
		func(ps photosorter.Label) string { return ps.UID },
		s.findOrCreateLabel)
}

// findOrCreateLabel returns the Kukátko label uid for a photo-sorter label,
// creating it (and caching it in byName) when absent. A blank name maps to no
// label.
func (s *Service) findOrCreateLabel(
	ctx context.Context, ps photosorter.Label, byName map[string]string,
) (string, error) {
	name := strings.TrimSpace(ps.Name)
	if name == "" {
		return "", nil
	}
	if uid, ok := byName[name]; ok {
		return uid, nil
	}
	created, err := s.labels.CreateLabel(ctx, organize.Label{Name: name, Priority: ps.Priority})
	if err != nil {
		return "", fmt.Errorf("psimport: creating label %q: %w", name, err)
	}
	byName[name] = created.UID
	return created.UID, nil
}
