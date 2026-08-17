package photoapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processing"
)

// fakeProcessing is a controllable ProcessingService: it answers with the pinned
// report/status or error and records what it was asked for.
type fakeProcessing struct {
	report    []processing.Status
	status    processing.Status
	reportErr error
	runErr    error
	ranStep   processing.Step
	ranUID    string
}

// Report returns the pinned report or error.
func (f *fakeProcessing) Report(_ context.Context, _ string) ([]processing.Status, error) {
	return f.report, f.reportErr
}

// Run records the request and returns the pinned status or error.
func (f *fakeProcessing) Run(
	_ context.Context, photoUID string, step processing.Step,
) (processing.Status, error) {
	f.ranUID, f.ranStep = photoUID, step
	return f.status, f.runErr
}

// TestResolveProcessing_neverFailsTheDetail checks the report is best-effort:
// with no service wired, or a failing one, the detail simply omits the block
// rather than refusing to show the photo.
func TestResolveProcessing_neverFailsTheDetail(t *testing.T) {
	t.Parallel()

	if got := (&API{}).resolveProcessing(t.Context(), "p1"); got != nil {
		t.Errorf("unwired API returned %v, want nil", got)
	}

	broken := &API{processing: &fakeProcessing{reportErr: errors.New("db down")}}
	if got := broken.resolveProcessing(t.Context(), "p1"); got != nil {
		t.Errorf("failing service returned %v, want nil", got)
	}

	wired := &API{processing: &fakeProcessing{
		report: []processing.Status{{Step: processing.StepMetadata, State: processing.StateDone}},
	}}
	if got := wired.resolveProcessing(t.Context(), "p1"); len(got) != 1 {
		t.Errorf("wired service returned %v, want one entry", got)
	}
}

// TestHandleRunProcessingStep_unwiredIs503 checks an instance with no processing
// service says so rather than pretending to schedule work.
func TestHandleRunProcessingStep_unwiredIs503(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, "/photos/p1/process/thumbnail", nil)
	(&API{}).handleRunProcessingStep(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// runStep drives the handler with uid and step in the chi route context and
// returns the recorder.
func runStep(t *testing.T, api *API, uid, step string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, "/photos/"+uid+"/process/"+step, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("uid", uid)
	rctx.URLParams.Add("step", step)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	api.handleRunProcessingStep(rec, req)
	return rec
}

// TestHandleRunProcessingStep_rejectsUnknownStepBeforeTheService checks the path
// parameter is validated at the boundary: a step nobody reports never reaches the
// service at all.
func TestHandleRunProcessingStep_rejectsUnknownStepBeforeTheService(t *testing.T) {
	t.Parallel()

	fake := &fakeProcessing{}
	rec := runStep(t, &API{processing: fake}, "p1", "storyboard")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if fake.ranStep != "" {
		t.Errorf("service was asked to run %q, want nothing", fake.ranStep)
	}
}

// TestHandleRunProcessingStep_answersWithTheNewState checks the success body: the
// step's new state, so the client can update the row without re-fetching.
func TestHandleRunProcessingStep_answersWithTheNewState(t *testing.T) {
	t.Parallel()

	fake := &fakeProcessing{
		status: processing.Status{Step: processing.StepOCR, State: processing.StateQueued},
	}
	rec := runStep(t, &API{processing: fake}, "p1", "ocr")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if fake.ranUID != "p1" || fake.ranStep != processing.StepOCR {
		t.Errorf("service ran (%q, %q), want (p1, ocr)", fake.ranUID, fake.ranStep)
	}
	var body processing.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Step != processing.StepOCR || body.State != processing.StateQueued {
		t.Errorf("body = %+v, want a queued ocr", body)
	}
}

// TestRunProcessingStepRouteIsGuardedByMaintainer pins the route's guard: an
// admin is not enough, because scheduling background work is operations. A
// rejected request must never reach the service.
func TestRunProcessingStepRouteIsGuardedByMaintainer(t *testing.T) {
	t.Parallel()

	fake := &fakeProcessing{}
	api := &API{
		processing:        fake,
		requireAuth:       regenPass,
		requireWrite:      regenPass,
		requireAdmin:      regenPass,
		requireMaintainer: regenDeny,
		requireDownload:   regenPass,
	}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req(http.MethodPost, "/photos/ph_1/process/thumbnail"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if fake.ranStep != "" {
		t.Errorf("the service ran %q despite the guard rejecting the request", fake.ranStep)
	}
}

// TestWriteProcessingError maps every refusal onto its status code.
func TestWriteProcessingError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "unknown step", err: processing.ErrUnknownStep, want: http.StatusBadRequest},
		{name: "missing photo", err: photos.ErrPhotoNotFound, want: http.StatusNotFound},
		{name: "not applicable", err: processing.ErrStepNotApplicable, want: http.StatusConflict},
		{name: "anything else", err: errors.New("boom"), want: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			writeProcessingError(rec, tt.err)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}
