package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestPayloadOrEmpty verifies the JSONB fallback used for an absent payload.
func TestPayloadOrEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{name: "nil yields empty object", raw: nil, want: "{}"},
		{name: "empty yields empty object", raw: json.RawMessage{}, want: "{}"},
		{name: "value passes through", raw: json.RawMessage(`{"photo_uid":"ph1"}`), want: `{"photo_uid":"ph1"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := string(payloadOrEmpty(tt.raw)); got != tt.want {
				t.Errorf("payloadOrEmpty(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// TestPhotoPayload verifies the canonical dedup payload shape.
func TestPhotoPayload(t *testing.T) {
	t.Parallel()

	raw, err := photoPayload("ph123")
	if err != nil {
		t.Fatalf("photoPayload: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["photo_uid"] != "ph123" {
		t.Errorf("photo_uid = %q, want ph123", decoded["photo_uid"])
	}
}

// TestClaimSQL verifies the claim statement always uses SKIP LOCKED and the
// priority/FIFO ordering, and that the type filter is added only when requested.
func TestClaimSQL(t *testing.T) {
	t.Parallel()

	unfiltered := claimSQL(false)
	for _, want := range []string{"FOR UPDATE SKIP LOCKED", "ORDER BY priority DESC, run_after ASC, id ASC"} {
		if !strings.Contains(unfiltered, want) {
			t.Errorf("claimSQL(false) missing %q in:\n%s", want, unfiltered)
		}
	}
	if strings.Contains(unfiltered, "type = ANY") {
		t.Errorf("claimSQL(false) should not filter by type:\n%s", unfiltered)
	}
	if filtered := claimSQL(true); !strings.Contains(filtered, "type = ANY($2)") {
		t.Errorf("claimSQL(true) should filter by type:\n%s", filtered)
	}
}

// fakeEnqueuer is a photoEnqueuer stub recording the last enqueue and returning a
// preset result, used to unit-test the Enqueuer adapter without a database.
type fakeEnqueuer struct {
	err         error
	lastType    string
	lastPayload json.RawMessage
	lastOpts    EnqueueOptions
	calls       int
	// enqueueErrs, when non-empty, answers the first Enqueue calls one entry at a
	// time before err takes over again. It is how a collision that resolves itself
	// between two attempts is scripted.
	enqueueErrs []error
	// upgrade is what UpgradeToForced reports, and upgradeErr the error it fails
	// with instead. upgradeErrs scripts the first calls the way enqueueErrs does.
	upgrade         ForceOutcome
	upgradeErr      error
	upgradeErrs     []error
	upgrades        int
	upgradePayloads []json.RawMessage
}

// Enqueue records the call and returns the preset error.
func (f *fakeEnqueuer) Enqueue(
	_ context.Context, jobType string, payload json.RawMessage, opts EnqueueOptions,
) (Job, error) {
	f.calls++
	f.lastType = jobType
	f.lastPayload = payload
	f.lastOpts = opts
	if err := nextErr(&f.enqueueErrs, f.err); err != nil {
		return Job{}, err
	}
	return Job{Type: jobType, State: StateQueued}, nil
}

// UpgradeToForced records the payload the adapter tried to force onto the
// colliding job and returns the preset outcome or error.
func (f *fakeEnqueuer) UpgradeToForced(
	_ context.Context, jobType, _ string, payload json.RawMessage,
) (ForceOutcome, error) {
	f.upgrades++
	f.lastType = jobType
	f.upgradePayloads = append(f.upgradePayloads, payload)
	if err := nextErr(&f.upgradeErrs, f.upgradeErr); err != nil {
		return "", err
	}
	return f.upgrade, nil
}

// nextErr pops the next scripted error from scripted, falling back to fallback
// once the script is exhausted.
func nextErr(scripted *[]error, fallback error) error {
	if len(*scripted) == 0 {
		return fallback
	}
	err := (*scripted)[0]
	*scripted = (*scripted)[1:]
	return err
}

// TestEnqueueSidecar_debounces verifies the sidecar enqueue maps to TypeSidecar
// and delays the job by SidecarDebounce — the queued-state coalescing window that
// collapses a burst of edits into a single file write and keeps the follow-up a
// scoped dedup schedules (migration 0044) from becoming a tight rewrite loop.
func TestEnqueueSidecar_debounces(t *testing.T) {
	t.Parallel()

	pinned := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	fake := &fakeEnqueuer{}
	enq := &Enqueuer{store: fake, clock: func() time.Time { return pinned }}

	if err := enq.EnqueueSidecar(context.Background(), "ph1"); err != nil {
		t.Fatalf("EnqueueSidecar: %v", err)
	}
	if fake.lastType != TypeSidecar {
		t.Errorf("lastType = %q, want %q", fake.lastType, TypeSidecar)
	}
	if fake.lastOpts.RunAfter == nil {
		t.Fatal("RunAfter is nil, want the debounce delay")
	}
	if want := pinned.Add(SidecarDebounce); !fake.lastOpts.RunAfter.Equal(want) {
		t.Errorf("RunAfter = %v, want %v (now + SidecarDebounce)", *fake.lastOpts.RunAfter, want)
	}
}

// TestEnqueueThumbnail_plainVsRebuild verifies the two thumbnail enqueues differ
// only in the payload's force flag: both are TypeThumbnail carrying the photo_uid
// the dedup index keys on (so a forced job dedupes against a plain one), and only
// the rebuild asks the handler to overwrite what is already cached.
func TestEnqueueThumbnail_plainVsRebuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		enqueue   func(*Enqueuer) error
		wantForce bool
	}{
		{
			name:      "repair leaves cached sizes alone",
			enqueue:   func(e *Enqueuer) error { return e.EnqueueThumbnail(context.Background(), "ph1") },
			wantForce: false,
		},
		{
			name:      "rebuild forces every size",
			enqueue:   func(e *Enqueuer) error { return e.EnqueueThumbnailRebuild(context.Background(), "ph1") },
			wantForce: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeEnqueuer{}
			if err := tt.enqueue(&Enqueuer{store: fake}); err != nil {
				t.Fatalf("enqueue: %v", err)
			}
			if fake.lastType != TypeThumbnail {
				t.Errorf("lastType = %q, want %q", fake.lastType, TypeThumbnail)
			}
			var decoded struct {
				PhotoUID string `json:"photo_uid"`
				Force    bool   `json:"force"`
			}
			if err := json.Unmarshal(fake.lastPayload, &decoded); err != nil {
				t.Fatalf("unmarshal payload %q: %v", fake.lastPayload, err)
			}
			if decoded.PhotoUID != "ph1" {
				t.Errorf("payload photo_uid = %q, want ph1", decoded.PhotoUID)
			}
			if decoded.Force != tt.wantForce {
				t.Errorf("payload force = %v, want %v", decoded.Force, tt.wantForce)
			}
		})
	}
}

// TestEnqueueRebuilds_forcePayloadPerType verifies every rebuild enqueue maps to
// the job type its repair uses and carries the force flag beside the photo_uid the
// dedup index keys on. Sharing the type is the design: at most one active job per
// photo per type survives, so a rebuild request cannot pile a second job onto a
// photo the queue is already working on.
func TestEnqueueRebuilds_forcePayloadPerType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enqueue  func(*Enqueuer) error
		wantType string
	}{
		{
			name:     "thumbnail",
			enqueue:  func(e *Enqueuer) error { return e.EnqueueThumbnailRebuild(context.Background(), "ph1") },
			wantType: TypeThumbnail,
		},
		{
			name:     "image embed",
			enqueue:  func(e *Enqueuer) error { _, err := e.EnqueueImageEmbedRebuild(context.Background(), "ph1"); return err },
			wantType: TypeImageEmbed,
		},
		{
			name:     "face detect",
			enqueue:  func(e *Enqueuer) error { _, err := e.EnqueueFaceDetectRebuild(context.Background(), "ph1"); return err },
			wantType: TypeFaceDetect,
		},
		{
			name:     "places",
			enqueue:  func(e *Enqueuer) error { _, err := e.EnqueuePlacesRebuild(context.Background(), "ph1"); return err },
			wantType: TypePlaces,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeEnqueuer{}
			if err := tt.enqueue(&Enqueuer{store: fake}); err != nil {
				t.Fatalf("enqueue: %v", err)
			}
			if fake.lastType != tt.wantType {
				t.Errorf("lastType = %q, want %q", fake.lastType, tt.wantType)
			}
			var decoded struct {
				PhotoUID string `json:"photo_uid"`
				Force    bool   `json:"force"`
			}
			if err := json.Unmarshal(fake.lastPayload, &decoded); err != nil {
				t.Fatalf("unmarshal payload %q: %v", fake.lastPayload, err)
			}
			if decoded.PhotoUID != "ph1" || !decoded.Force {
				t.Errorf("payload = %+v, want photo_uid ph1 with force set", decoded)
			}
		})
	}
}

// TestEnqueueRebuilds_collisionOutcomes is the point of the forced enqueue: a
// rebuild that hits the dedup index is not simply "success". The job it collided
// with decides what happened — a queued plain job is upgraded to the forced
// payload, an already-forced one absorbs the request, and a running one cannot be
// touched at all — and the caller is told which, because only the last of the
// three means the force is not going to happen.
func TestEnqueueRebuilds_collisionOutcomes(t *testing.T) {
	t.Parallel()

	enqueues := map[string]func(*Enqueuer) (ForceOutcome, error){
		"image embed": func(e *Enqueuer) (ForceOutcome, error) {
			return e.EnqueueImageEmbedRebuild(context.Background(), "ph1")
		},
		"face detect": func(e *Enqueuer) (ForceOutcome, error) {
			return e.EnqueueFaceDetectRebuild(context.Background(), "ph1")
		},
		"places": func(e *Enqueuer) (ForceOutcome, error) {
			return e.EnqueuePlacesRebuild(context.Background(), "ph1")
		},
	}
	outcomes := []ForceOutcome{ForceUpgraded, ForceAbsorbed, ForceInFlight}

	for name, enqueue := range enqueues {
		for _, want := range outcomes {
			t.Run(name+"/"+string(want), func(t *testing.T) {
				t.Parallel()
				fake := &fakeEnqueuer{err: ErrDuplicate, upgrade: want}
				got, err := enqueue(&Enqueuer{store: fake})
				if err != nil {
					t.Fatalf("rebuild enqueue on a duplicate = %v, want nil", err)
				}
				if got != want {
					t.Errorf("outcome = %q, want %q", got, want)
				}
				if fake.upgrades != 1 {
					t.Errorf("upgrade attempts = %d, want 1 — a collision must be resolved, not swallowed",
						fake.upgrades)
				}
			})
		}
	}
}

// TestEnqueueRebuilds_forcesTheCollidingJob pins what the upgrade writes: the
// same forced payload the insert would have carried, so the job that was already
// queued redoes the work instead of taking its idempotent skip.
func TestEnqueueRebuilds_forcesTheCollidingJob(t *testing.T) {
	t.Parallel()

	fake := &fakeEnqueuer{err: ErrDuplicate, upgrade: ForceUpgraded}
	if _, err := (&Enqueuer{store: fake}).EnqueueImageEmbedRebuild(context.Background(), "ph1"); err != nil {
		t.Fatalf("EnqueueImageEmbedRebuild: %v", err)
	}
	if len(fake.upgradePayloads) != 1 {
		t.Fatalf("upgrade payloads = %d, want 1", len(fake.upgradePayloads))
	}
	var decoded struct {
		PhotoUID string `json:"photo_uid"`
		Force    bool   `json:"force"`
	}
	if err := json.Unmarshal(fake.upgradePayloads[0], &decoded); err != nil {
		t.Fatalf("unmarshal upgrade payload %q: %v", fake.upgradePayloads[0], err)
	}
	if decoded.PhotoUID != "ph1" || !decoded.Force {
		t.Errorf("upgrade payload = %+v, want photo_uid ph1 with force set", decoded)
	}
}

// TestEnqueueRebuilds_scheduledWhenNothingCollides covers the ordinary case: an
// idle photo takes the insert and no upgrade is attempted at all.
func TestEnqueueRebuilds_scheduledWhenNothingCollides(t *testing.T) {
	t.Parallel()

	fake := &fakeEnqueuer{}
	got, err := (&Enqueuer{store: fake}).EnqueueFaceDetectRebuild(context.Background(), "ph1")
	if err != nil {
		t.Fatalf("EnqueueFaceDetectRebuild: %v", err)
	}
	if got != ForceScheduled {
		t.Errorf("outcome = %q, want %q", got, ForceScheduled)
	}
	if fake.upgrades != 0 {
		t.Errorf("upgrade attempts = %d, want 0", fake.upgrades)
	}
}

// TestEnqueueRebuilds_retriesAVanishedCollision covers the window between the two
// statements: the insert loses to the dedup index, but by the time the upgrade
// runs the colliding job has finished and there is nothing left to rewrite. The
// forced job must then be inserted after all rather than reported as a collision
// that no longer exists.
func TestEnqueueRebuilds_retriesAVanishedCollision(t *testing.T) {
	t.Parallel()

	fake := &fakeEnqueuer{
		enqueueErrs: []error{ErrDuplicate},
		upgradeErrs: []error{ErrNoActiveJob},
	}
	got, err := (&Enqueuer{store: fake}).EnqueuePlacesRebuild(context.Background(), "ph1")
	if err != nil {
		t.Fatalf("EnqueuePlacesRebuild: %v", err)
	}
	if got != ForceScheduled {
		t.Errorf("outcome = %q, want %q", got, ForceScheduled)
	}
	if fake.calls != 2 {
		t.Errorf("enqueue attempts = %d, want 2 — the retry is what schedules the job", fake.calls)
	}
}

// TestEnqueueRebuilds_racedOut bounds that retry: a queue where every insert
// collides and every collision is gone by the time the upgrade looks reports a
// failure instead of looping.
func TestEnqueueRebuilds_racedOut(t *testing.T) {
	t.Parallel()

	fake := &fakeEnqueuer{err: ErrDuplicate, upgradeErr: ErrNoActiveJob}
	_, err := (&Enqueuer{store: fake}).EnqueueImageEmbedRebuild(context.Background(), "ph1")
	if !errors.Is(err, ErrEnqueueRaced) {
		t.Errorf("error = %v, want ErrEnqueueRaced", err)
	}
	if fake.calls != forceEnqueueAttempts {
		t.Errorf("enqueue attempts = %d, want %d", fake.calls, forceEnqueueAttempts)
	}
}

// TestEnqueueRebuilds_propagatesFailures keeps the two real failures distinct
// from a collision: an insert that fails for any other reason, and an upgrade
// that fails outright, are both errors rather than a quiet "queued".
func TestEnqueueRebuilds_propagatesFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	tests := []struct {
		name string
		fake *fakeEnqueuer
	}{
		{name: "insert fails", fake: &fakeEnqueuer{err: boom}},
		{name: "upgrade fails", fake: &fakeEnqueuer{err: ErrDuplicate, upgradeErr: boom}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := (&Enqueuer{store: tt.fake}).EnqueueImageEmbedRebuild(context.Background(), "ph1"); !errors.Is(err, boom) {
				t.Errorf("error = %v, want %v", err, boom)
			}
		})
	}
}

// TestEnqueueThumbnailRebuild_collisionIsSuccess confirms the thumbnail rebuild
// keeps the adapter's idempotency: it shares the forced enqueue with the other
// three but has no caller to report an outcome to, so every collision — an
// upgraded queued job or a run already in flight — is still a no-op rather than
// an error the edit endpoint would log.
func TestEnqueueThumbnailRebuild_collisionIsSuccess(t *testing.T) {
	t.Parallel()

	for _, outcome := range []ForceOutcome{ForceUpgraded, ForceAbsorbed, ForceInFlight} {
		t.Run(string(outcome), func(t *testing.T) {
			t.Parallel()
			fake := &fakeEnqueuer{err: ErrDuplicate, upgrade: outcome}
			if err := (&Enqueuer{store: fake}).EnqueueThumbnailRebuild(context.Background(), "ph1"); err != nil {
				t.Errorf("EnqueueThumbnailRebuild on a %s collision = %v, want nil", outcome, err)
			}
		})
	}
}

// TestEnqueuer verifies the adapter maps each method to the right job type and
// treats a dedup hit as success while propagating other errors.
func TestEnqueuer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fakeErr error
		wantErr error
	}{
		{name: "success", fakeErr: nil, wantErr: nil},
		{name: "duplicate is swallowed", fakeErr: ErrDuplicate, wantErr: nil},
		{name: "other error propagates", fakeErr: errors.New("boom"), wantErr: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeEnqueuer{err: tt.fakeErr}
			enq := &Enqueuer{store: fake}

			err := enq.EnqueueImageEmbed(context.Background(), "ph1")
			assertEnqueueErr(t, "EnqueueImageEmbed", err, tt.fakeErr)
			if fake.lastType != TypeImageEmbed {
				t.Errorf("lastType = %q, want %q", fake.lastType, TypeImageEmbed)
			}

			if err := enq.EnqueueFaceDetect(context.Background(), "ph1"); fake.lastType != TypeFaceDetect {
				t.Errorf("EnqueueFaceDetect lastType = %q (err=%v), want %q", fake.lastType, err, TypeFaceDetect)
			}

			if err := enq.EnqueuePlaces(context.Background(), "ph1"); fake.lastType != TypePlaces {
				t.Errorf("EnqueuePlaces lastType = %q (err=%v), want %q", fake.lastType, err, TypePlaces)
			}
		})
	}
}

// assertEnqueueErr checks the adapter's error handling: ErrDuplicate and nil
// become nil, any other error is returned unchanged.
func assertEnqueueErr(t *testing.T, op string, got, fakeErr error) {
	t.Helper()
	switch {
	case fakeErr == nil || errors.Is(fakeErr, ErrDuplicate):
		if got != nil {
			t.Errorf("%s error = %v, want nil", op, got)
		}
	default:
		if !errors.Is(got, fakeErr) {
			t.Errorf("%s error = %v, want %v", op, got, fakeErr)
		}
	}
}
