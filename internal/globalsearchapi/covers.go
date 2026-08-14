package globalsearchapi

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/thumb"
)

// An album, a label and a person are all *pictures* to the reader, and the
// palette drew all three as the same grey glyph. This file gives every entity
// hit the one thing that tells them apart at a glance: the photo behind it.
//
// The cover itself is derived by the stores in a single query per group (see
// organize.Store.AlbumCovers / LabelCovers and people.Store.SubjectCovers) —
// eight rows cost one query, not eight — and what is added here is only the
// address the browser fetches it from, minted the same way a photo hit's
// thumb_url is: through internal/mediaurl, so a published bucket yields a signed
// edge URL and everything else falls back to this application's own thumb route.
//
// The size is thumb.AvatarSize rather than the grid size a photo hit carries: a
// palette row's medallion is a couple of dozen pixels across, and there can be
// two dozen of them in one dropdown.

// entityHit is a hit that knows its own entity uid, so one helper can collect a
// whole group's uids for a batch cover lookup.
type entityHit interface {
	entityUID() string
}

// entityUID returns the album's uid.
func (h albumHit) entityUID() string { return h.UID }

// entityUID returns the label's uid.
func (h labelHit) entityUID() string { return h.UID }

// entityUID returns the subject's uid.
func (h subjectHit) entityUID() string { return h.UID }

// hitUIDs collects the entity uids of a group of hits, which is what every batch
// cover lookup takes.
func hitUIDs[T entityHit](hits []T) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.entityUID())
	}
	return out
}

// stampAlbumCovers fills Cover and ThumbURL on every album hit that has a cover,
// from one batched lookup. An album with none is left as it is: the client draws
// its own glyph rather than a gap.
func (a *API) stampAlbumCovers(ctx context.Context, hits []albumHit) error {
	covers, err := a.organizer.AlbumCovers(ctx, hitUIDs(hits))
	if err != nil {
		return fmt.Errorf("globalsearchapi: reading album covers: %w", err)
	}
	for i := range hits {
		if cover, ok := covers[hits[i].UID]; ok {
			hits[i].Cover = &cover.PhotoUID
			hits[i].ThumbURL = a.coverThumb(cover.PhotoUID, cover.FileHash)
		}
	}
	return nil
}

// stampLabelCovers fills Cover and ThumbURL on every label hit that has one.
func (a *API) stampLabelCovers(ctx context.Context, hits []labelHit) error {
	covers, err := a.organizer.LabelCovers(ctx, hitUIDs(hits))
	if err != nil {
		return fmt.Errorf("globalsearchapi: reading label covers: %w", err)
	}
	for i := range hits {
		if cover, ok := covers[hits[i].UID]; ok {
			hits[i].Cover = &cover.PhotoUID
			hits[i].ThumbURL = a.coverThumb(cover.PhotoUID, cover.FileHash)
		}
	}
	return nil
}

// stampSubjectCovers fills Cover and ThumbURL on every person hit that has one.
func (a *API) stampSubjectCovers(ctx context.Context, hits []subjectHit) error {
	covers, err := a.people.SubjectCovers(ctx, hitUIDs(hits))
	if err != nil {
		return fmt.Errorf("globalsearchapi: reading subject covers: %w", err)
	}
	for i := range hits {
		if cover, ok := covers[hits[i].UID]; ok {
			hits[i].Cover = &cover.PhotoUID
			hits[i].ThumbURL = a.coverThumb(cover.PhotoUID, cover.FileHash)
		}
	}
	return nil
}

// coverThumb is where a client fetches an entity cover's medallion: the signed
// edge URL when the storage backend publishes one, otherwise this application's
// own thumb route.
func (a *API) coverThumb(photoUID, fileHash string) string {
	return a.media.Thumb(photoUID, fileHash, thumb.AvatarSize)
}
