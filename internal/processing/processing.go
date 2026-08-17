// Package processing answers, for one photo, what the library has already
// computed about it: the metadata read out of its file, its thumbnails, its
// embedding, its faces, the text printed in it, the place its coordinate names
// and its metadata sidecar. Each of those is a per-photo background job, and
// each leaves persisted evidence behind when it succeeds — so the truthful
// answer comes from the evidence first and from the queue only for the work that
// has not landed yet.
//
// The package is read-only over that evidence plus one write path: scheduling a
// single step for a single photo through the existing queue enqueuer, which is
// what a maintainer reaches for when one photo was missed. It never runs the
// work itself; the handlers that do live in internal/metajob, internal/thumbjob,
// internal/embedjob, internal/facejob, internal/ocrjob, internal/placesjob and
// internal/sidecarjob.
//
// The `storyboard` job is deliberately absent from the step list. It is not part
// of what the library computes about a photo: it is rendered lazily on the first
// playback of a video, into the local derived-media cache, and leaves no
// persisted evidence in the database at all — so it has no honest state to
// report here and nothing a "run now" button could usefully schedule ahead of a
// viewer actually pressing play.
package processing

import (
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
)

// Step identifies one per-photo computation. The values are the job-type
// constants of internal/jobs, not a parallel vocabulary: the state of a step is
// read from the queue rows of that type, and running one enqueues exactly that
// job.
type Step string

// The steps reported for a photo, mirroring the job types that compute
// something about a single photo.
const (
	// StepMetadata re-reads the original file into the metadata columns it is the
	// authority on (IPTC/XMP credits, image codec, colour profile).
	StepMetadata Step = jobs.TypeMetadata
	// StepThumbnail generates the cached thumbnails and the perceptual hashes.
	StepThumbnail Step = jobs.TypeThumbnail
	// StepImageEmbed computes the CLIP image embedding that backs semantic search
	// and "similar photos".
	StepImageEmbed Step = jobs.TypeImageEmbed
	// StepFaceDetect detects the faces in the photo and caches their vectors.
	StepFaceDetect Step = jobs.TypeFaceDetect
	// StepOCR reads the text printed in the photo so search can find it by what it
	// says.
	StepOCR Step = jobs.TypeOCR
	// StepPlaces reverse-geocodes the photo's coordinate into a named place.
	StepPlaces Step = jobs.TypePlaces
	// StepSidecar writes the metadata sidecar next to the original, so the
	// catalogue survives losing the database.
	StepSidecar Step = jobs.TypeSidecar
)

// Steps is the fixed order the steps are reported in: roughly the order the
// upload pipeline reaches them, from reading the file to writing the sidecar
// that records everything else. It is a stable presentation order, so the block
// in the photo detail does not reshuffle between two photos.
var Steps = []Step{
	StepMetadata,
	StepThumbnail,
	StepImageEmbed,
	StepFaceDetect,
	StepOCR,
	StepPlaces,
	StepSidecar,
}

// ParseStep returns the Step named by s, or ok=false when s is not one of the
// reported steps. It is what turns an untrusted path parameter into a Step —
// including rejecting the job types that exist but are not per-photo steps
// (storyboard, backup, the nameless repairs).
func ParseStep(s string) (Step, bool) {
	for _, step := range Steps {
		if string(step) == s {
			return step, true
		}
	}
	return "", false
}

// State is what a step's row says about the work.
type State string

// The recognised step states.
const (
	// StateDone means the work landed: the evidence for it is in the database.
	StateDone State = "done"
	// StateRunning means a worker is processing the step right now.
	StateRunning State = "running"
	// StateQueued means the step is waiting in the queue — for a worker, or for
	// the embeddings box to come back.
	StateQueued State = "queued"
	// StateFailed means the last attempt errored: the job was dead-lettered, or it
	// is waiting to be retried after a failure. Status.Error carries what went
	// wrong.
	StateFailed State = "failed"
	// StateSkipped means the step cannot apply to this photo at all — a place for
	// a photo with no coordinate, face detection or text recognition for a video —
	// so its absence is not a gap.
	StateSkipped State = "skipped"
	// StatePending means none of the above: the step has never run and nothing is
	// scheduled.
	StatePending State = "pending"
)

// Status is one step's line in the report: which step, what state it is in and,
// when the work landed, when that was. The two step-specific fields say what
// "done" actually produced, so a success with nothing in it does not read as a
// gap.
type Status struct {
	Step  Step  `json:"step"`
	State State `json:"state"`
	// At is when the evidence was recorded, present only for StateDone.
	At *time.Time `json:"at,omitempty"`
	// Error is the last attempt's failure message, present only for StateFailed.
	Error string `json:"error,omitempty"`
	// FaceCount is how many faces the detection recorded, present only on a done
	// StepFaceDetect. Zero is a real result: the photo was looked at and holds no
	// face.
	FaceCount *int `json:"face_count,omitempty"`
	// TextFound reports whether the recognised text was non-empty, present only on
	// a done StepOCR. False is a real result — OCR that looked and found no text
	// is a success, never a gap — which is why it is a tri-state pointer here
	// rather than an omitted false.
	TextFound *bool `json:"text_found,omitempty"`
}

// Evidence is everything one photo's row and its side tables say about the work
// already done on it. It is filled by a single query (see Store.Evidence): every
// side table is keyed by photo_uid, so the whole report costs one round trip.
type Evidence struct {
	// MediaType and HasGPS decide which steps can apply to this photo at all.
	MediaType photos.MediaType
	HasGPS    bool

	// MetadataAt is photos.metadata_extracted_at.
	MetadataAt *time.Time
	// ThumbnailAt is the photo_phashes row's created_at: the thumbnail job
	// computes the perceptual hashes alongside the thumbnails, so that row is the
	// durable record that it ran (a thumbnail itself is only a cache file).
	ThumbnailAt *time.Time
	// EmbeddingAt is the embeddings row's created_at.
	EmbeddingAt *time.Time
	// FaceAt and FaceCount are the face_detections row: when detection ran and how
	// many faces it stored.
	FaceAt    *time.Time
	FaceCount int
	// OCRAt is photos.ocr_at and OCRTextFound whether photos.ocr_text is non-empty.
	OCRAt        *time.Time
	OCRTextFound bool
	// PlaceAt is the photo_places row's geocoded_at, but only for a row that
	// actually names a place. A row with no coordinates is not a geocode: it is
	// the marker the job writes for a photo with no GPS so it never retries it,
	// and reporting that as "done" would claim a place the photo has not got.
	PlaceAt *time.Time
	// SidecarAt is photos.sidecar_written_at.
	SidecarAt *time.Time
}

// applies reports whether step can ever produce a result for this photo. A
// photo with no coordinate has nothing to reverse-geocode, and a video is
// deliberately outside face detection and text recognition — those read a still,
// and a video's poster frame is not one.
func (e Evidence) applies(step Step) bool {
	switch step {
	case StepPlaces:
		return e.HasGPS
	case StepFaceDetect, StepOCR:
		return e.MediaType != photos.MediaVideo
	default:
		return true
	}
}

// doneAt returns when step's evidence was recorded, or nil when there is none.
func (e Evidence) doneAt(step Step) *time.Time {
	switch step {
	case StepMetadata:
		return e.MetadataAt
	case StepThumbnail:
		return e.ThumbnailAt
	case StepImageEmbed:
		return e.EmbeddingAt
	case StepFaceDetect:
		return e.FaceAt
	case StepOCR:
		return e.OCRAt
	case StepPlaces:
		return e.PlaceAt
	case StepSidecar:
		return e.SidecarAt
	default:
		return nil
	}
}

// status resolves one step against the evidence and the photo's unfinished job
// of that type (nil when there is none). skip says the step will never run for
// this photo — it does not apply, or the feature behind it is switched off on
// this instance.
//
// The order is deliberate: persisted evidence wins, because work that has landed
// is done however the queue looks; the queue answers next, because work in
// flight is more informative than "will not run"; only then does skipping speak,
// and an absence with nothing behind it is pending.
func (e Evidence) status(step Step, job *jobs.Job, skip bool) Status {
	st := Status{Step: step}
	switch {
	case e.doneAt(step) != nil:
		st.State = StateDone
		st.At = e.doneAt(step)
		e.describeResult(&st)
	case job != nil:
		st.State, st.Error = jobState(*job)
	case skip:
		st.State = StateSkipped
	default:
		st.State = StatePending
	}
	return st
}

// describeResult stamps the step-specific facts about a completed step onto st:
// how many faces the detection found, and whether the recognised text was empty.
// Both exist so a legitimate empty result reads as the success it is.
func (e Evidence) describeResult(st *Status) {
	switch st.Step {
	case StepFaceDetect:
		count := e.FaceCount
		st.FaceCount = &count
	case StepOCR:
		found := e.OCRTextFound
		st.TextFound = &found
	case StepMetadata, StepThumbnail, StepImageEmbed, StepPlaces, StepSidecar:
	}
}

// jobState maps one unfinished queue row onto a step state and the error text
// that goes with it. A dead job has exhausted its attempts; a queued job that
// carries a last_error is a retry pending after a failure, and reporting it as a
// plain "queued" would hide the reason it is back in the queue. A running job
// wins over its own previous error: the retry is happening now.
func jobState(job jobs.Job) (State, string) {
	switch {
	case job.State == jobs.StateRunning:
		return StateRunning, ""
	case job.State == jobs.StateDead || job.State == jobs.StateFailed:
		return StateFailed, job.LastError
	case job.LastError != "":
		return StateFailed, job.LastError
	default:
		return StateQueued, ""
	}
}

// report builds the full ordered report from the evidence and the photo's
// unfinished jobs, keyed by job type. skip decides, per step, whether it will
// ever run for this photo.
func (e Evidence) report(byType map[string]jobs.Job, skip func(Step) bool) []Status {
	list := make([]Status, 0, len(Steps))
	for _, step := range Steps {
		var job *jobs.Job
		if j, ok := byType[string(step)]; ok {
			job = &j
		}
		list = append(list, e.status(step, job, skip(step)))
	}
	return list
}
