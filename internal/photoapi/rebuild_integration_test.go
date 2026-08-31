//go:build integration

package photoapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/worker"
)

// fakeRebuilders stands in for the three job services behind the rebuild
// endpoints. It satisfies all three interfaces at once, so the env wires one
// value where production wires embedjob, facejob and placesjob.
type fakeRebuilders struct {
	// faces is what a forced re-detection reports afterwards.
	faces int
	// err, when set, is returned by all three — an offline deferral or a failure.
	err error
	// embeds, detects and geocodes count what was actually asked for.
	embeds, detects, geocodes int
}

// ForceEmbed records the call and returns the configured error.
func (f *fakeRebuilders) ForceEmbed(_ context.Context, _ string) error {
	f.embeds++
	return f.err
}

// ForceDetect records the call and returns the configured count/error.
func (f *fakeRebuilders) ForceDetect(_ context.Context, _ string) (int, error) {
	f.detects++
	return f.faces, f.err
}

// ForceGeocode records the call and returns the configured error.
func (f *fakeRebuilders) ForceGeocode(_ context.Context, _ string) error {
	f.geocodes++
	return f.err
}

// TestRebuildEndpoints_rbac pins the guard: a rebuild throws stored work away, so
// it is held to the stricter of its two neighbours — the maintainer role that
// guards POST /process/{step}, not the write role that guards
// regenerate-thumbnail.
func TestRebuildEndpoints_rbac(t *testing.T) {
	env := newEnv(t)
	photo := env.seedPhoto(t, photos.Photo{Title: "rbac"}, "rebuild-rbac.jpg", 41, 42, 43)

	paths := []string{"reembed", "redetect-faces", "regeocode"}
	roles := []struct {
		role auth.Role
		want int
	}{
		{role: auth.RoleViewer, want: http.StatusForbidden},
		{role: auth.RoleEditor, want: http.StatusForbidden},
		{role: auth.RoleAdmin, want: http.StatusForbidden},
		{role: auth.RoleMaintainer, want: http.StatusOK},
	}

	for _, path := range paths {
		for _, tt := range roles {
			t.Run(path+"/"+string(tt.role), func(t *testing.T) {
				client, _ := env.login(t, "rb-"+path+"-"+string(tt.role), tt.role)
				resp := mustDo(t, client, http.MethodPost,
					env.server.URL+"/api/v1/photos/"+photo.UID+"/"+path, nil)
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != tt.want {
					t.Errorf("%s status = %d, want %d", tt.role, resp.StatusCode, tt.want)
				}
			})
		}
		t.Run(path+"/anonymous", func(t *testing.T) {
			resp := mustDo(t, &http.Client{}, http.MethodPost,
				env.server.URL+"/api/v1/photos/"+photo.UID+"/"+path, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("anonymous status = %d, want 401", resp.StatusCode)
			}
		})
	}
}

// TestRedetectFaces_reportsAndAudits walks the endpoint end to end: the
// recomputation runs, the answer carries the resulting face count, and the trail
// records who threw the previous detection away.
func TestRedetectFaces_reportsAndAudits(t *testing.T) {
	env := newEnv(t)
	env.rebuilds.faces = 4
	client, _ := env.login(t, "rb-audit", auth.RoleMaintainer)
	photo := env.seedPhoto(t, photos.Photo{Title: "audited"}, "rebuild-audit.jpg", 51, 52, 53)

	resp := mustDo(t, client, http.MethodPost,
		env.server.URL+"/api/v1/photos/"+photo.UID+"/redetect-faces", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Step   string `json:"step"`
		Status string `json:"status"`
		Faces  *int   `json:"faces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the rebuild response: %v", err)
	}
	if body.Step != "face_detect" || body.Status != "rebuilt" || body.Faces == nil || *body.Faces != 4 {
		t.Errorf("body = %+v, want a rebuilt face_detect reporting 4 faces", body)
	}
	if env.rebuilds.detects != 1 {
		t.Errorf("detections run = %d, want 1", env.rebuilds.detects)
	}

	entries, err := audit.NewStore(env.db.Pool()).List(t.Context(), audit.Filter{Limit: 50})
	if err != nil {
		t.Fatalf("listing the audit trail: %v", err)
	}
	if !hasAuditAction(entries, audit.ActionPhotoFaces, photo.UID) {
		t.Errorf("the trail carries no %s entry for %s", audit.ActionPhotoFaces, photo.UID)
	}
}

// TestReembed_offlineQueuesTheForcedJob is the offline contract, against the real
// queue: with the sidecar down the request answers "queued" and leaves exactly one
// forced image_embed job behind — and a second request is absorbed by the dedup
// index rather than piling a second one on.
func TestReembed_offlineQueuesTheForcedJob(t *testing.T) {
	env := newEnv(t)
	env.rebuilds.err = worker.RetryAfter(time.Minute, embedding.ErrUnavailable)
	client, _ := env.login(t, "rb-offline", auth.RoleMaintainer)
	photo := env.seedPhoto(t, photos.Photo{Title: "offline"}, "rebuild-offline.jpg", 61, 62, 63)

	for range 2 {
		resp := mustDo(t, client, http.MethodPost,
			env.server.URL+"/api/v1/photos/"+photo.UID+"/reembed", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 — an offline sidecar queues, it does not fail", resp.StatusCode)
		}
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decoding the rebuild response: %v", err)
		}
		_ = resp.Body.Close()
		if body.Status != "queued" {
			t.Fatalf("status = %q, want queued", body.Status)
		}
	}

	unfinished, err := env.jobs.UnfinishedForPhoto(t.Context(), photo.UID)
	if err != nil {
		t.Fatalf("UnfinishedForPhoto: %v", err)
	}
	forced := 0
	for _, job := range unfinished {
		if job.Type != jobs.TypeImageEmbed {
			continue
		}
		forced++
		var payload struct {
			Force bool `json:"force"`
		}
		if unmarshalErr := json.Unmarshal(job.Payload, &payload); unmarshalErr != nil {
			t.Fatalf("decoding the job payload: %v", unmarshalErr)
		}
		if !payload.Force {
			t.Error("the queued image_embed job is not forced; it would skip the photo again")
		}
	}
	if forced != 1 {
		t.Errorf("queued %d image_embed jobs, want 1 — two requests must dedupe into one", forced)
	}
}

// hasAuditAction reports whether the trail carries an entry for action on
// targetUID.
func hasAuditAction(entries []audit.Record, action, targetUID string) bool {
	for _, entry := range entries {
		if entry.Action == action && entry.TargetUID != nil && *entry.TargetUID == targetUID {
			return true
		}
	}
	return false
}

// postReembed drives the reembed endpoint for one photo and returns the status
// code and the decoded status word (empty when the body is not a rebuild).
func postReembed(t *testing.T, client *http.Client, url, photoUID string) (int, string) {
	t.Helper()
	resp := mustDo(t, client, http.MethodPost, url+"/api/v1/photos/"+photoUID+"/reembed", nil)
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body.Status
}

// queuedEmbedPayload returns the payload of the photo's single unfinished
// image_embed job, failing when there is not exactly one — the dedup invariant
// the upgrade must not break.
func queuedEmbedPayload(t *testing.T, store *jobs.Store, photoUID string) json.RawMessage {
	t.Helper()
	unfinished, err := store.UnfinishedForPhoto(t.Context(), photoUID)
	if err != nil {
		t.Fatalf("UnfinishedForPhoto: %v", err)
	}
	var found []json.RawMessage
	for _, job := range unfinished {
		if job.Type == jobs.TypeImageEmbed {
			found = append(found, job.Payload)
		}
	}
	if len(found) != 1 {
		t.Fatalf("unfinished image_embed jobs = %d, want exactly 1", len(found))
	}
	return found[0]
}

// isForcedPayload reports whether a job payload carries the force flag the
// handlers branch on.
func isForcedPayload(t *testing.T, payload json.RawMessage) bool {
	t.Helper()
	var decoded struct {
		Force bool `json:"force"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decoding the job payload %q: %v", payload, err)
	}
	return decoded.Force
}

// TestReembed_upgradesAQueuedPlainJob is the trap this endpoint was built to
// avoid, closed at its last hole: POST /process/image_embed leaves a plain repair
// job queued even for a photo that already has an embedding, and with the sidecar
// asleep the rebuild falls back to the same queue. The forced payload used to be
// dropped by dedup, leaving that plain job to take its idempotent skip — an
// operator watching a success that recomputed nothing. It must upgrade the queued
// job instead, and still leave exactly one.
func TestReembed_upgradesAQueuedPlainJob(t *testing.T) {
	env := newEnv(t)
	env.rebuilds.err = worker.RetryAfter(time.Minute, embedding.ErrUnavailable)
	client, _ := env.login(t, "rb-upgrade", auth.RoleMaintainer)
	photo := env.seedPhoto(t, photos.Photo{Title: "upgrade"}, "rebuild-upgrade.jpg", 71, 72, 73)

	if err := jobs.NewEnqueuer(env.jobs).EnqueueImageEmbed(t.Context(), photo.UID); err != nil {
		t.Fatalf("queueing the plain repair job: %v", err)
	}

	status, word := postReembed(t, client, env.server.URL, photo.UID)
	if status != http.StatusOK || word != "queued" {
		t.Fatalf("status = %d/%q, want 200/queued", status, word)
	}
	if payload := queuedEmbedPayload(t, env.jobs, photo.UID); !isForcedPayload(t, payload) {
		t.Errorf("the queued job's payload is %s, want the forced one — that job would skip the photo", payload)
	}
}

// TestReembed_runningJobIsAConflict is the collision the queue cannot resolve: a
// worker already claimed the photo's image_embed job and is running it with the
// payload it read then, so the force can be neither inserted nor written onto it.
// Answering "queued" there would be the very no-op the endpoint exists to kill,
// so it is a 409 asking for a retry once the run finishes.
func TestReembed_runningJobIsAConflict(t *testing.T) {
	env := newEnv(t)
	env.rebuilds.err = worker.RetryAfter(time.Minute, embedding.ErrUnavailable)
	client, _ := env.login(t, "rb-inflight", auth.RoleMaintainer)
	photo := env.seedPhoto(t, photos.Photo{Title: "in flight"}, "rebuild-inflight.jpg", 81, 82, 83)

	if err := jobs.NewEnqueuer(env.jobs).EnqueueImageEmbed(t.Context(), photo.UID); err != nil {
		t.Fatalf("queueing the plain repair job: %v", err)
	}
	if _, err := env.jobs.Claim(t.Context(), "worker-1", jobs.TypeImageEmbed); err != nil {
		t.Fatalf("claiming the job: %v", err)
	}

	if status, word := postReembed(t, client, env.server.URL, photo.UID); status != http.StatusConflict {
		t.Errorf("status = %d/%q, want %d — a force that will not happen is not a success",
			status, word, http.StatusConflict)
	}
	if payload := queuedEmbedPayload(t, env.jobs, photo.UID); isForcedPayload(t, payload) {
		t.Error("the running job's payload was forced; the flag would apply to a run that already read it")
	}
}
