package jobs

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestIsLibraryWide verifies the library-wide category holds exactly the types
// that work on one project's library as a whole, and none of the per-photo or
// instance-wide ones.
func TestIsLibraryWide(t *testing.T) {
	t.Parallel()

	for _, jobType := range []string{
		TypeFaceCluster, TypeFamilyExport, TypeNamelessDetach, TypeNamelessRestore,
		TypeTaskDigest, TypeNotificationPurge, TypeStorageRekey,
	} {
		if !IsLibraryWide(jobType) {
			t.Errorf("IsLibraryWide(%s) = false, want true", jobType)
		}
	}
	for _, jobType := range []string{
		TypeThumbnail, TypeImageEmbed, TypeFaceDetect, TypeMetadata, TypeOCR, TypeSidecar, TypePlaces,
		TypeStoryboard, TypeHLSTranscode, TypeAlbumFeaturedNotify,
		TypeBackup, TypeMailSend, TypePushSend, TypeTagNotify, "",
	} {
		if IsLibraryWide(jobType) {
			t.Errorf("IsLibraryWide(%q) = true, want false", jobType)
		}
	}
}

// TestProjectUIDOf verifies the project is read from the payload's
// project_uid, and "" for every payload naming none.
func TestProjectUIDOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{name: "named", payload: `{"project_uid":"pj_a","user_uid":"us_1"}`, want: "pj_a"},
		{name: "no payload"},
		{name: "empty object", payload: `{}`},
		{name: "photo job", payload: `{"photo_uid":"ph_1"}`},
		{name: "not an object", payload: `["pj_a"]`},
		{name: "not a string", payload: `{"project_uid":7}`},
		{name: "not JSON", payload: `{nope`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			job := Job{}
			if tt.payload != "" {
				job.Payload = json.RawMessage(tt.payload)
			}
			if got := ProjectUIDOf(job); got != tt.want {
				t.Errorf("ProjectUIDOf(%s) = %q, want %q", tt.payload, got, tt.want)
			}
		})
	}
}

// TestCheckProject verifies the refusal applies to a library-wide type naming
// no project and to nothing else.
func TestCheckProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		jobType string
		payload string
		wantErr error
	}{
		{name: "library-wide with a project", jobType: TypeFaceCluster, payload: `{"project_uid":"pj_a"}`},
		{name: "library-wide without one", jobType: TypeFaceCluster, payload: `{"recluster":true}`,
			wantErr: ErrProjectRequired},
		{name: "library-wide with no payload", jobType: TypeNotificationPurge, wantErr: ErrProjectRequired},
		{name: "per-photo needs none", jobType: TypeThumbnail, payload: `{"photo_uid":"ph_1"}`},
		{name: "instance-wide needs none", jobType: TypeBackup},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var payload json.RawMessage
			if tt.payload != "" {
				payload = json.RawMessage(tt.payload)
			}
			if err := checkProject(tt.jobType, payload); !errors.Is(err, tt.wantErr) {
				t.Errorf("checkProject(%s, %s) = %v, want %v", tt.jobType, tt.payload, err, tt.wantErr)
			}
		})
	}
}

// TestEnqueueNotificationPurge verifies the purge names its project in the
// payload, swallows a duplicate, and refuses an empty project without touching
// the queue.
func TestEnqueueNotificationPurge(t *testing.T) {
	t.Parallel()

	fake := &fakeEnqueuer{}
	enq := &Enqueuer{store: fake}
	if err := enq.EnqueueNotificationPurge(t.Context(), "pj_a"); err != nil {
		t.Fatalf("EnqueueNotificationPurge: %v", err)
	}
	if fake.lastType != TypeNotificationPurge || ProjectUIDOf(Job{Payload: fake.lastPayload}) != "pj_a" {
		t.Fatalf("enqueued %s %s, want notification_purge for pj_a", fake.lastType, fake.lastPayload)
	}

	fake.err = ErrDuplicate
	if err := enq.EnqueueNotificationPurge(t.Context(), "pj_a"); err != nil {
		t.Fatalf("a duplicate purge = %v, want a silent no-op", err)
	}

	calls := fake.calls
	if err := enq.EnqueueNotificationPurge(t.Context(), ""); !errors.Is(err, ErrProjectRequired) {
		t.Fatalf("EnqueueNotificationPurge without a project = %v, want ErrProjectRequired", err)
	}
	if fake.calls != calls {
		t.Fatalf("the refused purge reached the queue")
	}
}
