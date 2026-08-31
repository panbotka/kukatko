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
