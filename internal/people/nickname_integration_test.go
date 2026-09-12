//go:build integration

package people_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
)

// TestSubjectNicknameRoundTrip stores a nickname, reads it back through every
// path that returns a subject, and then clears it — the update rewrites the whole
// editable set, so an empty nickname has to mean "forget it" rather than "leave it
// alone". The slug must not move with it: it is UNIQUE and lives in URLs.
func TestSubjectNicknameRoundTrip(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := t.Context()

	created, err := store.CreateSubject(ctx, people.Subject{
		Name: "Bohumil Nečas", Nickname: "Bohouš",
	})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	if created.Nickname != "Bohouš" {
		t.Fatalf("created nickname = %q, want Bohouš", created.Nickname)
	}
	if created.Slug != "bohumil-necas" {
		t.Fatalf("created slug = %q, want it derived from the name alone", created.Slug)
	}

	got, err := store.GetSubjectByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetSubjectByUID: %v", err)
	}
	if got.Nickname != "Bohouš" {
		t.Errorf("read-back nickname = %q, want Bohouš", got.Nickname)
	}

	listed, err := store.ListSubjects(ctx)
	if err != nil {
		t.Fatalf("ListSubjects: %v", err)
	}
	if len(listed) != 1 || listed[0].Nickname != "Bohouš" {
		t.Errorf("listed = %+v, want the nickname on the people index row", listed)
	}

	changed, err := store.UpdateSubject(ctx, created.UID, people.SubjectUpdate{
		Name: "Bohumil Nečas", Nickname: "Bohoušek", Type: people.SubjectPerson,
	})
	if err != nil {
		t.Fatalf("UpdateSubject: %v", err)
	}
	if changed.Nickname != "Bohoušek" {
		t.Errorf("updated nickname = %q, want Bohoušek", changed.Nickname)
	}
	if changed.Slug != created.Slug {
		t.Errorf("slug moved to %q on a nickname edit, want %q kept", changed.Slug, created.Slug)
	}

	cleared, err := store.UpdateSubject(ctx, created.UID, people.SubjectUpdate{
		Name: "Bohumil Nečas", Type: people.SubjectPerson,
	})
	if err != nil {
		t.Fatalf("UpdateSubject clearing: %v", err)
	}
	if cleared.Nickname != "" {
		t.Errorf("nickname after an update that omits it = %q, want cleared", cleared.Nickname)
	}
}

// TestSearchSubjectsByNickname searches the real database for a nickname, with
// and without its diacritics, and pins down the trap the OR in the predicate
// creates: a subject whose nickname is empty must not answer to an unrelated
// query. Everything else in the library has an empty nickname, so getting that
// wrong would make every search return everybody.
func TestSearchSubjectsByNickname(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := t.Context()

	for _, subj := range []people.Subject{
		{Name: "Bohumil Nečas", Nickname: "Bohouš"},
		{Name: "Anna Nováková"},
		{Name: "Marie Dvořáková", Nickname: "Máňa"},
	} {
		if _, err := store.CreateSubject(ctx, subj); err != nil {
			t.Fatalf("CreateSubject %q: %v", subj.Name, err)
		}
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "the nickname as written", query: "Bohouš", want: []string{"Bohumil Nečas"}},
		{name: "the nickname without diacritics", query: "bohous", want: []string{"Bohumil Nečas"}},
		{name: "the nickname in upper case", query: "MANA", want: []string{"Marie Dvořáková"}},
		{name: "part of a nickname", query: "ouš", want: []string{"Bohumil Nečas"}},
		{name: "the name still matches", query: "novak", want: []string{"Anna Nováková"}},
		{name: "a query matching nobody matches nobody", query: "pepa", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, err := store.SearchSubjects(ctx, tt.query, 10)
			if err != nil {
				t.Fatalf("SearchSubjects(%q): %v", tt.query, err)
			}
			got := subjectNames(found)
			if len(got) != len(tt.want) {
				t.Fatalf("SearchSubjects(%q) = %v, want %v", tt.query, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("SearchSubjects(%q) = %v, want %v", tt.query, got, tt.want)
				}
			}
		})
	}
}

// TestSubjectNicknameInSidecar verifies the nickname reaches the marker rows the
// metadata sidecar is written from. The sidecar is what the catalogue survives on
// after losing the database, and a nickname only the database holds would be lost
// with it.
func TestSubjectNicknameInSidecar(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()

	subject, err := store.CreateSubject(ctx, people.Subject{
		Name: "Bohumil Nečas", Nickname: "Bohouš",
	})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	photoUID := makePhoto(t, photoStore, "hash-nickname")
	if _, err := store.CreateMarker(ctx, people.Marker{
		PhotoUID: photoUID, SubjectUID: &subject.UID, Type: people.MarkerFace,
		X: 0.1, Y: 0.1, W: 0.2, H: 0.2,
	}); err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}

	markers, err := store.ListMarkersWithSubjects(ctx, photoUID)
	if err != nil {
		t.Fatalf("ListMarkersWithSubjects: %v", err)
	}
	if len(markers) != 1 {
		t.Fatalf("sidecar markers = %+v, want one", markers)
	}
	if markers[0].SubjectName != "Bohumil Nečas" || markers[0].SubjectNickname != "Bohouš" {
		t.Fatalf("sidecar marker = %+v, want the name and the nickname", markers[0])
	}
}

// TestSubjectNicknameUnassignedMarkerHasNoNickname checks the other half of that
// join: a marker naming nobody carries an empty nickname rather than a NULL that
// would fail the scan. Unnamed faces are the common row in a growing library.
func TestSubjectNicknameUnassignedMarkerHasNoNickname(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()

	photoUID := makePhoto(t, photoStore, "hash-nickname-unassigned")
	if _, err := store.CreateMarker(ctx, people.Marker{
		PhotoUID: photoUID, Type: people.MarkerFace, X: 0.1, Y: 0.1, W: 0.2, H: 0.2,
	}); err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}

	markers, err := store.ListMarkersWithSubjects(ctx, photoUID)
	if err != nil {
		t.Fatalf("ListMarkersWithSubjects: %v", err)
	}
	if len(markers) != 1 {
		t.Fatalf("sidecar markers = %+v, want one", markers)
	}
	if markers[0].SubjectName != "" || markers[0].SubjectNickname != "" {
		t.Fatalf("unassigned marker = %+v, want no name and no nickname", markers[0])
	}
}

// TestAuditSubjectNicknameChange writes a nickname edit the way the API does —
// the old→new diff stamped into the audit entry's details — and reads the row
// back out of the real audit_log, so the change survives the JSON column.
//
// A nickname is the field most likely to be "corrected" by somebody who never
// met the person, which is exactly why the trail has to name it: the audit diff
// is the only record of what the nickname used to be.
func TestAuditSubjectNicknameChange(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeUser(t, db, "usr_nick", "nick")

	created, err := store.CreateSubject(ctx, people.Subject{
		Name: "Bohumil Nečas", Nickname: "Bohouš",
	})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}

	upd := people.SubjectUpdate{
		Name: created.Name, Nickname: "Bohoušek", Type: people.SubjectPerson,
	}
	details := map[string]any{"name": upd.Name, "type": string(upd.Type)}
	changes := audit.NewChangeSet()
	changes.Add("name", created.Name, upd.Name)
	changes.Add("nickname", created.Nickname, upd.Nickname)
	changes.StampInto(details)

	if _, err := store.UpdateSubjectAudited(ctx, created.UID, upd,
		actorEntry(actor, audit.ActionSubjectUpdate, "subjects", created.UID, details)); err != nil {
		t.Fatalf("UpdateSubjectAudited: %v", err)
	}

	rec := requireOneAudit(t, ctx, auditStore, audit.ActionSubjectUpdate, actor, created.UID)
	recorded, ok := rec.Details[audit.ChangesKey].(map[string]any)
	if !ok {
		t.Fatalf("audit details = %v, want a changes map", rec.Details)
	}
	nickname, ok := recorded["nickname"].(map[string]any)
	if !ok {
		t.Fatalf("changes = %v, want a nickname entry", recorded)
	}
	if nickname["old"] != "Bohouš" || nickname["new"] != "Bohoušek" {
		t.Errorf("nickname change = %v, want Bohouš→Bohoušek", nickname)
	}
	if _, unchanged := recorded["name"]; unchanged {
		t.Errorf("changes = %v, want no entry for the unchanged name", recorded)
	}
}
