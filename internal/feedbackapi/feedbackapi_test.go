package feedbackapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/feedback"
)

// passthrough is a no-op middleware standing in for the write guard in tests.
func passthrough(next http.Handler) http.Handler { return next }

// fakeStore records the last mutation it received and returns a preset error, so a
// handler test can assert the key, the audit entry and the status mapping without a
// database.
type fakeStore struct {
	err        error
	called     string
	faceKey    feedback.FaceRejectionKey
	labelKey   feedback.LabelRejectionKey
	confirmKey feedback.FaceConfirmationKey
	dupKey     feedback.DuplicateDismissalKey
	entry      audit.Entry
}

func (f *fakeStore) RejectFace(_ context.Context, key feedback.FaceRejectionKey, entry audit.Entry) error {
	f.called, f.faceKey, f.entry = "RejectFace", key, entry
	return f.err
}

func (f *fakeStore) UnrejectFace(_ context.Context, key feedback.FaceRejectionKey, entry audit.Entry) error {
	f.called, f.faceKey, f.entry = "UnrejectFace", key, entry
	return f.err
}

func (f *fakeStore) RejectLabel(_ context.Context, key feedback.LabelRejectionKey, entry audit.Entry) error {
	f.called, f.labelKey, f.entry = "RejectLabel", key, entry
	return f.err
}

func (f *fakeStore) UnrejectLabel(_ context.Context, key feedback.LabelRejectionKey, entry audit.Entry) error {
	f.called, f.labelKey, f.entry = "UnrejectLabel", key, entry
	return f.err
}

func (f *fakeStore) ConfirmFace(_ context.Context, key feedback.FaceConfirmationKey, entry audit.Entry) error {
	f.called, f.confirmKey, f.entry = "ConfirmFace", key, entry
	return f.err
}

func (f *fakeStore) UnconfirmFace(_ context.Context, key feedback.FaceConfirmationKey, entry audit.Entry) error {
	f.called, f.confirmKey, f.entry = "UnconfirmFace", key, entry
	return f.err
}

func (f *fakeStore) DismissDuplicate(
	_ context.Context, key feedback.DuplicateDismissalKey, entry audit.Entry,
) error {
	f.called, f.dupKey, f.entry = "DismissDuplicate", key, entry
	return f.err
}

func (f *fakeStore) UndismissDuplicate(
	_ context.Context, key feedback.DuplicateDismissalKey, entry audit.Entry,
) error {
	f.called, f.dupKey, f.entry = "UndismissDuplicate", key, entry
	return f.err
}

// serve routes one request through a fresh API over store and returns the recorder.
func serve(store Store, method, target, body string) *httptest.ResponseRecorder {
	api := NewAPI(Config{Store: store, RequireWrite: passthrough})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	req := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestHandleFaceReject checks a valid rejection answers 204 and forwards the key and
// an audit entry describing the face and subject.
func TestHandleFaceReject(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	rec := serve(store, http.MethodPost, "/api/v1/feedback/face-rejections",
		`{"photo_uid":"ph1","face_index":2,"subject_uid":"su1"}`)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if store.called != "RejectFace" {
		t.Fatalf("store call = %q, want RejectFace", store.called)
	}
	want := feedback.FaceRejectionKey{PhotoUID: "ph1", FaceIndex: 2, SubjectUID: "su1"}
	if store.faceKey != want {
		t.Errorf("face key = %+v, want %+v", store.faceKey, want)
	}
	if store.entry.Action != audit.ActionFaceReject || store.entry.TargetUID != "su1" {
		t.Errorf("audit entry = %+v, want face.reject targeting su1", store.entry)
	}
	if store.entry.Details["photo_uid"] != "ph1" || store.entry.Details["face_index"] != 2 {
		t.Errorf("audit details = %+v, want photo_uid ph1, face_index 2", store.entry.Details)
	}
}

// TestHandleFaceUnreject checks the take-back route answers 204 and calls UnrejectFace.
func TestHandleFaceUnreject(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	rec := serve(store, http.MethodDelete, "/api/v1/feedback/face-rejections",
		`{"photo_uid":"ph1","face_index":0,"subject_uid":"su1"}`)

	if rec.Code != http.StatusNoContent || store.called != "UnrejectFace" {
		t.Fatalf("status = %d, call = %q, want 204 UnrejectFace", rec.Code, store.called)
	}
	if store.entry.Action != audit.ActionFaceUnreject {
		t.Errorf("audit action = %q, want face.unreject", store.entry.Action)
	}
}

// TestHandleFaceConfirm checks a valid confirmation answers 204 and forwards the
// key and an audit entry describing the face and subject.
func TestHandleFaceConfirm(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	rec := serve(store, http.MethodPost, "/api/v1/feedback/face-confirmations",
		`{"photo_uid":"ph1","face_index":2,"subject_uid":"su1"}`)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if store.called != "ConfirmFace" {
		t.Fatalf("store call = %q, want ConfirmFace", store.called)
	}
	want := feedback.FaceConfirmationKey{PhotoUID: "ph1", FaceIndex: 2, SubjectUID: "su1"}
	if store.confirmKey != want {
		t.Errorf("confirmation key = %+v, want %+v", store.confirmKey, want)
	}
	if store.entry.Action != audit.ActionFaceConfirm || store.entry.TargetUID != "su1" {
		t.Errorf("audit entry = %+v, want face.confirm targeting su1", store.entry)
	}
	if store.entry.Details["photo_uid"] != "ph1" || store.entry.Details["face_index"] != 2 {
		t.Errorf("audit details = %+v, want photo_uid ph1, face_index 2", store.entry.Details)
	}
}

// TestHandleFaceUnconfirm checks the take-back route answers 204 and calls
// UnconfirmFace.
func TestHandleFaceUnconfirm(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	rec := serve(store, http.MethodDelete, "/api/v1/feedback/face-confirmations",
		`{"photo_uid":"ph1","face_index":0,"subject_uid":"su1"}`)

	if rec.Code != http.StatusNoContent || store.called != "UnconfirmFace" {
		t.Fatalf("status = %d, call = %q, want 204 UnconfirmFace", rec.Code, store.called)
	}
	if store.entry.Action != audit.ActionFaceUnconfirm {
		t.Errorf("audit action = %q, want face.unconfirm", store.entry.Action)
	}
}

// TestHandleLabelRejectAndUnreject checks both label routes forward the key and the
// matching audit action.
func TestHandleLabelRejectAndUnreject(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	rec := serve(store, http.MethodPost, "/api/v1/feedback/label-rejections",
		`{"photo_uid":"ph1","label_uid":"lb1"}`)
	if rec.Code != http.StatusNoContent || store.called != "RejectLabel" {
		t.Fatalf("reject: status = %d, call = %q, want 204 RejectLabel", rec.Code, store.called)
	}
	if store.labelKey != (feedback.LabelRejectionKey{PhotoUID: "ph1", LabelUID: "lb1"}) {
		t.Errorf("label key = %+v", store.labelKey)
	}
	if store.entry.Action != audit.ActionLabelReject || store.entry.TargetUID != "lb1" {
		t.Errorf("audit entry = %+v, want label.reject targeting lb1", store.entry)
	}

	store = &fakeStore{}
	rec = serve(store, http.MethodDelete, "/api/v1/feedback/label-rejections",
		`{"photo_uid":"ph1","label_uid":"lb1"}`)
	if rec.Code != http.StatusNoContent || store.called != "UnrejectLabel" {
		t.Fatalf("unreject: status = %d, call = %q, want 204 UnrejectLabel", rec.Code, store.called)
	}
}

// TestHandleReject_errors checks validation failures and store errors map to the
// right status without (for validation) ever calling the store.
func TestHandleReject_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		target     string
		body       string
		storeErr   error
		wantStatus int
		wantCalled bool
	}{
		{"missing subject", "/api/v1/feedback/face-rejections", `{"photo_uid":"ph1","face_index":0}`, nil, 400, false},
		{"missing photo", "/api/v1/feedback/face-rejections", `{"subject_uid":"su1"}`, nil, 400, false},
		{"negative index", "/api/v1/feedback/face-rejections",
			`{"photo_uid":"ph1","face_index":-1,"subject_uid":"su1"}`, nil, 400, false},
		{"unknown field", "/api/v1/feedback/face-rejections",
			`{"photo_uid":"ph1","subject_uid":"su1","x":1}`, nil, 400, false},
		{"malformed json", "/api/v1/feedback/label-rejections", `{`, nil, 400, false},
		{"missing label", "/api/v1/feedback/label-rejections", `{"photo_uid":"ph1"}`, nil, 400, false},
		{"target not found", "/api/v1/feedback/face-rejections",
			`{"photo_uid":"ph1","face_index":0,"subject_uid":"su1"}`, feedback.ErrTargetNotFound, 404, true},
		{"confirm missing subject", "/api/v1/feedback/face-confirmations",
			`{"photo_uid":"ph1","face_index":0}`, nil, 400, false},
		{"confirm target not found", "/api/v1/feedback/face-confirmations",
			`{"photo_uid":"ph1","face_index":0,"subject_uid":"su1"}`, feedback.ErrTargetNotFound, 404, true},
		{"store failure", "/api/v1/feedback/label-rejections",
			`{"photo_uid":"ph1","label_uid":"lb1"}`, errors.New("boom"), 500, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{err: tt.storeErr}
			rec := serve(store, http.MethodPost, tt.target, tt.body)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if (store.called != "") != tt.wantCalled {
				t.Errorf("store called = %v (%q), want %v", store.called != "", store.called, tt.wantCalled)
			}
		})
	}
}

// TestRejectionStatus checks the store-error → HTTP-status mapping.
func TestRejectionStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"empty key", feedback.ErrEmptyKey, http.StatusBadRequest},
		{"target not found", feedback.ErrTargetNotFound, http.StatusNotFound},
		{"other", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got, _ := rejectionStatus(tt.err); got != tt.want {
				t.Errorf("rejectionStatus(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

// TestHandleDuplicateDismiss checks a valid dismissal answers 204 and forwards the
// pair plus an audit entry naming both photos.
func TestHandleDuplicateDismiss(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	rec := serve(store, http.MethodPost, "/api/v1/feedback/duplicate-dismissals",
		`{"photo_uid":"ph1","other_uid":"ph2"}`)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if store.called != "DismissDuplicate" {
		t.Fatalf("store call = %q, want DismissDuplicate", store.called)
	}
	want := feedback.DuplicateDismissalKey{PhotoUID: "ph1", OtherUID: "ph2"}
	if store.dupKey != want {
		t.Errorf("dismissal key = %+v, want %+v", store.dupKey, want)
	}
	if store.entry.Action != audit.ActionDuplicateDismiss || store.entry.TargetUID != "ph1" {
		t.Errorf("audit entry = %+v, want duplicate.dismiss targeting ph1", store.entry)
	}
	if store.entry.Details["other_uid"] != "ph2" {
		t.Errorf("audit details = %+v, want other_uid ph2", store.entry.Details)
	}
}

// TestHandleDuplicateUndismiss checks the take-back route answers 204 and calls
// UndismissDuplicate.
func TestHandleDuplicateUndismiss(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	rec := serve(store, http.MethodDelete, "/api/v1/feedback/duplicate-dismissals",
		`{"photo_uid":"ph1","other_uid":"ph2"}`)

	if rec.Code != http.StatusNoContent || store.called != "UndismissDuplicate" {
		t.Fatalf("status = %d, call = %q, want 204 UndismissDuplicate", rec.Code, store.called)
	}
	if store.entry.Action != audit.ActionDuplicateUndismiss {
		t.Errorf("audit action = %q, want duplicate.undismiss", store.entry.Action)
	}
}

// TestHandleDuplicateDismissMissingOther checks a body naming only one photo is
// refused with 400 before the store is touched.
func TestHandleDuplicateDismissMissingOther(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	rec := serve(store, http.MethodPost, "/api/v1/feedback/duplicate-dismissals",
		`{"photo_uid":"ph1"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if store.called != "" {
		t.Errorf("store call = %q, want none", store.called)
	}
}

// TestHandleDuplicateDismissSamePhoto checks the store's ErrSamePhoto maps to 400:
// a photo is never a duplicate of itself, and that is a caller mistake, not a 500.
func TestHandleDuplicateDismissSamePhoto(t *testing.T) {
	t.Parallel()

	store := &fakeStore{err: feedback.ErrSamePhoto}
	rec := serve(store, http.MethodPost, "/api/v1/feedback/duplicate-dismissals",
		`{"photo_uid":"ph1","other_uid":"ph1"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestHandleDuplicateDismissUnknownPhoto checks a pair naming a photo that does not
// exist answers 404 rather than a generic failure.
func TestHandleDuplicateDismissUnknownPhoto(t *testing.T) {
	t.Parallel()

	store := &fakeStore{err: feedback.ErrTargetNotFound}
	rec := serve(store, http.MethodPost, "/api/v1/feedback/duplicate-dismissals",
		`{"photo_uid":"ph1","other_uid":"ph2"}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
