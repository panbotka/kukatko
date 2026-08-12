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
}

// Enqueue records the call and returns the preset error.
func (f *fakeEnqueuer) Enqueue(
	_ context.Context, jobType string, payload json.RawMessage, opts EnqueueOptions,
) (Job, error) {
	f.calls++
	f.lastType = jobType
	f.lastPayload = payload
	f.lastOpts = opts
	if f.err != nil {
		return Job{}, f.err
	}
	return Job{Type: jobType, State: StateQueued}, nil
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

// TestEnqueueThumbnailRebuild_duplicateIsSuccess confirms the forced enqueue keeps
// the adapter's idempotency: a photo whose thumbnail job is already active is a
// no-op rather than an error the edit endpoint would log.
func TestEnqueueThumbnailRebuild_duplicateIsSuccess(t *testing.T) {
	t.Parallel()
	fake := &fakeEnqueuer{err: ErrDuplicate}
	if err := (&Enqueuer{store: fake}).EnqueueThumbnailRebuild(context.Background(), "ph1"); err != nil {
		t.Errorf("EnqueueThumbnailRebuild on a duplicate = %v, want nil", err)
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
