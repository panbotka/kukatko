package ingest

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// countingPlaces is a PlacesEnqueuer that records the uids it was called for.
type countingPlaces struct {
	uids []string
	err  error
}

func (c *countingPlaces) EnqueuePlaces(_ context.Context, uid string) error {
	if c.err != nil {
		return c.err
	}
	c.uids = append(c.uids, uid)
	return nil
}

// TestEnqueueJobs_places covers every state the `places` enqueue can be in: a
// still or a video that arrived with coordinates schedules the geocode, one
// without them schedules nothing (there is nothing to look up), and an instance
// with no mapy.com key schedules nothing at all — no `places` handler is
// registered there, so a queued job would wait forever.
func TestEnqueueJobs_places(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		photo      photos.Photo
		wirePlaces bool
		want       []string
	}{
		"still with coordinates enqueues": {
			photo:      photos.Photo{UID: "ph1", MediaType: photos.MediaImage, Lat: new(49.3), Lng: new(16.7)},
			wirePlaces: true,
			want:       []string{"ph1"},
		},
		"video with coordinates enqueues": {
			photo:      photos.Photo{UID: "ph2", MediaType: photos.MediaVideo, Lat: new(49.3), Lng: new(16.7)},
			wirePlaces: true,
			want:       []string{"ph2"},
		},
		"no coordinates enqueues nothing": {
			photo:      photos.Photo{UID: "ph3", MediaType: photos.MediaImage},
			wirePlaces: true,
			want:       nil,
		},
		"latitude alone enqueues nothing": {
			photo:      photos.Photo{UID: "ph4", MediaType: photos.MediaImage, Lat: new(49.3)},
			wirePlaces: true,
			want:       nil,
		},
		"geocoding unconfigured enqueues nothing": {
			photo:      photos.Photo{UID: "ph5", MediaType: photos.MediaImage, Lat: new(49.3), Lng: new(16.7)},
			wirePlaces: false,
			want:       nil,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			enq := &countingEnqueuer{}
			places := &countingPlaces{}
			cfg := Config{Enqueuer: enq}
			if tc.wirePlaces {
				cfg.Places = places
			}
			svc := New(cfg)

			if warnings := svc.enqueueJobs(context.Background(), tc.photo); warnings != nil {
				t.Fatalf("warnings = %v, want none", warnings)
			}
			if len(places.uids) != len(tc.want) {
				t.Fatalf("places enqueued %v, want %v", places.uids, tc.want)
			}
			for i, uid := range tc.want {
				if places.uids[i] != uid {
					t.Errorf("places enqueued[%d] = %s, want %s", i, places.uids[i], uid)
				}
			}
			// The geocode is an addition to the always-scheduled work, never a
			// replacement for it.
			if len(enq.embeds) != 1 || len(enq.faces) != 1 {
				t.Errorf("embeds=%v faces=%v, want one of each", enq.embeds, enq.faces)
			}
		})
	}
}

// TestEnqueueJobs_placesFailureIsAWarning asserts a failed `places` enqueue
// degrades the upload rather than failing it: the photo is catalogued, and the
// maintenance backfill can schedule the geocode later.
func TestEnqueueJobs_placesFailureIsAWarning(t *testing.T) {
	t.Parallel()

	svc := New(Config{
		Enqueuer: &countingEnqueuer{},
		Places:   &countingPlaces{err: errors.New("queue down")},
	})
	photo := photos.Photo{UID: "ph1", MediaType: photos.MediaImage, Lat: new(49.3), Lng: new(16.7)}
	warnings := svc.enqueueJobs(context.Background(), photo)
	if len(warnings) != 1 || warnings[0].Code != warnEnqueueFailed {
		t.Fatalf("warnings = %+v, want one enqueue-failed warning", warnings)
	}
}
