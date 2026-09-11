package ingest

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// countingHLS is an HLSEnqueuer that records the uids it was called for.
type countingHLS struct {
	uids []string
	err  error
}

// EnqueueHLSTranscode records the uid or reports the configured failure.
func (c *countingHLS) EnqueueHLSTranscode(_ context.Context, uid string) error {
	if c.err != nil {
		return c.err
	}
	c.uids = append(c.uids, uid)
	return nil
}

// TestEnqueueJobs_hls covers the states the `hls_transcode` enqueue can be in:
// on for a standalone video, off entirely (no enqueuer wired), and deliberately
// skipped for everything that is not a video — a still has nothing to segment and
// a live photo's motion clip is a hover preview nobody streams.
func TestEnqueueJobs_hls(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		photo   photos.Photo
		wireHLS bool
		want    []string
	}{
		"video enqueues": {
			photo:   photos.Photo{UID: "ph1", MediaType: photos.MediaVideo},
			wireHLS: true,
			want:    []string{"ph1"},
		},
		"still is skipped": {
			photo:   photos.Photo{UID: "ph2", MediaType: photos.MediaImage},
			wireHLS: true,
			want:    nil,
		},
		"live photo is skipped": {
			photo:   photos.Photo{UID: "ph3", MediaType: photos.MediaLive},
			wireHLS: true,
			want:    nil,
		},
		"feature off enqueues nothing": {
			photo:   photos.Photo{UID: "ph4", MediaType: photos.MediaVideo},
			wireHLS: false,
			want:    nil,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			enq := &countingEnqueuer{}
			hls := &countingHLS{}
			cfg := Config{Enqueuer: enq}
			if tc.wireHLS {
				cfg.HLS = hls
			}
			svc := New(cfg)

			if warnings := svc.enqueueJobs(context.Background(), tc.photo); warnings != nil {
				t.Fatalf("warnings = %v, want none", warnings)
			}
			if len(hls.uids) != len(tc.want) {
				t.Fatalf("hls enqueued %v, want %v", hls.uids, tc.want)
			}
			for i, uid := range tc.want {
				if hls.uids[i] != uid {
					t.Errorf("hls enqueued[%d] = %s, want %s", i, hls.uids[i], uid)
				}
			}
			// The embedding is scheduled regardless — the streaming encode is an
			// addition to that work, never a replacement for it. Face detection
			// is the mirror image of the encode: a still's job, never a clip's.
			if len(enq.embeds) != 1 || len(enq.faces) != wantFaceJobs(tc.photo) {
				t.Errorf("embeds=%v faces=%v, want 1 embed and %d faces",
					enq.embeds, enq.faces, wantFaceJobs(tc.photo))
			}
		})
	}
}

// TestEnqueueJobs_hlsFailureIsAWarning asserts a failed `hls_transcode` enqueue
// degrades the upload rather than failing it: the video is catalogued, and it can
// be scheduled for encoding later.
func TestEnqueueJobs_hlsFailureIsAWarning(t *testing.T) {
	t.Parallel()

	svc := New(Config{Enqueuer: &countingEnqueuer{}, HLS: &countingHLS{err: errors.New("queue down")}})
	warnings := svc.enqueueJobs(context.Background(), photos.Photo{UID: "ph1", MediaType: photos.MediaVideo})
	if len(warnings) != 1 || warnings[0].Code != warnEnqueueFailed {
		t.Fatalf("warnings = %+v, want one enqueue-failed warning", warnings)
	}
}
