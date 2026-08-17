//go:build integration

package photoapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/processing"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They cover the per-photo processing report on the
// detail payload — every state it can report — and the maintainer-only endpoint
// that schedules one step.

// processingEntry mirrors one entry of the detail response's `processing` array.
type processingEntry struct {
	Step      string     `json:"step"`
	State     string     `json:"state"`
	At        *time.Time `json:"at"`
	Error     string     `json:"error"`
	FaceCount *int       `json:"face_count"`
	TextFound *bool      `json:"text_found"`
}

// detailProcessing fetches the photo detail and returns its processing report
// indexed by step, failing the test on a non-200 status.
func detailProcessing(
	t *testing.T, client *http.Client, base, uid string,
) map[string]processingEntry {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/"+uid, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Processing []processingEntry `json:"processing"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if len(body.Processing) != len(processing.Steps) {
		t.Fatalf("processing has %d entries, want %d", len(body.Processing), len(processing.Steps))
	}
	byStep := make(map[string]processingEntry, len(body.Processing))
	for i, entry := range body.Processing {
		if entry.Step != string(processing.Steps[i]) {
			t.Errorf("processing[%d].Step = %q, want %q", i, entry.Step, processing.Steps[i])
		}
		byStep[entry.Step] = entry
	}
	return byStep
}

// wantState fails the test unless the step is reported in the expected state.
func wantState(t *testing.T, report map[string]processingEntry, step processing.Step, state processing.State) {
	t.Helper()
	entry, ok := report[string(step)]
	if !ok {
		t.Fatalf("step %q missing from the report", step)
	}
	if entry.State != string(state) {
		t.Errorf("step %q state = %q, want %q", step, entry.State, state)
	}
}

// TestDetailProcessing_freshPhotoIsPending covers the baseline: a catalogued
// photo nothing has run on yet reports every applicable step as pending, and the
// steps that cannot apply to it as skipped.
func TestDetailProcessing_freshPhotoIsPending(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	photo := env.seedPhoto(t, photos.Photo{Title: "fresh"}, "fresh.jpg", 10, 20, 30)

	report := detailProcessing(t, client, env.server.URL, photo.UID)
	for _, step := range []processing.Step{
		processing.StepMetadata, processing.StepThumbnail, processing.StepImageEmbed,
		processing.StepFaceDetect, processing.StepOCR, processing.StepSidecar,
	} {
		wantState(t, report, step, processing.StatePending)
	}
	// No coordinate: there is nothing to reverse-geocode, so this is not a gap.
	wantState(t, report, processing.StepPlaces, processing.StateSkipped)
}

// TestDetailProcessing_evidenceReadsDone walks each step's evidence into the
// database and checks it is reported as done, with the moment it landed and the
// step-specific result.
func TestDetailProcessing_evidenceReadsDone(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	lat, lng := 49.36, 16.72
	photo := env.seedPhoto(t,
		photos.Photo{Title: "done", Lat: &lat, Lng: &lng}, "done.jpg", 40, 50, 60)

	if _, err := env.db.Pool().Exec(t.Context(),
		"UPDATE photos SET metadata_extracted_at = now() WHERE uid = $1", photo.UID); err != nil {
		t.Fatalf("stamping metadata_extracted_at: %v", err)
	}
	if err := env.store.SetPhash(t.Context(),
		photos.Phash{PhotoUID: photo.UID, Phash: 1, Dhash: 2}); err != nil {
		t.Fatalf("SetPhash: %v", err)
	}
	if _, err := env.vectors.SaveEmbedding(t.Context(), vectors.Embedding{
		PhotoUID: photo.UID, Vector: imageVecAt(map[int]float32{0: 1}), Model: "m",
	}); err != nil {
		t.Fatalf("SaveEmbedding: %v", err)
	}
	// A detection that found nothing: the photo was looked at, and that is a
	// success with a face count of zero, not a gap.
	if err := env.vectors.RecordFaceDetection(t.Context(), photo.UID,
		vectors.Detection{Model: "face-model", FrameWidth: photo.FileWidth, FrameHeight: photo.FileHeight}); err != nil {
		t.Fatalf("RecordFaceDetection: %v", err)
	}
	if err := env.store.SaveOCR(t.Context(), photo.UID, photos.OCR{Text: "", Model: "ocr-model"}); err != nil {
		t.Fatalf("SaveOCR: %v", err)
	}
	if _, err := places.NewStore(env.db.Pool()).SavePlace(t.Context(), places.Place{
		PhotoUID: photo.UID, Country: "Česko", City: "Brno", PlaceName: "Brno", Lat: &lat, Lng: &lng,
	}); err != nil {
		t.Fatalf("SavePlace: %v", err)
	}
	if err := env.store.MarkSidecarWritten(t.Context(), photo.UID); err != nil {
		t.Fatalf("MarkSidecarWritten: %v", err)
	}

	report := detailProcessing(t, client, env.server.URL, photo.UID)
	for _, step := range processing.Steps {
		wantState(t, report, step, processing.StateDone)
		if report[string(step)].At == nil {
			t.Errorf("step %q is done but carries no timestamp", step)
		}
	}
	faces := report[string(processing.StepFaceDetect)]
	if faces.FaceCount == nil || *faces.FaceCount != 0 {
		t.Errorf("face_count = %v, want a present 0", faces.FaceCount)
	}
	ocr := report[string(processing.StepOCR)]
	if ocr.TextFound == nil || *ocr.TextFound {
		t.Errorf("text_found = %v, want a present false", ocr.TextFound)
	}
}

// TestDetailProcessing_queueStates covers the three states that come from the
// queue rather than from evidence: waiting, being worked on, and broken.
func TestDetailProcessing_queueStates(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	photo := env.seedPhoto(t, photos.Photo{Title: "queued"}, "queued.jpg", 70, 80, 90)

	enqueue := func(jobType string) jobs.Job {
		t.Helper()
		job, err := env.jobs.Enqueue(t.Context(), jobType,
			json.RawMessage(`{"photo_uid":"`+photo.UID+`"}`), jobs.EnqueueOptions{})
		if err != nil {
			t.Fatalf("Enqueue(%s): %v", jobType, err)
		}
		return job
	}
	enqueue(jobs.TypeMetadata)
	enqueue(jobs.TypeImageEmbed)
	dead := enqueue(jobs.TypeFaceDetect)

	// Claim the embed job so it reports as running.
	if _, err := env.jobs.Claim(t.Context(), "w1", jobs.TypeImageEmbed); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	// Dead-letter the face job so it reports as failed, carrying its reason.
	if _, err := env.db.Pool().Exec(t.Context(),
		"UPDATE jobs SET state = 'dead', last_error = $2 WHERE id = $1",
		dead.ID, "the box refused the image"); err != nil {
		t.Fatalf("dead-lettering the face job: %v", err)
	}

	report := detailProcessing(t, client, env.server.URL, photo.UID)
	wantState(t, report, processing.StepMetadata, processing.StateQueued)
	wantState(t, report, processing.StepImageEmbed, processing.StateRunning)
	wantState(t, report, processing.StepFaceDetect, processing.StateFailed)
	if got := report[string(processing.StepFaceDetect)].Error; got != "the box refused the image" {
		t.Errorf("failed step error = %q, want the recorded reason", got)
	}
	wantState(t, report, processing.StepThumbnail, processing.StatePending)
}

// TestDetailProcessing_queuedRetryAfterAnErrorReadsFailed pins the second half of
// "failed": a job back in the queue for another attempt still says why the last
// one did not work, rather than reading as a fresh wait.
func TestDetailProcessing_queuedRetryAfterAnErrorReadsFailed(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	photo := env.seedPhoto(t, photos.Photo{Title: "retry"}, "retry.jpg", 11, 22, 33)

	job, err := env.jobs.Enqueue(t.Context(), jobs.TypeThumbnail,
		json.RawMessage(`{"photo_uid":"`+photo.UID+`"}`), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := env.jobs.Claim(t.Context(), "w1", jobs.TypeThumbnail)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if _, err := env.jobs.Fail(t.Context(), claimed.ID, "w1",
		errors.New("decoding the original failed")); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if claimed.ID != job.ID {
		t.Fatalf("claimed job %d, want %d", claimed.ID, job.ID)
	}

	report := detailProcessing(t, client, env.server.URL, photo.UID)
	wantState(t, report, processing.StepThumbnail, processing.StateFailed)
	if got := report[string(processing.StepThumbnail)].Error; got == "" {
		t.Error("a retry pending after a failure reported no error text")
	}
}

// TestDetailProcessing_videoSkipsFacesAndText covers the media-type rule: a video
// is outside face detection and text recognition, so their absence is not a gap.
func TestDetailProcessing_videoSkipsFacesAndText(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	clip := env.seedPhoto(t,
		photos.Photo{Title: "clip", MediaType: photos.MediaVideo}, "clip.mp4", 12, 34, 56)

	report := detailProcessing(t, client, env.server.URL, clip.UID)
	wantState(t, report, processing.StepFaceDetect, processing.StateSkipped)
	wantState(t, report, processing.StepOCR, processing.StateSkipped)
	wantState(t, report, processing.StepThumbnail, processing.StatePending)
}

// TestDetailProcessing_placeMarkerWithoutGPSIsNotDone pins the one row that looks
// like evidence and is not: the coordinate-less marker the geocoder writes for a
// photo with no GPS so it never retries it. It records that there was nothing to
// do, and reporting it as done would claim a place the photo has not got.
func TestDetailProcessing_placeMarkerWithoutGPSIsNotDone(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	photo := env.seedPhoto(t, photos.Photo{Title: "nogps"}, "nogps.jpg", 21, 32, 43)

	if _, err := places.NewStore(env.db.Pool()).SavePlace(t.Context(),
		places.Place{PhotoUID: photo.UID}); err != nil {
		t.Fatalf("SavePlace: %v", err)
	}

	report := detailProcessing(t, client, env.server.URL, photo.UID)
	wantState(t, report, processing.StepPlaces, processing.StateSkipped)
}

// TestRunProcessingStep_rbac pins the guard: scheduling background work is an
// operations action, so only a maintainer may ask for it.
func TestRunProcessingStep_rbac(t *testing.T) {
	env := newEnv(t)
	photo := env.seedPhoto(t, photos.Photo{Title: "rbac"}, "rbac.jpg", 13, 24, 35)

	tests := []struct {
		role auth.Role
		want int
	}{
		{role: auth.RoleViewer, want: http.StatusForbidden},
		{role: auth.RoleEditor, want: http.StatusForbidden},
		{role: auth.RoleAdmin, want: http.StatusForbidden},
		{role: auth.RoleMaintainer, want: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			client, _ := env.login(t, "user-"+string(tt.role), tt.role)
			resp := mustDo(t, client, http.MethodPost,
				env.server.URL+"/api/v1/photos/"+photo.UID+"/process/thumbnail", nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.want {
				t.Errorf("%s status = %d, want %d", tt.role, resp.StatusCode, tt.want)
			}
		})
	}

	// Anonymous callers never reach the handler.
	resp := mustDo(t, &http.Client{}, http.MethodPost,
		env.server.URL+"/api/v1/photos/"+photo.UID+"/process/thumbnail", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous status = %d, want 401", resp.StatusCode)
	}
}

// TestRunProcessingStep_enqueuesAndIsIdempotent checks the happy path: the step
// is scheduled, answered as queued, and a second click is absorbed by the queue's
// dedup index rather than piling up work.
func TestRunProcessingStep_enqueuesAndIsIdempotent(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "maint", auth.RoleMaintainer)
	photo := env.seedPhoto(t, photos.Photo{Title: "run"}, "run.jpg", 14, 25, 36)

	for range 2 {
		resp := mustDo(t, client, http.MethodPost,
			env.server.URL+"/api/v1/photos/"+photo.UID+"/process/image_embed", nil)
		var status processingEntry
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			t.Fatalf("decode: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if status.Step != jobs.TypeImageEmbed || status.State != string(processing.StateQueued) {
			t.Errorf("response = %+v, want a queued image_embed", status)
		}
	}

	var queued int
	if err := env.db.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM jobs WHERE type = $1 AND payload ->> 'photo_uid' = $2",
		jobs.TypeImageEmbed, photo.UID).Scan(&queued); err != nil {
		t.Fatalf("counting jobs: %v", err)
	}
	if queued != 1 {
		t.Errorf("two clicks left %d jobs, want 1", queued)
	}
}

// TestRunProcessingStep_refusals covers the three requests the endpoint turns
// away: a step that is not one of the reported ones, a photo that does not exist,
// and a step that could never apply to this photo.
func TestRunProcessingStep_refusals(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "maint", auth.RoleMaintainer)
	photo := env.seedPhoto(t, photos.Photo{Title: "refuse"}, "refuse.jpg", 15, 26, 37)

	tests := []struct {
		name string
		uid  string
		step string
		want int
	}{
		{name: "unknown step", uid: photo.UID, step: "nonsense", want: http.StatusBadRequest},
		{name: "storyboard is not a step", uid: photo.UID, step: "storyboard", want: http.StatusBadRequest},
		{name: "missing photo", uid: "does-not-exist", step: "thumbnail", want: http.StatusNotFound},
		{name: "places without GPS", uid: photo.UID, step: "places", want: http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := mustDo(t, client, http.MethodPost,
				env.server.URL+"/api/v1/photos/"+tt.uid+"/process/"+tt.step, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}
