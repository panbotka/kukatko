package globalsearchapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
)

// The photo states a direct hit reports. A photo reached by its id may sit
// outside the default library view; turning up without saying so is what makes
// such a hit confusing ("why is it not in the grid?").
const (
	// stateArchived marks a photo on its way out (archived_at is set).
	stateArchived = "archived"
	// stateHidden marks a photo hidden from the library firehose.
	stateHidden = "hidden"
	// statePrivate marks a private photo.
	statePrivate = "private"
	// stateStackMember marks a non-primary member of a stack — a file the grid
	// folds into its primary.
	stateStackMember = "stack_member"
)

// directHit is the answer to a UID pasted into the search box: what the id
// names, whether it resolved, and what to open. It is deliberately not one of
// the fuzzy groups — an id is an exact reference, so the client presents it as
// "go to this" rather than as one text match among others.
type directHit struct {
	// UID is the id recognised in the query, lowercased.
	UID string `json:"uid"`
	// Kind is what that id itself names: photo, album, label, person, marker,
	// stack or photoprism.
	Kind string `json:"kind"`
	// Found reports whether the id resolved to a row. A well-formed id that
	// matches nothing is answered with false rather than with an empty result
	// set, which would look like a broken search.
	Found bool `json:"found"`
	// TargetKind is the entity the client should open: photo, album, label or
	// person. It differs from Kind for the ids that stand for something else —
	// a marker resolves to the photo it sits on, a stack to its primary photo,
	// a PhotoPrism uid to the catalogue photo holding that source photo.
	TargetKind string `json:"target_kind,omitempty"`
	// TargetUID is that entity's uid.
	TargetUID string `json:"target_uid,omitempty"`
	// Title is a human label for the hit: an album's title, a label's or a
	// person's name, a photo's title or file name. It may be empty for an
	// untitled photo, which the client renders in its own words.
	Title string `json:"title,omitempty"`
	// Photo is the resolved photo row, with its media URLs stamped, when the
	// target is a photo. It lets the client show the thumbnail without a second
	// request.
	Photo *photos.Photo `json:"photo,omitempty"`
	// Cover is the uid of the photo standing for a non-photo target — an album's,
	// a label's or a person's — when it has one.
	Cover *string `json:"cover,omitempty"`
	// ThumbURL is where a client fetches that cover's medallion. It is set
	// together with Cover and never on its own, so a client either has both or
	// draws its own glyph.
	ThumbURL string `json:"thumb_url,omitempty"`
	// States names the non-default states the resolved photo is in (archived,
	// hidden, private, stack_member). It is empty for a photo in the ordinary
	// library view and for every non-photo target.
	States []string `json:"states,omitempty"`
}

// resolveDirect resolves a recognised UID to the entity it names. A miss is not
// an error: it returns a hit with Found false, so the caller can say "no such
// id" plainly. Only a store failure returns an error.
func (a *API) resolveDirect(ctx context.Context, ref query.UIDRef) (*directHit, error) {
	hit := &directHit{UID: ref.UID, Kind: string(ref.Kind)}
	switch ref.Kind {
	case query.EntityAlbum:
		return hit, a.resolveAlbum(ctx, hit)
	case query.EntityLabel:
		return hit, a.resolveLabel(ctx, hit)
	case query.EntityPerson:
		return hit, a.resolvePerson(ctx, hit)
	case query.EntityPhoto, query.EntityMarker, query.EntityStack, query.EntityPhotoprism:
		return hit, a.resolvePhotoRef(ctx, ref, hit)
	default:
		return hit, nil
	}
}

// resolveAlbum fills hit from the album with its uid, leaving Found false when
// there is none. The cover comes from the same batch lookup the fuzzy groups
// use, asked for this one uid, so a pasted id draws the picture a typed name
// would have.
func (a *API) resolveAlbum(ctx context.Context, hit *directHit) error {
	album, err := a.organizer.GetAlbumByUID(ctx, hit.UID)
	if err != nil {
		return skipNotFound(err, organize.ErrAlbumNotFound)
	}
	hit.Found, hit.TargetKind, hit.TargetUID = true, string(query.EntityAlbum), album.UID
	hit.Title = album.Title
	covers, err := a.organizer.AlbumCovers(ctx, []string{album.UID})
	if err != nil {
		return fmt.Errorf("globalsearchapi: reading album cover %s: %w", album.UID, err)
	}
	a.stampDirect(hit, covers[album.UID].PhotoUID, covers[album.UID].FileHash)
	return nil
}

// resolveLabel fills hit from the label with its uid.
func (a *API) resolveLabel(ctx context.Context, hit *directHit) error {
	label, err := a.organizer.GetLabelByUID(ctx, hit.UID)
	if err != nil {
		return skipNotFound(err, organize.ErrLabelNotFound)
	}
	hit.Found, hit.TargetKind, hit.TargetUID = true, string(query.EntityLabel), label.UID
	hit.Title = label.Name
	covers, err := a.organizer.LabelCovers(ctx, []string{label.UID})
	if err != nil {
		return fmt.Errorf("globalsearchapi: reading label cover %s: %w", label.UID, err)
	}
	a.stampDirect(hit, covers[label.UID].PhotoUID, covers[label.UID].FileHash)
	return nil
}

// resolvePerson fills hit from the subject with its uid.
func (a *API) resolvePerson(ctx context.Context, hit *directHit) error {
	subject, err := a.people.GetSubjectByUID(ctx, hit.UID)
	if err != nil {
		return skipNotFound(err, people.ErrSubjectNotFound)
	}
	hit.Found, hit.TargetKind, hit.TargetUID = true, string(query.EntityPerson), subject.UID
	hit.Title = subject.Name
	covers, err := a.people.SubjectCovers(ctx, []string{subject.UID})
	if err != nil {
		return fmt.Errorf("globalsearchapi: reading subject cover %s: %w", subject.UID, err)
	}
	a.stampDirect(hit, covers[subject.UID].PhotoUID, covers[subject.UID].FileHash)
	return nil
}

// stampDirect puts a resolved cover onto a direct hit, doing nothing when the
// entity has none — an absent map entry reads back as the zero cover, and an
// empty photo uid is what says so.
func (a *API) stampDirect(hit *directHit, photoUID, fileHash string) {
	if photoUID == "" {
		return
	}
	hit.Cover = &photoUID
	hit.ThumbURL = a.coverThumb(photoUID, fileHash)
}

// resolvePhotoRef fills hit from whichever photo the id stands for: the photo
// itself, the photo a marker sits on, a stack's primary, or the catalogue row
// holding a PhotoPrism source photo.
func (a *API) resolvePhotoRef(ctx context.Context, ref query.UIDRef, hit *directHit) error {
	photo, found, err := a.lookupPhoto(ctx, ref)
	if err != nil || !found {
		return err
	}
	a.media.DecorateOne(&photo)
	hit.Found, hit.TargetKind, hit.TargetUID = true, string(query.EntityPhoto), photo.UID
	hit.Title, hit.Photo, hit.States = photoTitle(photo), &photo, photoStates(photo)
	return nil
}

// lookupPhoto resolves one photo-ish id to its photo row. found is false when
// nothing carries the id; an error means the store failed.
func (a *API) lookupPhoto(ctx context.Context, ref query.UIDRef) (photos.Photo, bool, error) {
	switch ref.Kind {
	case query.EntityPhoto:
		return getPhoto(a.photos.GetByUID(ctx, ref.UID))
	case query.EntityMarker:
		marker, err := a.people.GetMarkerByUID(ctx, ref.UID)
		if err != nil {
			if errors.Is(err, people.ErrMarkerNotFound) {
				return photos.Photo{}, false, nil
			}
			return photos.Photo{}, false, fmt.Errorf("globalsearchapi: resolving marker %s: %w", ref.UID, err)
		}
		return getPhoto(a.photos.GetByUID(ctx, marker.PhotoUID))
	case query.EntityStack:
		return a.stackPrimary(ctx, ref.UID)
	case query.EntityPhotoprism:
		return a.photoprismPhoto(ctx, ref.UID)
	case query.EntityAlbum, query.EntityLabel, query.EntityPerson:
		return photos.Photo{}, false, fmt.Errorf("globalsearchapi: %s is not a photo reference", ref.Kind)
	default:
		return photos.Photo{}, false, fmt.Errorf("globalsearchapi: unknown uid kind %q", ref.Kind)
	}
}

// stackPrimary returns the primary photo of a stack. ListStackMembers orders the
// primary first, so the first row is it; an empty stack (no photo carries the
// uid) is a miss.
func (a *API) stackPrimary(ctx context.Context, stackUID string) (photos.Photo, bool, error) {
	members, err := a.photos.ListStackMembers(ctx, stackUID)
	if err != nil {
		return photos.Photo{}, false, fmt.Errorf("globalsearchapi: listing stack %s: %w", stackUID, err)
	}
	if len(members) == 0 {
		return photos.Photo{}, false, nil
	}
	return members[0], true, nil
}

// photoprismPhoto resolves a PhotoPrism source uid: first the row imported under
// it, then — for a source photo whose bytes were already catalogued under
// another uid — the alias recorded for it (see migration 0046).
func (a *API) photoprismPhoto(ctx context.Context, ppUID string) (photos.Photo, bool, error) {
	photo, found, err := getPhoto(a.photos.GetByPhotoprismUID(ctx, ppUID))
	if err != nil || found {
		return photo, found, err
	}
	return getPhoto(a.photos.GetByPhotoprismAlias(ctx, ppUID))
}

// getPhoto turns a photo lookup's (photo, error) into the (photo, found, error)
// shape, swallowing ErrPhotoNotFound as a plain miss.
func getPhoto(photo photos.Photo, err error) (photos.Photo, bool, error) {
	if err != nil {
		if errors.Is(err, photos.ErrPhotoNotFound) {
			return photos.Photo{}, false, nil
		}
		return photos.Photo{}, false, err
	}
	return photo, true, nil
}

// skipNotFound swallows the store's not-found sentinel (a miss, not a failure)
// and passes every other error through.
func skipNotFound(err, notFound error) error {
	if errors.Is(err, notFound) {
		return nil
	}
	return err
}

// photoTitle is the human label of a photo hit: its title, else the name it was
// uploaded under, else the stored file name. All three may be empty, in which
// case the client names it itself.
func photoTitle(photo photos.Photo) string {
	for _, candidate := range []string{photo.Title, photo.OriginalName, photo.FileName} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

// photoStates names the states that keep a photo out of the default library
// view, so a hit reached by its id can say why it is not in the grid. It returns
// nil for an ordinary photo.
func photoStates(photo photos.Photo) []string {
	var states []string
	if photo.ArchivedAt != nil {
		states = append(states, stateArchived)
	}
	if photo.HiddenFromLibrary {
		states = append(states, stateHidden)
	}
	if photo.Private {
		states = append(states, statePrivate)
	}
	if photo.StackUID != nil && !photo.StackPrimary {
		states = append(states, stateStackMember)
	}
	return states
}
