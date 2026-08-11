package ingest

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// countingEnqueuer is a JobEnqueuer that records the uids it was called for.
type countingEnqueuer struct {
	embeds []string
	faces  []string
}

func (c *countingEnqueuer) EnqueueImageEmbed(_ context.Context, uid string) error {
	c.embeds = append(c.embeds, uid)
	return nil
}

func (c *countingEnqueuer) EnqueueFaceDetect(_ context.Context, uid string) error {
	c.faces = append(c.faces, uid)
	return nil
}

// countingOCR is an OCREnqueuer that records the uids it was called for.
type countingOCR struct {
	uids []string
	err  error
}

func (c *countingOCR) EnqueueOCR(_ context.Context, uid string) error {
	if c.err != nil {
		return c.err
	}
	c.uids = append(c.uids, uid)
	return nil
}

// TestEnqueueJobs_ocr covers the three states the `ocr` enqueue can be in: on for
// a still, off entirely (no enqueuer wired), and deliberately skipped for a video
// — there is no poster-frame recognition.
func TestEnqueueJobs_ocr(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		photo   photos.Photo
		wireOCR bool
		want    []string
	}{
		"still enqueues": {
			photo:   photos.Photo{UID: "ph1", MediaType: photos.MediaImage},
			wireOCR: true,
			want:    []string{"ph1"},
		},
		"live photo enqueues": {
			photo:   photos.Photo{UID: "ph2", MediaType: photos.MediaLive},
			wireOCR: true,
			want:    []string{"ph2"},
		},
		"video is skipped": {
			photo:   photos.Photo{UID: "ph3", MediaType: photos.MediaVideo},
			wireOCR: true,
			want:    nil,
		},
		"feature off enqueues nothing": {
			photo:   photos.Photo{UID: "ph4", MediaType: photos.MediaImage},
			wireOCR: false,
			want:    nil,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			enq := &countingEnqueuer{}
			ocr := &countingOCR{}
			cfg := Config{Enqueuer: enq}
			if tc.wireOCR {
				cfg.OCR = ocr
			}
			svc := New(cfg)

			if warnings := svc.enqueueJobs(context.Background(), tc.photo); warnings != nil {
				t.Fatalf("warnings = %v, want none", warnings)
			}
			if len(ocr.uids) != len(tc.want) {
				t.Fatalf("ocr enqueued %v, want %v", ocr.uids, tc.want)
			}
			for i, uid := range tc.want {
				if ocr.uids[i] != uid {
					t.Errorf("ocr enqueued[%d] = %s, want %s", i, ocr.uids[i], uid)
				}
			}
			// The embedding and face jobs are scheduled regardless — OCR is an
			// addition to that work, never a replacement for it.
			if len(enq.embeds) != 1 || len(enq.faces) != 1 {
				t.Errorf("embeds=%v faces=%v, want one of each", enq.embeds, enq.faces)
			}
		})
	}
}

// TestEnqueueJobs_ocrFailureIsAWarning asserts a failed `ocr` enqueue degrades the
// upload rather than failing it: the photo is catalogued, and the backfill can
// pick the recognition up later.
func TestEnqueueJobs_ocrFailureIsAWarning(t *testing.T) {
	t.Parallel()

	svc := New(Config{Enqueuer: &countingEnqueuer{}, OCR: &countingOCR{err: errors.New("queue down")}})
	warnings := svc.enqueueJobs(context.Background(), photos.Photo{UID: "ph1", MediaType: photos.MediaImage})
	if len(warnings) != 1 || warnings[0].Code != warnEnqueueFailed {
		t.Fatalf("warnings = %+v, want one enqueue-failed warning", warnings)
	}
}
