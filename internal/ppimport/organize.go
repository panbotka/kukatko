package ppimport

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
)

// mapScope maps the structure of the photos a scoped run imported, without
// walking the whole source catalogue: the named album and the named label, each
// of which resolves its membership from one bounded listing. The other filters
// need no mapping — the people of a --person run are seeded per photo from their
// face markers during the import itself, and a --year run carries no structure
// beyond the capture dates already on the photos.
func (s *Service) mapScope(ctx context.Context, scope Scope) error {
	if scope.AlbumUID != "" {
		if err := s.mapAlbum(ctx, scope.AlbumUID); err != nil {
			return err
		}
	}
	if scope.Label != "" {
		return s.mapLabel(ctx, scope.Label)
	}
	return nil
}

// mapAlbums finds-or-creates a Kukátko album for every PhotoPrism album (by
// title) and attaches the already-imported member photos. A per-album failure is
// logged and skipped; only a listing failure is returned to fail the run.
func (s *Service) mapAlbums(ctx context.Context) error {
	byTitle, err := s.albumsByTitle(ctx)
	if err != nil {
		return err
	}
	// The source rejects an album listing that names no type, so the catalogue is
	// walked one type at a time.
	for _, albumType := range s.albumTypes {
		if err := s.mapAlbumsOfType(ctx, albumType, byTitle); err != nil {
			return err
		}
	}
	return nil
}

// mapAlbumsOfType maps every album of one PhotoPrism album type, paging the
// source listing.
func (s *Service) mapAlbumsOfType(ctx context.Context, albumType string, byTitle map[string]string) error {
	for offset := 0; ; {
		page, err := s.client.ListAlbums(ctx, photoprism.ListParams{
			Count:  s.pageSize,
			Offset: offset,
			Type:   albumType,
		})
		if err != nil {
			return fmt.Errorf("ppimport: listing photoprism %s albums at offset %d: %w", albumType, offset, err)
		}
		for i := range page {
			s.mapOneAlbum(ctx, page[i], byTitle)
		}
		if len(page) < s.pageSize {
			return nil
		}
		offset += len(page)
	}
}

// mapAlbum maps a single source album (identified by its PhotoPrism uid) and
// attaches its members. It is the album-scoped counterpart of mapAlbums: the
// source has no get-album-by-uid endpoint, so the album catalogue is paged until
// the uid is found. An unknown uid is an error — a scoped run that imported
// photos but silently mapped no album would look like a success.
func (s *Service) mapAlbum(ctx context.Context, ppAlbumUID string) error {
	byTitle, err := s.albumsByTitle(ctx)
	if err != nil {
		return err
	}
	// A scoped uid may name an album of any type, so every type is searched — not
	// just the ones a full run maps.
	for _, albumType := range photoprism.AlbumTypes {
		found, err := s.findAlbumOfType(ctx, albumType, ppAlbumUID, byTitle)
		if err != nil {
			return err
		}
		if found {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrAlbumNotFound, ppAlbumUID)
}

// findAlbumOfType pages one album type looking for the uid, mapping it when
// found. It reports whether the album was found.
func (s *Service) findAlbumOfType(
	ctx context.Context, albumType, ppAlbumUID string, byTitle map[string]string,
) (bool, error) {
	for offset := 0; ; {
		page, err := s.client.ListAlbums(ctx, photoprism.ListParams{
			Count:  s.pageSize,
			Offset: offset,
			Type:   albumType,
		})
		if err != nil {
			return false, fmt.Errorf("ppimport: listing photoprism %s albums at offset %d: %w", albumType, offset, err)
		}
		for i := range page {
			if page[i].UID == ppAlbumUID {
				s.mapOneAlbum(ctx, page[i], byTitle)
				return true, nil
			}
		}
		if len(page) < s.pageSize {
			return false, nil
		}
		offset += len(page)
	}
}

// mapOneAlbum finds-or-creates the Kukátko album for a PhotoPrism album and
// attaches its members. byTitle caches title→uid across the run so a created
// album is reused for later references.
func (s *Service) mapOneAlbum(ctx context.Context, a photoprism.Album, byTitle map[string]string) {
	title := strings.TrimSpace(a.Title)
	if title == "" {
		return
	}
	uid, ok := byTitle[title]
	if !ok {
		created, err := s.albums.CreateAlbum(ctx, organize.Album{
			Title:       title,
			Description: a.Description,
			Type:        mapAlbumType(a.Type),
			Private:     a.Private,
		})
		if err != nil {
			s.log.Warn("ppimport: creating album", "title", title, "err", err)
			return
		}
		uid = created.UID
		byTitle[title] = uid
	}
	if err := s.attachAlbumMembers(ctx, a.UID, uid); err != nil {
		s.log.Warn("ppimport: attaching album members", "album", a.UID, "err", err)
	}
}

// attachAlbumMembers pages through a PhotoPrism album's photos and adds every
// one already imported into Kukátko to the mapped album. Kukátko presents
// albums chronologically, so PhotoPrism's listing order is not carried over.
// Members not (yet) imported are skipped; AddPhoto is idempotent.
func (s *Service) attachAlbumMembers(ctx context.Context, ppAlbumUID, albumUID string) error {
	for offset := 0; ; {
		page, err := s.client.ListPhotos(ctx, photoprism.PhotoListParams{
			Count:    s.pageSize,
			Offset:   offset,
			AlbumUID: ppAlbumUID,
		})
		if err != nil {
			return fmt.Errorf("ppimport: listing album %s photos: %w", ppAlbumUID, err)
		}
		for i := range page {
			photo, ok := s.lookupImported(ctx, page[i].UID)
			if !ok {
				continue
			}
			if err := s.albums.AddPhoto(ctx, albumUID, photo.UID); err != nil {
				s.log.Warn("ppimport: adding photo to album", "album", albumUID, "photo", photo.UID, "err", err)
			}
		}
		if len(page) < s.pageSize {
			return nil
		}
		offset += len(page)
	}
}

// albumsByTitle indexes the existing Kukátko albums by title, the key the
// importer finds-or-creates source albums on.
func (s *Service) albumsByTitle(ctx context.Context) (map[string]string, error) {
	existing, err := s.albums.ListAlbums(ctx)
	if err != nil {
		return nil, fmt.Errorf("ppimport: listing kukatko albums: %w", err)
	}
	byTitle := make(map[string]string, len(existing))
	for _, a := range existing {
		byTitle[a.Title] = a.UID
	}
	return byTitle, nil
}

// labelsByName indexes the existing Kukátko labels by name, the key the importer
// finds-or-creates source labels on.
func (s *Service) labelsByName(ctx context.Context) (map[string]string, error) {
	existing, err := s.labels.ListLabels(ctx)
	if err != nil {
		return nil, fmt.Errorf("ppimport: listing kukatko labels: %w", err)
	}
	byName := make(map[string]string, len(existing))
	for _, l := range existing {
		byName[l.Name] = l.UID
	}
	return byName, nil
}

// mapLabels finds-or-creates a Kukátko label for every PhotoPrism label (by name)
// and attaches the already-imported tagged photos. A per-label failure is logged
// and skipped; only a listing failure is returned to fail the run.
func (s *Service) mapLabels(ctx context.Context) error {
	byName, err := s.labelsByName(ctx)
	if err != nil {
		return err
	}
	for offset := 0; ; {
		page, err := s.client.ListLabels(ctx, photoprism.ListParams{Count: s.pageSize, Offset: offset})
		if err != nil {
			return fmt.Errorf("ppimport: listing photoprism labels at offset %d: %w", offset, err)
		}
		for i := range page {
			s.mapOneLabel(ctx, page[i], byName)
		}
		if len(page) < s.pageSize {
			return nil
		}
		offset += len(page)
	}
}

// mapLabel maps a single source label (identified by its slug) and attaches its
// tagged photos. It is the label-scoped counterpart of mapLabels: the source has
// no get-label-by-slug endpoint, so the label catalogue is paged until the slug
// is found — which lists labels, never the whole photo catalogue. An unknown slug
// is an error: a scoped run that imported photos but silently mapped no label
// would look like a success.
func (s *Service) mapLabel(ctx context.Context, slug string) error {
	byName, err := s.labelsByName(ctx)
	if err != nil {
		return err
	}
	for offset := 0; ; {
		page, err := s.client.ListLabels(ctx, photoprism.ListParams{Count: s.pageSize, Offset: offset})
		if err != nil {
			return fmt.Errorf("ppimport: listing photoprism labels at offset %d: %w", offset, err)
		}
		for i := range page {
			if strings.EqualFold(page[i].Slug, slug) {
				s.mapOneLabel(ctx, page[i], byName)
				return nil
			}
		}
		if len(page) < s.pageSize {
			return fmt.Errorf("%w: %s", ErrLabelNotFound, slug)
		}
		offset += len(page)
	}
}

// mapOneLabel finds-or-creates the Kukátko label for a PhotoPrism label and
// attaches its tagged photos. byName caches name→uid across the run.
func (s *Service) mapOneLabel(ctx context.Context, l photoprism.Label, byName map[string]string) {
	name := strings.TrimSpace(l.Name)
	if name == "" {
		return
	}
	uid, ok := byName[name]
	if !ok {
		created, err := s.labels.CreateLabel(ctx, organize.Label{Name: name, Priority: l.Priority})
		if err != nil {
			s.log.Warn("ppimport: creating label", "name", name, "err", err)
			return
		}
		uid = created.UID
		byName[name] = uid
	}
	if err := s.attachLabelMembers(ctx, l.Slug, name, uid); err != nil {
		s.log.Warn("ppimport: attaching label members", "label", name, "err", err)
	}
}

// attachLabelMembers pages through the photos tagged with a PhotoPrism label
// (filtered by label:"<slug>") and attaches the mapped label to every one already
// imported into Kukátko, with import as the source. AttachLabel is idempotent.
func (s *Service) attachLabelMembers(ctx context.Context, ppSlug, name, labelUID string) error {
	for offset := 0; ; {
		page, err := s.client.ListPhotos(ctx, photoprism.PhotoListParams{
			Count:  s.pageSize,
			Offset: offset,
			Query:  labelQuery(ppSlug, name),
		})
		if err != nil {
			return fmt.Errorf("ppimport: listing label %q photos: %w", name, err)
		}
		for i := range page {
			photo, ok := s.lookupImported(ctx, page[i].UID)
			if !ok {
				continue
			}
			if err := s.labels.AttachLabel(ctx, photo.UID, labelUID, organize.SourceImport, 0); err != nil {
				s.log.Warn("ppimport: attaching label", "label", labelUID, "photo", photo.UID, "err", err)
			}
		}
		if len(page) < s.pageSize {
			return nil
		}
		offset += len(page)
	}
}

// lookupImported resolves a PhotoPrism photo UID to its imported Kukátko photo,
// reporting false when it has not been imported (yet) or the lookup errors (which
// is logged), so membership mapping silently skips unknown photos.
func (s *Service) lookupImported(ctx context.Context, ppUID string) (photos.Photo, bool) {
	photo, err := s.photos.GetByPhotoprismUID(ctx, ppUID)
	if errors.Is(err, photos.ErrPhotoNotFound) {
		return photos.Photo{}, false
	}
	if err != nil {
		s.log.Warn("ppimport: resolving imported photo", "pp_uid", ppUID, "err", err)
		return photos.Photo{}, false
	}
	return photo, true
}

// labelQuery builds the PhotoPrism photo-search expression that scopes a listing
// to a label, preferring the label slug and falling back to its name. It renders
// through Scope so a --label run and the membership listing that maps it ask the
// source the very same question.
func labelQuery(slug, name string) string {
	term := strings.TrimSpace(slug)
	if term == "" {
		term = strings.TrimSpace(name)
	}
	return Scope{Label: term}.Query()
}

// mapAlbumType maps a PhotoPrism album type onto Kukátko's album type, defaulting
// an unknown or empty type to a manual album.
func mapAlbumType(ppType string) organize.AlbumType {
	switch strings.ToLower(ppType) {
	case "folder":
		return organize.AlbumFolder
	case "moment":
		return organize.AlbumMoment
	case "month":
		return organize.AlbumMonth
	case "state":
		return organize.AlbumState
	default:
		return organize.AlbumManual
	}
}
