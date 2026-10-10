package processing

import (
	"context"
	"errors"
	"fmt"

	"github.com/panbotka/kukatko/internal/jobs"
)

// Sentinel errors returned by Service so the HTTP layer can map them onto status
// codes with errors.Is.
var (
	// ErrUnknownStep indicates the requested step is not one of Steps.
	ErrUnknownStep = errors.New("processing: unknown step")
	// ErrStepNotApplicable indicates the step cannot apply to this photo at all —
	// a place for a photo with no coordinate, text recognition for a video, a
	// streaming encode for a still — so there is nothing to schedule.
	ErrStepNotApplicable = errors.New("processing: step does not apply to this photo")
	// ErrLibraryUnavailable indicates Unscheduled was called on a Service built
	// without a LibraryReader.
	ErrLibraryUnavailable = errors.New("processing: library gap scan not configured")
)

// EvidenceReader reads the persisted evidence of the work already done on a
// photo. It is satisfied by *Store; a fake stands in for unit tests.
type EvidenceReader interface {
	// Evidence returns the photo's processing evidence, or photos.ErrPhotoNotFound.
	Evidence(ctx context.Context, photoUID string) (Evidence, error)
}

// JobLister reads the queue's side of the story: the photo's unfinished jobs,
// at most one per type. It is satisfied by *jobs.Store.
type JobLister interface {
	// UnfinishedForPhoto returns the newest unfinished job per type for photoUID.
	UnfinishedForPhoto(ctx context.Context, photoUID string) ([]jobs.Job, error)
}

// LibraryReader reads the library-wide half of the gap scan: the evidence of
// every live photo and the job types each has unfinished work of. It is satisfied
// by *Store.
type LibraryReader interface {
	// EachLiveEvidence calls fn with the evidence of every live photo.
	EachLiveEvidence(ctx context.Context, fn func(photoUID string, ev Evidence) error) error
	// UnfinishedSteps returns, per photo uid, the job types it has an unfinished
	// job of.
	UnfinishedSteps(ctx context.Context) (map[string]map[Step]bool, error)
}

// Gap is one step one live photo is owed and that nothing is going to deliver:
// no evidence it ran and no job of its type in the queue. It is the state the
// report calls StatePending, found across the whole library.
type Gap struct {
	PhotoUID string `json:"photo_uid"`
	Step     Step   `json:"step"`
}

// StepEnqueuer schedules one step for one photo. It is satisfied by
// *jobs.Enqueuer, and every method there already swallows a dedup hit — which is
// what makes a double click on "run now" harmless.
type StepEnqueuer interface {
	// EnqueueMetadata schedules a re-read of the photo's original file.
	EnqueueMetadata(ctx context.Context, photoUID string) error
	// EnqueueThumbnail schedules thumbnail and perceptual-hash generation.
	EnqueueThumbnail(ctx context.Context, photoUID string) error
	// EnqueueHLSTranscode schedules the video's streaming encode.
	EnqueueHLSTranscode(ctx context.Context, photoUID string) error
	// EnqueueImageEmbed schedules the image embedding.
	EnqueueImageEmbed(ctx context.Context, photoUID string) error
	// EnqueueFaceDetect schedules face detection.
	EnqueueFaceDetect(ctx context.Context, photoUID string) error
	// EnqueueOCR schedules text recognition.
	EnqueueOCR(ctx context.Context, photoUID string) error
	// EnqueuePlaces schedules the reverse geocode.
	EnqueuePlaces(ctx context.Context, photoUID string) error
	// EnqueueSidecar schedules a rewrite of the metadata sidecar.
	EnqueueSidecar(ctx context.Context, photoUID string) error
}

// Config bundles the dependencies of New.
type Config struct {
	// Evidence reads what the database already knows about a photo.
	Evidence EvidenceReader
	// Jobs reads the photo's unfinished queue rows.
	Jobs JobLister
	// Enqueuer schedules a single step.
	Enqueuer StepEnqueuer
	// Library reads the whole library for Unscheduled. It is optional: a Service
	// without one reports and schedules per photo only.
	Library LibraryReader
	// Disabled lists the steps whose feature is switched off on this instance
	// (`ocr` without OCR enabled, `sidecar` without the export, `places` without a
	// mapy.com key, `hls_transcode` without streaming). No worker handler is registered for those, so a job of that
	// type would sit in the queue forever — they are therefore reported as skipped
	// and refused by Run rather than quietly enqueued.
	Disabled []Step
}

// Service answers what has been computed about a photo, and schedules a single
// missing step on request. It is read-only apart from that one enqueue: it never
// runs the work itself.
type Service struct {
	evidence EvidenceReader
	jobs     JobLister
	enqueuer StepEnqueuer
	library  LibraryReader
	disabled map[Step]bool
}

// New returns a Service from cfg. It panics if the evidence reader, the job
// lister or the enqueuer is nil: none has a sensible default, and a Service
// missing one could only report or schedule half the truth.
func New(cfg Config) *Service {
	if cfg.Evidence == nil || cfg.Jobs == nil || cfg.Enqueuer == nil {
		panic("processing: New requires an evidence reader, a job lister and an enqueuer")
	}
	disabled := make(map[Step]bool, len(cfg.Disabled))
	for _, step := range cfg.Disabled {
		disabled[step] = true
	}
	return &Service{
		evidence: cfg.Evidence, jobs: cfg.Jobs, enqueuer: cfg.Enqueuer, library: cfg.Library,
		disabled: disabled,
	}
}

// skips reports whether step will never run for this photo: it cannot apply to
// it, or the feature behind it is switched off on this instance. Both read the
// same way to the person looking at the photo — this step is not coming — so
// they share the skipped state.
func (s *Service) skips(ev Evidence, step Step) bool {
	return s.disabled[step] || !ev.applies(step)
}

// Report returns the state of every step for the photo identified by photoUID,
// in the fixed order of Steps. It costs exactly two round trips — one for the
// evidence, one for the photo's unfinished jobs — however many steps there are.
// It returns photos.ErrPhotoNotFound for an unknown photo.
func (s *Service) Report(ctx context.Context, photoUID string) ([]Status, error) {
	ev, err := s.evidence.Evidence(ctx, photoUID)
	if err != nil {
		return nil, fmt.Errorf("processing: reporting %s: %w", photoUID, err)
	}
	byType, err := s.unfinishedByType(ctx, photoUID)
	if err != nil {
		return nil, err
	}
	return ev.report(byType, func(step Step) bool { return s.skips(ev, step) }), nil
}

// Run schedules one step for one photo and returns that step's new state. An
// already-active job for the same step absorbs the request (the queue's dedup
// index), so pressing the button twice schedules the work once.
//
// It returns ErrUnknownStep for a step outside Steps, photos.ErrPhotoNotFound
// for an unknown photo, and ErrStepNotApplicable for a step that will never run
// for this photo — it does not apply, or its feature is off — refusing to queue
// work that no handler would ever claim.
func (s *Service) Run(ctx context.Context, photoUID string, step Step) (Status, error) {
	if _, ok := ParseStep(string(step)); !ok {
		return Status{}, fmt.Errorf("%w: %s", ErrUnknownStep, step)
	}
	ev, err := s.evidence.Evidence(ctx, photoUID)
	if err != nil {
		return Status{}, fmt.Errorf("processing: running %s for %s: %w", step, photoUID, err)
	}
	if s.skips(ev, step) {
		return Status{}, fmt.Errorf("%w: %s", ErrStepNotApplicable, step)
	}
	if err := s.enqueue(ctx, photoUID, step); err != nil {
		return Status{}, err
	}
	byType, err := s.unfinishedByType(ctx, photoUID)
	if err != nil {
		return Status{}, err
	}
	var job *jobs.Job
	if j, ok := byType[string(step)]; ok {
		job = &j
	}
	return ev.status(step, job, false), nil
}

// SchedulePending enqueues every step of the photo identified by photoUID that
// the report would call pending — owed, never done, nothing queued — and returns
// the steps it scheduled, in report order. A step that is done, queued, running,
// failed or skipped is left exactly as it is, so calling it on a fully processed
// photo costs the two reads of Report and enqueues nothing.
//
// It is how a photo whose processing was cut short is completed without anybody
// having to know which steps it missed: a re-upload of the same bytes calls it
// (see internal/ingest). An enqueue failure does not stop the others; the
// failures are joined into the returned error. It returns
// photos.ErrPhotoNotFound for an unknown photo.
func (s *Service) SchedulePending(ctx context.Context, photoUID string) ([]Step, error) {
	report, err := s.Report(ctx, photoUID)
	if err != nil {
		return nil, err
	}
	var scheduled []Step
	var errs []error
	for _, st := range report {
		if st.State != StatePending {
			continue
		}
		if err := s.enqueue(ctx, photoUID, st.Step); err != nil {
			errs = append(errs, err)
			continue
		}
		scheduled = append(scheduled, st.Step)
	}
	return scheduled, errors.Join(errs...)
}

// Unscheduled returns every gap in the live library — each (photo, step) the
// report would call pending — ordered by photo uid and then by the report's step
// order. Steps switched off on this instance are never gaps, exactly as they are
// never pending in the report. It costs two queries however large the library
// is: one walk over the evidence, one scan of the unfinished jobs.
//
// It returns ErrLibraryUnavailable on a Service built without a LibraryReader.
func (s *Service) Unscheduled(ctx context.Context) ([]Gap, error) {
	if s.library == nil {
		return nil, ErrLibraryUnavailable
	}
	unfinished, err := s.library.UnfinishedSteps(ctx)
	if err != nil {
		return nil, fmt.Errorf("processing: library gap scan: %w", err)
	}
	gaps := make([]Gap, 0)
	err = s.library.EachLiveEvidence(ctx, func(photoUID string, ev Evidence) error {
		skip := func(step Step) bool { return s.skips(ev, step) }
		for _, step := range ev.pending(unfinished[photoUID], skip) {
			gaps = append(gaps, Gap{PhotoUID: photoUID, Step: step})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("processing: library gap scan: %w", err)
	}
	return gaps, nil
}

// unfinishedByType reads the photo's unfinished jobs and indexes them by job
// type, so building the report is a map lookup per step rather than a scan.
func (s *Service) unfinishedByType(ctx context.Context, photoUID string) (map[string]jobs.Job, error) {
	list, err := s.jobs.UnfinishedForPhoto(ctx, photoUID)
	if err != nil {
		return nil, fmt.Errorf("processing: unfinished jobs for %s: %w", photoUID, err)
	}
	byType := make(map[string]jobs.Job, len(list))
	for _, job := range list {
		byType[job.Type] = job
	}
	return byType, nil
}

// stepEnqueuers maps each step to the enqueuer method that owns it, so each step
// keeps its own scheduling semantics (the sidecar's debounce, for one) rather
// than being re-implemented here. It is a table of method expressions rather
// than a switch because a switch grows one branch per step and there are now
// enough of them that it was the most complex function in the package;
// TestService_Run_everyStepReachesItsEnqueuer is what keeps a new step from
// silently falling out of it.
var stepEnqueuers = map[Step]func(StepEnqueuer, context.Context, string) error{
	StepMetadata:   StepEnqueuer.EnqueueMetadata,
	StepThumbnail:  StepEnqueuer.EnqueueThumbnail,
	StepHLS:        StepEnqueuer.EnqueueHLSTranscode,
	StepImageEmbed: StepEnqueuer.EnqueueImageEmbed,
	StepFaceDetect: StepEnqueuer.EnqueueFaceDetect,
	StepOCR:        StepEnqueuer.EnqueueOCR,
	StepPlaces:     StepEnqueuer.EnqueuePlaces,
	StepSidecar:    StepEnqueuer.EnqueueSidecar,
}

// enqueue schedules step for photoUID through the method stepEnqueuers names for
// it, returning ErrUnknownStep for a step no method owns.
func (s *Service) enqueue(ctx context.Context, photoUID string, step Step) error {
	schedule, ok := stepEnqueuers[step]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownStep, step)
	}
	if err := schedule(s.enqueuer, ctx, photoUID); err != nil {
		return fmt.Errorf("processing: scheduling %s for %s: %w", step, photoUID, err)
	}
	return nil
}
