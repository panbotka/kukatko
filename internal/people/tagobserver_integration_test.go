//go:build integration

package people_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
)

// tagEvent is one call a recordingObserver heard.
type tagEvent struct {
	kind string
	tag  people.Tagging
}

// recordingObserver remembers every tagging it hears and can be told to fail,
// which is how a test proves the report runs inside the assignment's
// transaction.
type recordingObserver struct {
	events []tagEvent
	fail   error
}

// Tagged records a tagging, or fails with o.fail.
func (o *recordingObserver) Tagged(_ context.Context, _ pgx.Tx, t people.Tagging) error {
	if o.fail != nil {
		return o.fail
	}
	o.events = append(o.events, tagEvent{kind: "tagged", tag: t})
	return nil
}

// Untagged records an untagging, or fails with o.fail.
func (o *recordingObserver) Untagged(_ context.Context, _ pgx.Tx, t people.Tagging) error {
	if o.fail != nil {
		return o.fail
	}
	o.events = append(o.events, tagEvent{kind: "untagged", tag: t})
	return nil
}

// take returns the events heard so far and forgets them.
func (o *recordingObserver) take() []tagEvent {
	out := o.events
	o.events = nil
	return out
}

// TestTagObserver_hearsEveryMarkerChange walks one marker through every write
// that puts a person on a photo or takes them off, and checks the observer
// hears each — with the acting account where the path is audited — and hears
// nothing for a re-assignment to the same person.
func TestTagObserver_hearsEveryMarkerChange(t *testing.T) {
	base, photoStore, _, db := newStores(t)
	ctx := t.Context()
	// The audit trail's actor is a foreign key, so the acting account must exist.
	if _, err := db.Pool().Exec(ctx, `INSERT INTO users (uid, username, email, password_hash, role)
		VALUES ('us-actor', 'actor', 'actor@example.test', 'x', 'editor')`); err != nil {
		t.Fatalf("seeding the actor: %v", err)
	}
	observer := &recordingObserver{}
	store := base.WithTagObserver(observer)

	photoUID := makePhoto(t, photoStore, "tagobs")
	anna := makeSubject(t, store, "Anna")
	bert := makeSubject(t, store, "Bert")
	entry := audit.Entry{Action: audit.ActionFaceAssign, TargetType: "markers", ActorUID: "us-actor"}

	marker, err := store.CreateMarkerAudited(ctx, people.Marker{
		PhotoUID: photoUID, SubjectUID: &anna.UID, W: 0.1, H: 0.1,
	}, entry)
	if err != nil {
		t.Fatalf("CreateMarkerAudited: %v", err)
	}
	onPhoto := func(kind, subject, actor string) tagEvent {
		return tagEvent{kind: kind, tag: people.Tagging{PhotoUID: photoUID, SubjectUID: subject, ActorUID: actor}}
	}
	steps := []struct {
		name string
		run  func() error
		want []tagEvent
	}{
		{name: "create with a subject", run: func() error { return nil },
			want: []tagEvent{onPhoto("tagged", anna.UID, "us-actor")}},
		{name: "re-assign to the same subject", run: func() error {
			_, err := store.AssignSubjectAudited(ctx, marker.UID, anna.UID, entry)
			return err
		}},
		{name: "re-assign to another subject", run: func() error {
			_, err := store.AssignSubjectAudited(ctx, marker.UID, bert.UID, entry)
			return err
		}, want: []tagEvent{onPhoto("untagged", anna.UID, "us-actor"), onPhoto("tagged", bert.UID, "us-actor")}},
		{name: "unassign", run: func() error {
			_, err := store.UnassignSubjectAudited(ctx, marker.UID, entry)
			return err
		}, want: []tagEvent{onPhoto("untagged", bert.UID, "us-actor")}},
		{name: "plain assign has no actor", run: func() error {
			_, err := store.AssignSubject(ctx, marker.UID, anna.UID)
			return err
		}, want: []tagEvent{onPhoto("tagged", anna.UID, "")}},
		{name: "delete", run: func() error { return store.DeleteMarker(ctx, marker.UID) },
			want: []tagEvent{onPhoto("untagged", anna.UID, "")}},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if got := observer.take(); !reflect.DeepEqual(got, step.want) {
			t.Fatalf("%s: observer heard %+v, want %+v", step.name, got, step.want)
		}
	}
}

// TestTagObserver_failureRollsBackAssignment checks the observer runs inside
// the assignment's transaction: when it fails, the assignment does not happen.
func TestTagObserver_failureRollsBackAssignment(t *testing.T) {
	base, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	boom := errors.New("boom")
	store := base.WithTagObserver(&recordingObserver{fail: boom})

	photoUID := makePhoto(t, photoStore, "tagobsfail")
	anna := makeSubject(t, store, "Anna")
	marker, err := base.CreateMarker(ctx, people.Marker{PhotoUID: photoUID, W: 0.1, H: 0.1})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	if _, err := store.AssignSubjectAudited(ctx, marker.UID, anna.UID, audit.Entry{
		Action: audit.ActionFaceAssign, TargetType: "markers",
	}); !errors.Is(err, boom) {
		t.Fatalf("AssignSubjectAudited = %v, want the observer's error", err)
	}
	got, err := base.GetMarkerByUID(ctx, marker.UID)
	if err != nil {
		t.Fatalf("GetMarkerByUID: %v", err)
	}
	if got.SubjectUID != nil {
		t.Fatalf("marker subject = %v after a failed report, want the assignment rolled back", *got.SubjectUID)
	}
}

// TestTagObserver_hearsHandAttachment checks attaching a person by hand is a
// tagging (once — a repeat attach is not) and detaching it an untagging (only
// when there was a link to remove).
func TestTagObserver_hearsHandAttachment(t *testing.T) {
	base, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	observer := &recordingObserver{}
	store := base.WithTagObserver(observer)

	photoUID := makePhoto(t, photoStore, "tagobsattach")
	anna := makeSubject(t, store, "Anna")
	entry := audit.Entry{Action: audit.ActionFaceAssign, TargetType: "markers"}
	want := func(kind string) []tagEvent {
		return []tagEvent{{kind: kind, tag: people.Tagging{PhotoUID: photoUID, SubjectUID: anna.UID}}}
	}
	steps := []struct {
		name string
		run  func() error
		want []tagEvent
	}{
		{name: "attach", want: want("tagged"), run: func() error {
			_, err := store.AttachSubjectToPhoto(ctx, photoUID, anna.UID, entry)
			return err
		}},
		{name: "repeat attach", run: func() error {
			_, err := store.AttachSubjectToPhoto(ctx, photoUID, anna.UID, entry)
			return err
		}},
		{name: "detach", want: want("untagged"), run: func() error {
			_, err := store.DetachSubjectFromPhoto(ctx, photoUID, anna.UID, entry)
			return err
		}},
		{name: "repeat detach", run: func() error {
			_, err := store.DetachSubjectFromPhoto(ctx, photoUID, anna.UID, entry)
			return err
		}},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if got := observer.take(); !reflect.DeepEqual(got, step.want) {
			t.Fatalf("%s: observer heard %+v, want %+v", step.name, got, step.want)
		}
	}
}
