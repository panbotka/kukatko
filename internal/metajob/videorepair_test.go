package metajob

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/video"
)

// fullProbe is what a healthy ffprobe reading of a clip looks like: every field
// the container can state, stated.
func fullProbe() video.Metadata {
	taken := time.Date(2024, 5, 17, 9, 30, 0, 0, time.UTC)
	return video.Metadata{
		TakenAt:    &taken,
		Lat:        new(49.3456),
		Lng:        new(16.7654),
		Altitude:   new(412.0),
		Width:      1920,
		Height:     1080,
		DurationMs: new(7_500),
		VideoCodec: "h264",
		AudioCodec: "aac",
		HasAudio:   true,
		FPS:        new(29.97),
	}
}

// blankVideo is a video row the way a failed probe left it: catalogued, marked as
// read, and with every field only a probe could fill still empty.
func blankVideo() photos.Photo {
	return photos.Photo{UID: "vid1", MediaType: photos.MediaVideo, FileName: "clip.mp4"}
}

// repairedVideo is a video row that already agrees with fullProbe, so a re-probe
// has nothing to say about it.
func repairedVideo() photos.Photo {
	p := blankVideo()
	probe := fullProbe()
	p.DurationMs = probe.DurationMs
	p.FileWidth, p.FileHeight = probe.Width, probe.Height
	p.FPS = probe.FPS
	p.VideoCodec, p.AudioCodec, p.HasAudio = probe.VideoCodec, probe.AudioCodec, probe.HasAudio
	p.TakenAt, p.TakenAtSource = probe.TakenAt, "exif"
	p.Lat, p.Lng, p.Altitude = probe.Lat, probe.Lng, probe.Altitude
	p.LocationSource = photos.LocationSourceExif
	return p
}

// TestPlanVideoRepair_restoresEverythingAfterAFailedProbe checks the case the
// repair exists for: a clip whose probe failed at upload gets every field the
// container states, its capture time (with the provenance that says where it came
// from) and its coordinates.
func TestPlanVideoRepair_restoresEverythingAfterAFailedProbe(t *testing.T) {
	t.Parallel()

	probe := fullProbe()
	got := planVideoRepair(blankVideo(), probe)

	if got.Empty() {
		t.Fatal("planVideoRepair() = empty; a blank video row must be repaired")
	}
	if got.DurationMs == nil || *got.DurationMs != 7_500 {
		t.Errorf("DurationMs = %v, want 7500", got.DurationMs)
	}
	if got.Width == nil || got.Height == nil || *got.Width != 1920 || *got.Height != 1080 {
		t.Errorf("dimensions = %v×%v, want 1920×1080", got.Width, got.Height)
	}
	if got.FPS == nil || *got.FPS != 29.97 {
		t.Errorf("FPS = %v, want 29.97", got.FPS)
	}
	if got.VideoCodec == nil || *got.VideoCodec != "h264" {
		t.Errorf("VideoCodec = %v, want h264", got.VideoCodec)
	}
	if got.AudioCodec == nil || *got.AudioCodec != "aac" || got.HasAudio == nil || !*got.HasAudio {
		t.Errorf("audio = %v / %v, want aac / true", got.AudioCodec, got.HasAudio)
	}
	if got.TakenAt == nil || !got.TakenAt.Equal(*probe.TakenAt) {
		t.Errorf("TakenAt = %v, want %v", got.TakenAt, probe.TakenAt)
	}
	if got.TakenAtSource != "exif" {
		t.Errorf("TakenAtSource = %q, want exif — the file said so", got.TakenAtSource)
	}
	if got.Lat == nil || got.Lng == nil || got.LocationSource != photos.LocationSourceExif {
		t.Errorf("location = %v/%v (%q), want the container's fix", got.Lat, got.Lng, got.LocationSource)
	}
	if got.Altitude == nil || *got.Altitude != 412 {
		t.Errorf("Altitude = %v, want 412", got.Altitude)
	}
}

// TestPlanVideoRepair_fileDerivedFields covers the decision for the fields only a
// probe ever writes: a value that differs from the file is corrected, an identical
// one is left alone (so a healthy clip is a no-op), and a field the probe could not
// read is never written — "I did not see it" is not "it is not there".
func TestPlanVideoRepair_fileDerivedFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		photo func(*photos.Photo)
		probe func(*video.Metadata)
		want  func(*testing.T, photos.VideoRepair)
	}{
		{
			name:  "a healthy clip is untouched",
			photo: func(*photos.Photo) {},
			probe: func(*video.Metadata) {},
			want: func(t *testing.T, r photos.VideoRepair) {
				if !r.Empty() {
					t.Errorf("repair = %+v, want empty", r)
				}
			},
		},
		{
			name:  "a wrong duration is corrected",
			photo: func(p *photos.Photo) { p.DurationMs = new(1_000) },
			probe: func(*video.Metadata) {},
			want: func(t *testing.T, r photos.VideoRepair) {
				if r.DurationMs == nil || *r.DurationMs != 7_500 {
					t.Errorf("DurationMs = %v, want the probed 7500", r.DurationMs)
				}
			},
		},
		{
			name:  "an unreadable duration leaves the stored one alone",
			photo: func(p *photos.Photo) { p.DurationMs = new(1_000) },
			probe: func(m *video.Metadata) { m.DurationMs = nil },
			want: func(t *testing.T, r photos.VideoRepair) {
				if r.DurationMs != nil {
					t.Errorf("DurationMs = %v, want nil — the probe read no duration", r.DurationMs)
				}
			},
		},
		{
			name:  "transposed dimensions are corrected as a pair",
			photo: func(p *photos.Photo) { p.FileWidth, p.FileHeight = 1080, 1920 },
			probe: func(*video.Metadata) {},
			want: func(t *testing.T, r photos.VideoRepair) {
				if r.Width == nil || r.Height == nil || *r.Width != 1920 || *r.Height != 1080 {
					t.Errorf("dimensions = %v×%v, want 1920×1080", r.Width, r.Height)
				}
			},
		},
		{
			name:  "half a frame size is not written",
			photo: func(p *photos.Photo) { p.FileWidth, p.FileHeight = 1920, 1080 },
			probe: func(m *video.Metadata) { m.Height = 0 },
			want: func(t *testing.T, r photos.VideoRepair) {
				if r.Width != nil || r.Height != nil {
					t.Errorf("dimensions = %v×%v, want neither", r.Width, r.Height)
				}
			},
		},
		{
			name:  "float noise in the frame rate is not a difference",
			photo: func(p *photos.Photo) { p.FPS = new(29.97 + 1e-9) },
			probe: func(*video.Metadata) {},
			want: func(t *testing.T, r photos.VideoRepair) {
				if r.FPS != nil {
					t.Errorf("FPS = %v, want nil — the same rate", r.FPS)
				}
			},
		},
		{
			name:  "an empty codec reading never erases the stored name",
			photo: func(p *photos.Photo) { p.VideoCodec = "hevc" },
			probe: func(m *video.Metadata) { m.VideoCodec = "" },
			want: func(t *testing.T, r photos.VideoRepair) {
				if r.VideoCodec != nil {
					t.Errorf("VideoCodec = %v, want nil", r.VideoCodec)
				}
			},
		},
		{
			name:  "a silent clip loses the audio codec it never had",
			photo: func(p *photos.Photo) { p.HasAudio, p.AudioCodec = true, "aac" },
			probe: func(m *video.Metadata) { m.HasAudio, m.AudioCodec = false, "" },
			want: func(t *testing.T, r photos.VideoRepair) {
				if r.HasAudio == nil || *r.HasAudio {
					t.Errorf("HasAudio = %v, want false", r.HasAudio)
				}
				if r.AudioCodec == nil || *r.AudioCodec != "" {
					t.Errorf("AudioCodec = %v, want the empty name", r.AudioCodec)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			photo := repairedVideo()
			tt.photo(&photo)
			probe := fullProbe()
			tt.probe(&probe)
			tt.want(t, planVideoRepair(photo, probe))
		})
	}
}

// TestPlanVideoRepair_captureTime covers the delicate half: a re-probe fills a
// missing or filename-guessed date and never overrules a person — not a typed
// date, not one an earlier probe read, not a date declared unknown, not an
// estimate.
func TestPlanVideoRepair_captureTime(t *testing.T) {
	t.Parallel()

	stored := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	tests := []struct {
		name  string
		photo func(*photos.Photo)
		want  bool
	}{
		{
			name:  "no date at all is a gap",
			photo: func(p *photos.Photo) { p.TakenAt, p.TakenAtSource = nil, "unknown" },
			want:  true,
		},
		{
			name:  "a date guessed from the file name yields to the container",
			photo: func(p *photos.Photo) { p.TakenAt, p.TakenAtSource = &stored, "filename" },
			want:  true,
		},
		{
			name:  "a typed date stands",
			photo: func(p *photos.Photo) { p.TakenAt, p.TakenAtSource = &stored, photos.TakenAtSourceManual },
			want:  false,
		},
		{
			name:  "a date an earlier probe read stands",
			photo: func(p *photos.Photo) { p.TakenAt, p.TakenAtSource = &stored, "exif" },
			want:  false,
		},
		{
			name: "a date declared unknown stays disowned",
			photo: func(p *photos.Photo) {
				p.TakenAt, p.TakenAtSource = nil, photos.TakenAtSourceUnknown
				p.TakenAtBeforeUnknown = &stored
			},
			want: false,
		},
		{
			name: "an estimated date is not overruled",
			photo: func(p *photos.Photo) {
				p.TakenAt, p.TakenAtSource, p.TakenAtEstimated = nil, "unknown", true
				p.TakenAtNote = "kolem roku 1950"
			},
			want: false,
		},
		{
			name:  "a manual clear with no date left is still a decision",
			photo: func(p *photos.Photo) { p.TakenAt, p.TakenAtSource = nil, photos.TakenAtSourceManual },
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			photo := repairedVideo()
			tt.photo(&photo)
			got := planVideoRepair(photo, fullProbe())
			if (got.TakenAt != nil) != tt.want {
				t.Errorf("TakenAt offered = %v (%v), want %v", got.TakenAt != nil, got.TakenAt, tt.want)
			}
			if tt.want && got.TakenAtSource != "exif" {
				t.Errorf("TakenAtSource = %q, want exif", got.TakenAtSource)
			}
			if !tt.want && got.TakenAtSource != "" {
				t.Errorf("TakenAtSource = %q, want empty when no date is written", got.TakenAtSource)
			}
		})
	}
}

// TestPlanVideoRepair_location covers the other delicate half: the container's
// coordinates fill a gap nobody has decided about, and never replace a fix that is
// there or a location somebody deleted (the "manual" tombstone).
func TestPlanVideoRepair_location(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		photo func(*photos.Photo)
		want  bool
	}{
		{
			name: "no location and no decision is a gap",
			photo: func(p *photos.Photo) {
				p.Lat, p.Lng, p.LocationSource = nil, nil, ""
			},
			want: true,
		},
		{
			name: "a hand-set location stands",
			photo: func(p *photos.Photo) {
				p.Lat, p.Lng = new(50.0), new(14.0)
				p.LocationSource = photos.LocationSourceManual
			},
			want: false,
		},
		{
			name: "a deleted location is not handed back",
			photo: func(p *photos.Photo) {
				p.Lat, p.Lng = nil, nil
				p.LocationSource = photos.LocationSourceManual
			},
			want: false,
		},
		{
			name: "an estimated location is left for the estimator to own",
			photo: func(p *photos.Photo) {
				p.Lat, p.Lng = new(50.0), new(14.0)
				p.LocationSource = photos.LocationSourceEstimate
			},
			want: false,
		},
		{
			name: "a fix already read out of the file is unchanged",
			photo: func(p *photos.Photo) {
				p.LocationSource = photos.LocationSourceExif
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			photo := repairedVideo()
			tt.photo(&photo)
			got := planVideoRepair(photo, fullProbe())
			if (got.Lat != nil) != tt.want || (got.Lng != nil) != tt.want {
				t.Errorf("coordinates offered = %v/%v, want %v", got.Lat, got.Lng, tt.want)
			}
			if tt.want && got.LocationSource != photos.LocationSourceExif {
				t.Errorf("LocationSource = %q, want exif", got.LocationSource)
			}
		})
	}
}

// TestPlanVideoRepair_incredibleProbeChangesNothing checks the guard that keeps the
// repair from doing the damage it exists to undo: a probe that read no container at
// all writes nothing, rather than "correcting" a clip's duration to unknown and its
// sound to silence.
func TestPlanVideoRepair_incredibleProbeChangesNothing(t *testing.T) {
	t.Parallel()

	if got := planVideoRepair(repairedVideo(), video.Metadata{}); !got.Empty() {
		t.Errorf("planVideoRepair(_, nothing) = %+v, want empty", got)
	}
	// A probe that read only a creation time out of the tags has still not read the
	// container, so it may not date the photo either.
	taken := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	blank := blankVideo()
	if got := planVideoRepair(blank, video.Metadata{TakenAt: &taken}); !got.Empty() {
		t.Errorf("planVideoRepair(_, a bare date) = %+v, want empty", got)
	}
	// One credible reading is enough to trust the rest of the document.
	if got := planVideoRepair(blank, video.Metadata{DurationMs: new(2_000), TakenAt: &taken}); got.Empty() {
		t.Error("planVideoRepair() = empty; a duration is a credible reading")
	}
}
