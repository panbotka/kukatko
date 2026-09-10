package maintenance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/hlsjob"
)

// ErrStreamingUnavailable indicates a streaming repair was requested on an
// instance that does not stream (`video.hls.enabled: false`). There is nothing to
// reconcile there — no encode job runs, so a dropped row would never be
// re-encoded and a swept object would never be rewritten — so the repair refuses
// rather than acting on a half-wired feature.
var ErrStreamingUnavailable = errors.New("maintenance: video streaming not configured")

// StreamingCatalog is the subset of the streaming catalogue the scan and its
// repair need: what the catalogue claims a player can fetch, what the queue is
// still in the middle of producing, and the withdrawal of a claim that turned out
// to be false. It is satisfied by *hlsjob.Store.
type StreamingCatalog interface {
	// ListRecorded returns every recorded rendition with the file hash naming its
	// prefix in the store.
	ListRecorded(ctx context.Context) ([]hlsjob.Recorded, error)
	// EncodingHashes returns the file hash of every video with an unfinished
	// hls_transcode job, whose objects are being written right now.
	EncodingHashes(ctx context.Context) ([]string, error)
	// Delete removes one recorded rendition, reporting whether there was a row.
	Delete(ctx context.Context, photoUID, rendition string) (bool, error)
}

// SegmentStore is the subset of the object store the streaming check needs: a
// listing of one prefix, the presence and size of one object, and — only for the
// explicitly requested sweep — its removal. It is satisfied by a storage backend
// that is also a storage.PrefixLister, which both of them are.
//
// It is separate from OriginalStore because the two answer about different
// things: OriginalStore is asked whether a photo's original is still there, this
// one about the derived objects a video's playlists point at.
type SegmentStore interface {
	// KeysWithPrefix calls yield once per object whose key starts with prefix.
	KeysWithPrefix(ctx context.Context, prefix string, yield func(key string) error) error
	// Stat returns file information for the object at relPath, or an error
	// wrapping os.ErrNotExist when it is absent.
	Stat(ctx context.Context, relPath string) (os.FileInfo, error)
	// Delete removes the object at relPath, reporting an os.ErrNotExist-wrapping
	// error when there is nothing there.
	Delete(ctx context.Context, relPath string) error
}

// Streaming bundles the two collaborators the streaming half of the scan needs.
// A zero value — or one with either member left nil — is how "this instance does
// not stream" reaches the scan, which then reports nothing and, crucially, asks
// the store nothing: an instance with `video.hls.enabled: false` must not pay a
// bucket listing for a feature it does not run.
type Streaming struct {
	// Renditions is the catalogue of what has been encoded.
	Renditions StreamingCatalog
	// Segments is the store the encoded objects live in.
	Segments SegmentStore
}

// enabled reports whether both halves of the streaming check are wired, i.e.
// whether this instance streams at all.
func (s Streaming) enabled() bool {
	return s.Renditions != nil && s.Segments != nil
}

// SegmentOrphans is the orphan half of the streaming check: objects under the
// streaming prefix that no recorded rendition accounts for, which nothing will
// ever serve and nothing else will ever clean up.
//
// Count is objects while Samples are the hls/<file_hash>/<rendition>/ prefixes
// they sit under, because twenty segment file names out of one abandoned encode
// say far less than the one prefix that holds them — and the prefix is what
// names the video and the rendition a maintainer has to decide about. Bytes is
// what makes the finding actionable in the other direction: dead weight nobody
// can see the size of never gets swept.
type SegmentOrphans struct {
	Finding
	// Bytes is the total size of the orphan objects, in bytes.
	Bytes int64 `json:"bytes"`
}

// segmentRef is what an object key under the streaming prefix says about itself:
// which video's objects it sits among, which rendition of it, and the prefix
// those two name together.
type segmentRef struct {
	// fileHash is the video's content hash, empty when the key is unshaped.
	fileHash string
	// rendition is the rendition name, empty when the key is unshaped.
	rendition string
	// group is the hls/<file_hash>/<rendition>/ prefix the object belongs to, or
	// the whole key when it belongs to no rendition that could exist.
	group string
	// shaped reports whether the key matches the layout at all.
	shaped bool
}

// parseSegmentKey splits an object key under the streaming prefix into the video
// and rendition it belongs to.
//
// A key that is not shaped like hls/<file_hash>/<rendition>/<name> is reported
// unshaped and grouped under itself: it belongs to no rendition that could exist,
// so folding it into a neighbouring prefix would let a real rendition excuse it —
// and would let an in-flight encode excuse an object it is not writing.
func parseSegmentKey(key string) segmentRef {
	rest, ok := strings.CutPrefix(key, hls.Prefix+"/")
	if !ok {
		return segmentRef{group: key}
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return segmentRef{group: key}
	}
	return segmentRef{
		fileHash:  parts[0],
		rendition: parts[1],
		group:     hls.Prefix + "/" + parts[0] + "/" + parts[1] + "/",
		shaped:    true,
	}
}

// renditionID names one rendition in a finding sample as <photo_uid>/<rendition>
// — the photo it belongs to and which quality of it, which is everything needed
// to find the row in the catalogue and its objects in the store.
func renditionID(rec hlsjob.Recorded) string {
	return rec.PhotoUID + "/" + rec.Rendition
}

// renditionKey spells the key of rec's initialisation segment, reporting false
// when the row cannot name an object at all — a file hash or rendition name the
// layout would never have produced, so nothing the row describes was ever
// fetchable.
func renditionKey(rec hlsjob.Recorded) (string, bool) {
	key, err := hls.Key(rec.FileHash, rec.Rendition, hls.InitName)
	if err != nil {
		return "", false
	}
	return key, true
}

// recordedPrefixes returns the set of hls/<file_hash>/<rendition>/ prefixes the
// catalogue records, which is what "this object belongs to a rendition" means.
//
// A row whose hash or name cannot even spell a key contributes nothing: it claims
// no object, so no object may be excused by it. Such a row is separately reported
// as a rendition whose objects are missing, which it necessarily is.
func recordedPrefixes(recorded []hlsjob.Recorded) map[string]struct{} {
	prefixes := make(map[string]struct{}, len(recorded))
	for _, rec := range recorded {
		key, ok := renditionKey(rec)
		if !ok {
			continue
		}
		prefixes[strings.TrimSuffix(key, hls.InitName)] = struct{}{}
	}
	return prefixes
}

// inSet reports whether key is a member of set.
func inSet(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

// claimed reports whether something accounts for the object ref describes: a
// rendition the catalogue records, or a video an unfinished encode is publishing
// right now. The second half is why a scan taken during an encode does not
// report that encode's half-uploaded segments as rot.
func claimed(ref segmentRef, live, encoding map[string]struct{}) bool {
	if inSet(live, ref.group) {
		return true
	}
	return ref.shaped && inSet(encoding, ref.fileHash)
}

// orphanSweep accumulates what a walk of the streaming prefix found unclaimed:
// how many objects, how much they weigh, and a bounded sample of the rendition
// prefixes they sit under, each prefix sampled once however many objects it holds.
type orphanSweep struct {
	limit   int
	count   int
	bytes   int64
	seen    map[string]struct{}
	samples []string
}

// newOrphanSweep returns a sweep that keeps at most limit prefix samples.
func newOrphanSweep(limit int) *orphanSweep {
	return &orphanSweep{limit: limit, seen: make(map[string]struct{}), samples: make([]string, 0, limit)}
}

// add records one orphan object of the given size, sampling its group prefix the
// first time that prefix is seen.
func (o *orphanSweep) add(group string, size int64) {
	o.count++
	o.bytes += size
	if _, ok := o.seen[group]; ok {
		return
	}
	o.seen[group] = struct{}{}
	if len(o.samples) < o.limit {
		o.samples = append(o.samples, group)
	}
}

// result returns the accumulated finding.
func (o *orphanSweep) result() SegmentOrphans {
	return SegmentOrphans{Finding: Finding{Count: o.count, Samples: o.samples}, Bytes: o.bytes}
}

// emptyOrphans is the orphan finding of a library that was not asked about: zero
// objects, zero bytes and a non-nil sample slice, so it serialises as [].
func emptyOrphans() SegmentOrphans {
	return SegmentOrphans{Finding: Finding{Samples: []string{}}}
}

// scanStreaming fills the two streaming findings of report: the renditions the
// catalogue promises whose objects are gone, and the objects under the streaming
// prefix no rendition claims.
//
// It costs nothing at all on an instance that does not stream or holds no video —
// no query and no store listing — because both are libraries where the answer is
// known in advance and the check would only be a bucket listing charged for
// nothing. Otherwise it costs one Stat per recorded rendition (one per video
// under the default single-quality plan), one prefix listing of hls/ for the
// whole library, and one further Stat per orphan object found.
func (s *Service) scanStreaming(ctx context.Context, report *Report) error {
	report.MissingRenditions = Finding{Samples: []string{}}
	report.OrphanSegments = emptyOrphans()
	if !s.streaming.enabled() {
		return nil
	}
	videos, err := s.photos.CountVideos(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: counting videos: %w", err)
	}
	if videos == 0 {
		return nil
	}
	recorded, err := s.streaming.Renditions.ListRecorded(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: listing recorded renditions: %w", err)
	}
	missing, err := s.scanRenditions(ctx, recorded)
	if err != nil {
		return err
	}
	orphans, err := s.scanOrphanSegments(ctx, recordedPrefixes(recorded))
	if err != nil {
		return err
	}
	report.MissingRenditions = missing
	report.OrphanSegments = orphans
	return nil
}

// scanRenditions turns the recorded renditions whose objects are not in the store
// into a Finding sampled by <photo_uid>/<rendition>. It is the dry run of
// `maintenance repair --missing-renditions`.
//
// Presence is judged by the initialisation segment alone, and that is deliberate:
// a player fetches it before any media segment, so a rendition without it cannot
// be played whatever else survives — and the alternative, asking the store about
// every segment of every video, would turn a library of long clips into tens of
// thousands of round trips per scan. The stored playlist is no help here: it is
// never written to the store, it lives in the catalogue.
func (s *Service) scanRenditions(ctx context.Context, recorded []hlsjob.Recorded) (Finding, error) {
	missing := newFindingCollector(s.sampleLimit)
	for _, rec := range recorded {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Finding{}, fmt.Errorf("maintenance: streaming scan interrupted: %w", ctxErr)
		}
		gone, err := s.renditionGone(ctx, rec)
		if err != nil {
			return Finding{}, err
		}
		if gone {
			missing.add(renditionID(rec))
		}
	}
	return missing.finding(), nil
}

// renditionGone reports whether the objects rec promises are absent from the
// store, judged by its initialisation segment.
//
// A row whose hash or rendition name cannot even spell an object key is gone by
// definition: no object the layout can hold corresponds to it, so nothing it
// describes was ever fetchable.
func (s *Service) renditionGone(ctx context.Context, rec hlsjob.Recorded) (bool, error) {
	key, ok := renditionKey(rec)
	if !ok {
		return true, nil
	}
	if _, err := s.streaming.Segments.Stat(ctx, key); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, fmt.Errorf("maintenance: statting streaming object %s: %w", key, err)
	}
	return false, nil
}

// scanOrphanSegments walks the streaming prefix and reports the objects that
// belong to neither a recorded rendition (live) nor a video the queue is
// encoding right now, together with how much storage they occupy.
//
// The size costs one Stat per orphan and nothing at all otherwise, which is the
// right way round: a healthy library pays only the listing, and a library with
// rot pays in proportion to how much of it there is.
func (s *Service) scanOrphanSegments(ctx context.Context, live map[string]struct{}) (SegmentOrphans, error) {
	encoding, err := s.encodingHashes(ctx)
	if err != nil {
		return SegmentOrphans{}, err
	}
	sweep := newOrphanSweep(s.sampleLimit)
	walkErr := s.streaming.Segments.KeysWithPrefix(ctx, hls.Prefix+"/", func(key string) error {
		return s.sweepOne(ctx, key, live, encoding, sweep)
	})
	if walkErr != nil {
		return SegmentOrphans{}, fmt.Errorf("maintenance: listing streaming objects: %w", walkErr)
	}
	return sweep.result(), nil
}

// sweepOne classifies one object under the streaming prefix, adding it to sweep
// when nothing claims it. An object that disappears between the listing and the
// question about its size is simply not there any more, so it is left out rather
// than counted at zero.
func (s *Service) sweepOne(
	ctx context.Context, key string, live, encoding map[string]struct{}, sweep *orphanSweep,
) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("maintenance: streaming scan interrupted: %w", ctxErr)
	}
	ref := parseSegmentKey(key)
	if claimed(ref, live, encoding) {
		return nil
	}
	info, err := s.streaming.Segments.Stat(ctx, key)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("maintenance: statting streaming object %s: %w", key, err)
	}
	sweep.add(ref.group, info.Size())
	return nil
}

// encodingHashes returns, as a set, the file hashes of the videos the queue still
// owes an encode.
func (s *Service) encodingHashes(ctx context.Context) (map[string]struct{}, error) {
	hashes, err := s.streaming.Renditions.EncodingHashes(ctx)
	if err != nil {
		return nil, fmt.Errorf("maintenance: listing videos being encoded: %w", err)
	}
	return keySet(hashes), nil
}

// repairMissingRenditions drops the rendition rows whose objects are gone, when
// that repair is selected.
//
// It is the safe half of the streaming repair, and the only half that runs
// without being asked twice. A row is a promise that a player can fetch the
// segments it describes; when they are not there the promise is false, the photo
// keeps advertising itself as streamable and every segment request answers 404.
// Withdrawing the row makes the catalogue honest again and puts the video back
// into exactly the state the ordinary encode backfill (POST /process/hls) looks
// for, so recovering is one background job rather than a special repair. The
// original is never touched — this repair, like the rest of the integrity check,
// deletes no media.
//
// Stray segments of a partly-surviving rendition are deliberately left where they
// are: once the row is gone they become orphans, which the scan reports and only
// an explicit sweep removes.
func (s *Service) repairMissingRenditions(ctx context.Context, opts RepairOptions, res *RepairResult) error {
	if !opts.MissingRenditions {
		return nil
	}
	if !s.streaming.enabled() {
		return ErrStreamingUnavailable
	}
	recorded, err := s.streaming.Renditions.ListRecorded(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: listing recorded renditions: %w", err)
	}
	for _, rec := range recorded {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("maintenance: streaming repair interrupted: %w", ctxErr)
		}
		if err := s.dropIfGone(ctx, rec, res); err != nil {
			return err
		}
	}
	return nil
}

// dropIfGone removes rec's row when the objects it promises are absent, counting
// the drop in res. A row already removed by a concurrent run is not counted
// twice, so re-running converges.
func (s *Service) dropIfGone(ctx context.Context, rec hlsjob.Recorded, res *RepairResult) error {
	gone, err := s.renditionGone(ctx, rec)
	if err != nil {
		return err
	}
	if !gone {
		return nil
	}
	dropped, delErr := s.streaming.Renditions.Delete(ctx, rec.PhotoUID, rec.Rendition)
	if delErr != nil {
		return fmt.Errorf("maintenance: dropping rendition %s of %s: %w",
			rec.Rendition, rec.PhotoUID, delErr)
	}
	if dropped {
		res.RenditionsDropped++
	}
	return nil
}

// repairOrphanSegments deletes the objects under the streaming prefix that no
// recorded rendition claims, when that deletion is explicitly selected.
//
// It is off unless asked for, and it is the only part of the integrity check that
// removes anything from the store. The reason is the encode itself: it publishes
// a rendition's objects *before* it writes the row describing them, so for the
// length of a transcode a healthy job's uploads are indistinguishable from
// abandoned ones. Every object belonging to a video with an unfinished
// hls_transcode job is therefore excluded and counted as kept — the queue is
// re-read here rather than trusted from the scan, so an encode that started in
// between is still safe.
func (s *Service) repairOrphanSegments(ctx context.Context, opts RepairOptions, res *RepairResult) error {
	if !opts.DeleteOrphanSegments {
		return nil
	}
	if !s.streaming.enabled() {
		return ErrStreamingUnavailable
	}
	doomed, kept, err := s.collectOrphanSegments(ctx)
	if err != nil {
		return err
	}
	res.OrphanSegmentsKept = kept
	for _, key := range doomed {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("maintenance: orphan segment sweep interrupted: %w", ctxErr)
		}
		if err := s.deleteOrphanSegment(ctx, key); err != nil {
			return err
		}
		res.OrphanSegmentsDeleted++
	}
	return nil
}

// deleteOrphanSegment removes one orphan object, treating an object that is
// already gone as done rather than as a failure.
func (s *Service) deleteOrphanSegment(ctx context.Context, key string) error {
	if err := s.streaming.Segments.Delete(ctx, key); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("maintenance: deleting orphan streaming object %s: %w", key, err)
	}
	return nil
}

// collectOrphanSegments lists the streaming prefix and separates the objects
// nothing claims from those an unfinished encode could still be writing,
// returning the former sorted and the count of the latter.
//
// The keys are collected before anything is deleted so the store is never mutated
// while it is being walked — a filesystem backend walks a directory tree, and
// removing entries from under a walk is how a sweep silently skips half of them.
func (s *Service) collectOrphanSegments(ctx context.Context) (doomed []string, kept int, err error) {
	recorded, err := s.streaming.Renditions.ListRecorded(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("maintenance: listing recorded renditions: %w", err)
	}
	encoding, err := s.encodingHashes(ctx)
	if err != nil {
		return nil, 0, err
	}
	live := recordedPrefixes(recorded)
	doomed = make([]string, 0)
	walkErr := s.streaming.Segments.KeysWithPrefix(ctx, hls.Prefix+"/", func(key string) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("maintenance: orphan segment sweep interrupted: %w", ctxErr)
		}
		ref := parseSegmentKey(key)
		switch {
		case inSet(live, ref.group):
			return nil
		case ref.shaped && inSet(encoding, ref.fileHash):
			kept++
			return nil
		}
		doomed = append(doomed, key)
		return nil
	})
	if walkErr != nil {
		return nil, 0, fmt.Errorf("maintenance: listing streaming objects: %w", walkErr)
	}
	sort.Strings(doomed)
	return doomed, kept, nil
}
