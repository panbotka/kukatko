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

// mapAlbums finds-or-creates a Kukátko album for every PhotoPrism album (by
// title) and attaches the already-imported member photos. A per-album failure is
// logged and skipped; only a listing failure is returned to fail the run.
func (s *Service) mapAlbums(ctx context.Context) error {
	existing, err := s.albums.ListAlbums(ctx)
	if err != nil {
		return fmt.Errorf("ppimport: listing kukatko albums: %w", err)
	}
	byTitle := make(map[string]string, len(existing))
	for _, a := range existing {
		byTitle[a.Title] = a.UID
	}
	for offset := 0; ; {
		page, err := s.client.ListAlbums(ctx, photoprism.ListParams{Count: s.pageSize, Offset: offset})
		if err != nil {
			return fmt.Errorf("ppimport: listing photoprism albums at offset %d: %w", offset, err)
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

// mapLabels finds-or-creates a Kukátko label for every PhotoPrism label (by name)
// and attaches the already-imported tagged photos. A per-label failure is logged
// and skipped; only a listing failure is returned to fail the run.
func (s *Service) mapLabels(ctx context.Context) error {
	existing, err := s.labels.ListLabels(ctx)
	if err != nil {
		return fmt.Errorf("ppimport: listing kukatko labels: %w", err)
	}
	byName := make(map[string]string, len(existing))
	for _, l := range existing {
		byName[l.Name] = l.UID
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
// to a label, preferring the label slug and falling back to its name.
func labelQuery(slug, name string) string {
	term := strings.TrimSpace(slug)
	if term == "" {
		term = strings.TrimSpace(name)
	}
	return fmt.Sprintf("label:%q", term)
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
