package main

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/system"
)

// TestLibrarySnapshot_MapsEveryCount verifies the adapter between the shared
// library aggregation and the metric shape: every count lands under the label
// value an operator queries by, and the plain-image count comes from the derived
// field rather than being recomputed here.
func TestLibrarySnapshot_MapsEveryCount(t *testing.T) {
	t.Parallel()

	counts := system.Library{
		Photos: 30, Images: 25, Videos: 4, LivePhotos: 1, PhotosArchived: 3,
		PhotosWithEmbedding: 28, PhotosWithoutEmbedding: 2,
		PhotosWithFaces: 20, PhotosWithoutFaces: 10,
		PhotosGeocoded: 12, PhotosPendingGeocode: 5,
		Embeddings: 28, Faces: 77,
		MarkersAssigned: 40, MarkersUnassigned: 9,
		SubjectsPerson: 6, SubjectsPet: 2, SubjectsOther: 1,
		AlbumsManual: 3, AlbumsFolder: 2, AlbumsMoment: 1, AlbumsState: 4, AlbumsMonth: 5,
		Labels: 11,
	}

	got := librarySnapshot(counts)
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"photos image", got.PhotosByMediaType["image"], 25},
		{"photos video", got.PhotosByMediaType["video"], 4},
		{"photos live", got.PhotosByMediaType["live"], 1},
		{"archived", got.PhotosArchived, 3},
		{"processed embedding", got.PhotosProcessed[metrics.StageEmbedding], 28},
		{"processed faces", got.PhotosProcessed[metrics.StageFaces], 20},
		{"processed places", got.PhotosProcessed[metrics.StagePlaces], 12},
		{"pending embedding", got.PhotosPending[metrics.StageEmbedding], 2},
		{"pending faces", got.PhotosPending[metrics.StageFaces], 10},
		{"pending places", got.PhotosPending[metrics.StagePlaces], 5},
		{"embeddings", got.Embeddings, 28},
		{"faces", got.Faces, 77},
		{"markers assigned", got.MarkersByState[metrics.MarkerAssigned], 40},
		{"markers unassigned", got.MarkersByState[metrics.MarkerUnassigned], 9},
		{"subjects person", got.SubjectsByType["person"], 6},
		{"albums manual", got.AlbumsByType["album"], 3},
		{"albums folder", got.AlbumsByType["folder"], 2},
		{"albums moment", got.AlbumsByType["moment"], 1},
		{"albums state", got.AlbumsByType["state"], 4},
		{"albums month", got.AlbumsByType["month"], 5},
		{"labels", got.Labels, 11},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %d, want %d", check.name, check.got, check.want)
		}
	}
}

// TestImportRuns_CarriesStatusVocabulary verifies each exported run carries the
// full status vocabulary (so the collector can zero the statuses it is not in),
// leaves a still-running run without a finish time, and skips sources that have
// never run.
func TestImportRuns_CarriesStatusVocabulary(t *testing.T) {
	t.Parallel()

	finished := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	runs := map[importer.Source]importer.Run{
		importer.SourcePhotoPrism: {
			Source: importer.SourcePhotoPrism, Status: importer.StatusPartial,
			StartedAt: finished.Add(-time.Hour), FinishedAt: &finished,
		},
		importer.SourceFolder: {
			Source: importer.SourceFolder, Status: importer.StatusRunning,
			StartedAt: finished,
		},
	}

	got := importRuns(runs)
	if len(got) != 2 {
		t.Fatalf("importRuns() returned %d runs, want 2: %+v", len(got), got)
	}
	// AllSources order puts photoprism before folder, so the export is stable.
	if got[0].Source != string(importer.SourcePhotoPrism) || got[1].Source != string(importer.SourceFolder) {
		t.Errorf("importRuns() = %+v, want photoprism then folder", got)
	}
	if got[0].Status != string(importer.StatusPartial) {
		t.Errorf("photoprism status = %q, want partial", got[0].Status)
	}
	if len(got[0].Statuses) != len(importer.AllStatuses()) {
		t.Errorf("statuses = %v, want the full vocabulary", got[0].Statuses)
	}
	if !got[0].FinishedAt.Equal(finished) {
		t.Errorf("photoprism finished at %v, want %v", got[0].FinishedAt, finished)
	}
	if !got[1].FinishedAt.IsZero() {
		t.Errorf("a running import must have no finish time, got %v", got[1].FinishedAt)
	}
}

// TestImportRuns_NoRuns verifies a fresh instance exports no import series at all
// rather than a row of zeroes that would read as "imported nothing".
func TestImportRuns_NoRuns(t *testing.T) {
	t.Parallel()

	if got := importRuns(nil); len(got) != 0 {
		t.Errorf("importRuns(nil) = %+v, want no runs", got)
	}
}

// TestRegisterLibraryMetrics_NilRegistryIsNoop verifies wiring the collector on a
// metrics-disabled instance is inert rather than a nil-pointer panic at startup.
func TestRegisterLibraryMetrics_NilRegistryIsNoop(t *testing.T) {
	t.Parallel()

	registerLibraryMetrics(nil, nil, time.Minute)
}
