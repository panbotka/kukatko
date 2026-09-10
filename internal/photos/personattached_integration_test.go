//go:build integration

package photos_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
)

// TestList_handAttachedPerson verifies which of the marker-backed filters see a
// person attached to a media item by hand — the link that carries no bounding box
// and no detected face.
//
// The split is the whole point of the feature. The person scope and the `person:`
// filter must find the photo, because the person really is on it. The `faces:`
// count must not, because there is no face there: `faces:no` is what a curator
// searches to find the pictures still needing a box drawn, and a hand-attached
// person is precisely a picture where drawing one is impossible.
func TestList_handAttachedPerson(t *testing.T) {
	store, db := newStore(t)
	ppl := people.NewStore(db.Pool())
	ctx := t.Context()

	attached := mustCreate(t, store, photos.Photo{
		FileHash: "hp-1", FilePath: "p/1.jpg", FileName: "1.jpg", FileMime: "image/jpeg",
		Title: "attached",
	})
	detected := mustCreate(t, store, photos.Photo{
		FileHash: "hp-2", FilePath: "p/2.jpg", FileName: "2.jpg", FileMime: "image/jpeg",
		Title: "detected",
	})

	subject, err := ppl.CreateSubject(ctx, people.Subject{Name: "Zdena"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	if _, err := ppl.AttachSubjectToPhoto(ctx, attached.UID, subject.UID, audit.Entry{
		Action: audit.ActionPersonAttach, TargetType: "markers",
	}); err != nil {
		t.Fatalf("AttachSubjectToPhoto: %v", err)
	}
	mustMarker(t, ppl, detected.UID, subject.UID, false)

	t.Run("person scope finds the attached photo", func(t *testing.T) {
		list, err := store.List(ctx, photos.ListParams{SubjectUIDs: []string{subject.UID}})
		if err != nil {
			t.Fatalf("List(person): %v", err)
		}
		set := uidSet(list)
		if len(set) != 2 || !set[attached.UID] || !set[detected.UID] {
			t.Fatalf("person scope = %v, want both photos", set)
		}
	})

	t.Run("person: filter finds the attached photo", func(t *testing.T) {
		parsed := query.Parse("person:Zdena")
		list, err := store.List(ctx, photos.ListParams{QueryFilters: parsed.Filters})
		if err != nil {
			t.Fatalf("List(person:): %v", err)
		}
		if set := uidSet(list); len(set) != 2 || !set[attached.UID] {
			t.Fatalf("person: filter = %v, want both photos", set)
		}
	})

	t.Run("faces: counts only real faces", func(t *testing.T) {
		parsed := query.Parse("faces:no")
		list, err := store.List(ctx, photos.ListParams{QueryFilters: parsed.Filters})
		if err != nil {
			t.Fatalf("List(faces:no): %v", err)
		}
		set := uidSet(list)
		if len(set) != 1 || !set[attached.UID] {
			t.Fatalf("faces:no = %v, want only the hand-attached photo", set)
		}

		parsed = query.Parse("faces:yes")
		list, err = store.List(ctx, photos.ListParams{QueryFilters: parsed.Filters})
		if err != nil {
			t.Fatalf("List(faces:yes): %v", err)
		}
		set = uidSet(list)
		if len(set) != 1 || !set[detected.UID] {
			t.Fatalf("faces:yes = %v, want only the photo with a detected face", set)
		}
	})
}
