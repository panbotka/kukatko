package photoapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
)

// TestDecodeAttach checks the attach body is read strictly enough that a request
// naming nobody is a 400 rather than a silent no-op.
func TestDecodeAttach(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{name: "a subject uid", body: `{"subject_uid":"su_1"}`, want: "su_1"},
		{name: "surrounding space is trimmed", body: `{"subject_uid":"  su_1 "}`, want: "su_1"},
		{name: "an empty uid is rejected", body: `{"subject_uid":"   "}`, wantErr: true},
		{name: "a missing key is rejected", body: `{}`, wantErr: true},
		{name: "a malformed body is rejected", body: `{`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
				"/photos/p1/people", strings.NewReader(tt.body))
			got, err := decodeAttach(req)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("decodeAttach(%q) = %q, want an error", tt.body, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeAttach(%q) returned %v", tt.body, err)
			}
			if got != tt.want {
				t.Errorf("decodeAttach(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

// TestWriteAttachError checks each end of a missing link is reported as a 404 and
// anything else as a 500, so a client can tell "you asked for something that does
// not exist" from "we broke".
func TestWriteAttachError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "missing photo", err: people.ErrPhotoNotFound, want: http.StatusNotFound},
		{name: "missing subject", err: people.ErrSubjectNotFound, want: http.StatusNotFound},
		{name: "anything else", err: errors.New("boom"), want: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			writeAttachError(rec, tt.err, "attaching failed")
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

// failingAttacher is a PeopleAttacher whose read always fails, so the detail's
// degradation can be exercised.
type failingAttacher struct{}

// ListPhotoSubjects always fails.
func (failingAttacher) ListPhotoSubjects(context.Context, string) ([]people.PhotoSubject, error) {
	return nil, errors.New("database is on fire")
}

// AttachSubjectToPhoto is unused by these tests.
func (failingAttacher) AttachSubjectToPhoto(
	context.Context, string, string, audit.Entry,
) ([]people.PhotoSubject, error) {
	return nil, errors.New("unused")
}

// DetachSubjectFromPhoto is unused by these tests.
func (failingAttacher) DetachSubjectFromPhoto(
	context.Context, string, string, audit.Entry,
) ([]people.PhotoSubject, error) {
	return nil, errors.New("unused")
}

// TestResolveAttachedPeople checks the detail never loses a photo over the list
// of who is on it: with no backend, and with a backend that fails, it answers an
// empty array rather than null or an error.
func TestResolveAttachedPeople(t *testing.T) {
	t.Parallel()

	unwired := &API{}
	if got := unwired.resolveAttachedPeople(context.Background(), "p1"); got == nil || len(got) != 0 {
		t.Errorf("unwired = %+v, want an empty non-nil slice", got)
	}
	broken := &API{attacher: failingAttacher{}}
	if got := broken.resolveAttachedPeople(context.Background(), "p1"); got == nil || len(got) != 0 {
		t.Errorf("failing backend = %+v, want an empty non-nil slice", got)
	}
}
