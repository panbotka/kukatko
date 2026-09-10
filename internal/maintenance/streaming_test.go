package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/hlsjob"
)

// videoHash is a well-formed file hash used throughout the streaming tests; a
// malformed one would be rejected by hls.Key before the store is ever asked.
const videoHash = "1111111111111111111111111111111111111111111111111111111111111111"

// otherHash is a second well-formed file hash, for the video whose objects are
// nobody's.
const otherHash = "2222222222222222222222222222222222222222222222222222222222222222"

// fakeRenditions is a StreamingCatalog over fixed slices. It records the rows a
// repair deleted so a test can assert the catalogue, not just the counter.
type fakeRenditions struct {
	recorded []hlsjob.Recorded
	encoding []string
	deleted  []string
	// missing makes Delete report "there was no row", the state a concurrent run
	// leaves behind.
	missing bool
	// listErr fails the listing, the shape a database outage takes.
	listErr error
}

func (f *fakeRenditions) ListRecorded(context.Context) ([]hlsjob.Recorded, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.recorded, nil
}

func (f *fakeRenditions) EncodingHashes(context.Context) ([]string, error) {
	return f.encoding, nil
}

func (f *fakeRenditions) Delete(_ context.Context, photoUID, rendition string) (bool, error) {
	f.deleted = append(f.deleted, photoUID+"/"+rendition)
	return !f.missing, nil
}

// fakeSegments is a SegmentStore over an in-memory key→size map. Deletes are
// applied to the map, so a sweep's effect is observable.
type fakeSegments struct {
	objects map[string]int64
	deleted []string
}

func (f *fakeSegments) KeysWithPrefix(_ context.Context, prefix string, yield func(string) error) error {
	keys := make([]string, 0, len(f.objects))
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := yield(key); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeSegments) Stat(_ context.Context, relPath string) (os.FileInfo, error) {
	size, ok := f.objects[relPath]
	if !ok {
		return nil, os.ErrNotExist
	}
	return fakeFileInfo{size: size}, nil
}

func (f *fakeSegments) Delete(_ context.Context, relPath string) error {
	if _, ok := f.objects[relPath]; !ok {
		return os.ErrNotExist
	}
	delete(f.objects, relPath)
	f.deleted = append(f.deleted, relPath)
	return nil
}

// fakeFileInfo is the minimum os.FileInfo the size of an object travels in.
type fakeFileInfo struct {
	os.FileInfo
	size int64
}

func (f fakeFileInfo) Size() int64 { return f.size }

// segKey spells one streaming object key, failing the test on a malformed part —
// which would mean the test itself, not the code, is wrong.
func segKey(t *testing.T, hash, rendition, name string) string {
	t.Helper()
	key, err := hls.Key(hash, rendition, name)
	if err != nil {
		t.Fatalf("hls.Key(%s, %s, %s): %v", hash, rendition, name, err)
	}
	return key
}

// streamingService builds a Service whose only interesting collaborators are the
// streaming ones: one video in the catalogue, and the given rendition rows and
// objects. Everything else is an empty fake, so a report's other findings stay
// zero and the streaming half is what a test reads.
func streamingService(videos int, rend *fakeRenditions, seg *fakeSegments) *Service {
	return New(Config{
		Photos:    &fakePhotos{videos: videos},
		Vectors:   &fakeVectors{},
		Originals: fakeOriginals{},
		Store:     fakeStore{},
		Thumbs:    fakeThumbs{},
		Enqueuer:  &fakeEnqueuer{},
		Embed:     &fakeBackfiller{},
		Faces:     &fakeFaceBackfiller{},
		FaceCache: &fakeFaceCache{},
		Streaming: Streaming{Renditions: rend, Segments: seg},
	})
}

// healthyStreaming is one recorded rendition with all of its objects present.
func healthyStreaming(t *testing.T) (*fakeRenditions, *fakeSegments) {
	t.Helper()
	return &fakeRenditions{
			recorded: []hlsjob.Recorded{
				{PhotoUID: "v1", Rendition: hls.Rendition1080p, FileHash: videoHash},
			},
		}, &fakeSegments{objects: map[string]int64{
			segKey(t, videoHash, hls.Rendition1080p, hls.InitName): 800,
			segKey(t, videoHash, hls.Rendition1080p, "00000.m4s"):  4096,
			segKey(t, videoHash, hls.Rendition1080p, "00001.m4s"):  4096,
			"2023/06/" + videoHash[:8] + "-clip.mp4":               1 << 20,
		}}
}

// TestParseSegmentKey covers the shapes an object key under the streaming prefix
// can take, including the ones the layout could never have produced.
func TestParseSegmentKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		key           string
		wantHash      string
		wantRendition string
		wantGroup     string
		shaped        bool
	}{
		{
			name:          "media segment",
			key:           "hls/abc/1080p/00007.m4s",
			wantHash:      "abc",
			wantRendition: "1080p",
			wantGroup:     "hls/abc/1080p/",
			shaped:        true,
		},
		{
			name:          "initialisation segment",
			key:           "hls/abc/1080p/init.mp4",
			wantHash:      "abc",
			wantRendition: "1080p",
			wantGroup:     "hls/abc/1080p/",
			shaped:        true,
		},
		{
			name:          "nested junk still groups by its rendition",
			key:           "hls/abc/1080p/deeper/thing",
			wantHash:      "abc",
			wantRendition: "1080p",
			wantGroup:     "hls/abc/1080p/",
			shaped:        true,
		},
		{
			name:      "no rendition level",
			key:       "hls/abc/loose.m4s",
			wantGroup: "hls/abc/loose.m4s",
		},
		{
			name:      "bare prefix child",
			key:       "hls/loose",
			wantGroup: "hls/loose",
		},
		{
			name:      "outside the prefix",
			key:       "2023/06/photo.jpg",
			wantGroup: "2023/06/photo.jpg",
		},
		{
			name:      "empty rendition",
			key:       "hls/abc//00000.m4s",
			wantGroup: "hls/abc//00000.m4s",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseSegmentKey(tt.key)
			if got.shaped != tt.shaped {
				t.Errorf("parseSegmentKey(%q).shaped = %v, want %v", tt.key, got.shaped, tt.shaped)
			}
			if got.fileHash != tt.wantHash {
				t.Errorf("parseSegmentKey(%q).fileHash = %q, want %q", tt.key, got.fileHash, tt.wantHash)
			}
			if got.rendition != tt.wantRendition {
				t.Errorf("parseSegmentKey(%q).rendition = %q, want %q",
					tt.key, got.rendition, tt.wantRendition)
			}
			if got.group != tt.wantGroup {
				t.Errorf("parseSegmentKey(%q).group = %q, want %q", tt.key, got.group, tt.wantGroup)
			}
		})
	}
}

// TestRecordedPrefixes verifies a row contributes its prefix only when it can
// spell an object key at all — a row that cannot claims nothing and so excuses
// nothing.
func TestRecordedPrefixes(t *testing.T) {
	t.Parallel()

	got := recordedPrefixes([]hlsjob.Recorded{
		{PhotoUID: "v1", Rendition: hls.Rendition1080p, FileHash: videoHash},
		{PhotoUID: "v2", Rendition: "720p", FileHash: "NOT-HEX"},
		{PhotoUID: "v3", Rendition: "../escape", FileHash: otherHash},
	})
	want := "hls/" + videoHash + "/" + hls.Rendition1080p + "/"
	if len(got) != 1 {
		t.Fatalf("recordedPrefixes = %v, want exactly one prefix", got)
	}
	if !inSet(got, want) {
		t.Errorf("recordedPrefixes = %v, want %q", got, want)
	}
}

// TestClaimed verifies what excuses an object from being an orphan: a recorded
// rendition, or an encode in flight over a key the layout could have produced.
func TestClaimed(t *testing.T) {
	t.Parallel()

	live := map[string]struct{}{"hls/abc/1080p/": {}}
	encoding := map[string]struct{}{"def": {}}
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "object of a recorded rendition", key: "hls/abc/1080p/00000.m4s", want: true},
		{name: "object of a video being encoded", key: "hls/def/720p/00000.m4s", want: true},
		{name: "object of neither", key: "hls/ghi/1080p/00000.m4s", want: false},
		{name: "unshaped key is never excused", key: "hls/def", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := claimed(parseSegmentKey(tt.key), live, encoding); got != tt.want {
				t.Errorf("claimed(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// TestOrphanSweepSamplesPrefixesOnce verifies the sweep counts every object but
// samples each rendition prefix once, and within the configured budget.
func TestOrphanSweepSamplesPrefixesOnce(t *testing.T) {
	t.Parallel()

	sweep := newOrphanSweep(1)
	sweep.add("hls/a/1080p/", 10)
	sweep.add("hls/a/1080p/", 10)
	sweep.add("hls/b/1080p/", 5)
	got := sweep.result()

	if got.Count != 3 {
		t.Errorf("Count = %d, want 3", got.Count)
	}
	if got.Bytes != 25 {
		t.Errorf("Bytes = %d, want 25", got.Bytes)
	}
	if len(got.Samples) != 1 || got.Samples[0] != "hls/a/1080p/" {
		t.Errorf("Samples = %v, want [hls/a/1080p/]", got.Samples)
	}
}

// TestSegmentOrphansJSONShape verifies the orphan finding serialises flat —
// count, samples and bytes at one level — since that is the shape docs/API.md
// promises and a client reads.
func TestSegmentOrphansJSONShape(t *testing.T) {
	t.Parallel()

	got, err := json.Marshal(SegmentOrphans{
		Finding: Finding{Count: 2, Samples: []string{"hls/a/1080p/"}}, Bytes: 99,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"count":2,"samples":["hls/a/1080p/"],"bytes":99}`
	if string(got) != want {
		t.Errorf("SegmentOrphans JSON = %s, want %s", got, want)
	}
}

// TestScanStreamingHealthy verifies a fully encoded video produces no finding at
// all — neither a missing rendition nor an orphan out of its own segments.
func TestScanStreamingHealthy(t *testing.T) {
	t.Parallel()
	rend, seg := healthyStreaming(t)

	report, err := streamingService(1, rend, seg).Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.MissingRenditions.Count != 0 {
		t.Errorf("MissingRenditions = %+v, want none", report.MissingRenditions)
	}
	if report.OrphanSegments.Count != 0 || report.OrphanSegments.Bytes != 0 {
		t.Errorf("OrphanSegments = %+v, want none", report.OrphanSegments)
	}
}

// TestScanStreamingMissingRendition verifies a row whose objects are gone is
// reported, named by photo and rendition.
func TestScanStreamingMissingRendition(t *testing.T) {
	t.Parallel()
	rend, seg := healthyStreaming(t)
	seg.objects = map[string]int64{}

	report, err := streamingService(1, rend, seg).Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	want := "v1/" + hls.Rendition1080p
	if report.MissingRenditions.Count != 1 || report.MissingRenditions.Samples[0] != want {
		t.Errorf("MissingRenditions = %+v, want one sample %q", report.MissingRenditions, want)
	}
	if report.Clean() {
		t.Error("a report with a missing rendition must not be Clean")
	}
}

// TestScanStreamingOrphans verifies objects no rendition claims are counted with
// their weight and sampled by prefix, while an in-flight encode's objects are
// left out of the finding entirely.
func TestScanStreamingOrphans(t *testing.T) {
	t.Parallel()
	rend, seg := healthyStreaming(t)
	seg.objects[segKey(t, otherHash, "720p", hls.InitName)] = 700
	seg.objects[segKey(t, otherHash, "720p", "00000.m4s")] = 1300
	// A third video whose objects belong to an encode that has not finished yet.
	encodingHash := "3333333333333333333333333333333333333333333333333333333333333333"
	seg.objects[segKey(t, encodingHash, hls.Rendition1080p, hls.InitName)] = 999
	rend.encoding = []string{encodingHash}

	report, err := streamingService(1, rend, seg).Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.OrphanSegments.Count != 2 || report.OrphanSegments.Bytes != 2000 {
		t.Errorf("OrphanSegments = %+v, want 2 objects of 2000 bytes", report.OrphanSegments)
	}
	want := "hls/" + otherHash + "/720p/"
	if len(report.OrphanSegments.Samples) != 1 || report.OrphanSegments.Samples[0] != want {
		t.Errorf("OrphanSegments.Samples = %v, want [%s]", report.OrphanSegments.Samples, want)
	}
}

// TestScanStreamingOffAsksNothing verifies an instance that does not stream, and
// one with no videos, produce no findings and touch neither the catalogue nor the
// store.
func TestScanStreamingOffAsksNothing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		videos    int
		streaming bool
	}{
		{name: "streaming switched off", videos: 3, streaming: false},
		{name: "no videos in the library", videos: 0, streaming: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// A listing failure the moment anything asks: if the scan touches the
			// store at all, the fake's objects would surface as orphans.
			rend := &fakeRenditions{listErr: errors.New("must not be asked")}
			seg := &fakeSegments{objects: map[string]int64{"hls/x/1080p/00000.m4s": 1}}
			svc := streamingService(tt.videos, rend, seg)
			if !tt.streaming {
				svc.streaming = Streaming{}
			}

			report, err := svc.Scan(context.Background())
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if report.MissingRenditions.Count != 0 || report.OrphanSegments.Count != 0 {
				t.Errorf("findings = %+v / %+v, want none",
					report.MissingRenditions, report.OrphanSegments)
			}
			if report.OrphanSegments.Samples == nil || report.MissingRenditions.Samples == nil {
				t.Error("empty findings must carry a non-nil sample slice")
			}
		})
	}
}

// TestRepairMissingRenditionsDropsOnlyBrokenRows verifies the repair withdraws
// exactly the rows the store cannot back, and leaves the healthy one alone.
func TestRepairMissingRenditionsDropsOnlyBrokenRows(t *testing.T) {
	t.Parallel()
	rend, seg := healthyStreaming(t)
	rend.recorded = append(rend.recorded,
		hlsjob.Recorded{PhotoUID: "v2", Rendition: "720p", FileHash: otherHash})

	res, err := streamingService(2, rend, seg).Repair(
		context.Background(), RepairOptions{MissingRenditions: true}, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.RenditionsDropped != 1 {
		t.Errorf("RenditionsDropped = %d, want 1", res.RenditionsDropped)
	}
	if len(rend.deleted) != 1 || rend.deleted[0] != "v2/720p" {
		t.Errorf("deleted = %v, want [v2/720p]", rend.deleted)
	}
	if len(seg.deleted) != 0 {
		t.Errorf("the rendition repair deleted objects %v; it must remove no media", seg.deleted)
	}
}

// TestRepairMissingRenditionsCountsOnlyRealDrops verifies a row another run
// already removed is not counted again, so re-running converges.
func TestRepairMissingRenditionsCountsOnlyRealDrops(t *testing.T) {
	t.Parallel()
	rend, seg := healthyStreaming(t)
	rend.recorded = []hlsjob.Recorded{{PhotoUID: "v2", Rendition: "720p", FileHash: otherHash}}
	rend.missing = true

	res, err := streamingService(1, rend, seg).Repair(
		context.Background(), RepairOptions{MissingRenditions: true}, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.RenditionsDropped != 0 {
		t.Errorf("RenditionsDropped = %d, want 0 for a row that was already gone", res.RenditionsDropped)
	}
}

// TestRepairOrphanSegments verifies the sweep deletes what nothing claims, keeps
// what an unfinished encode is still writing, and never touches a recorded
// rendition's objects.
func TestRepairOrphanSegments(t *testing.T) {
	t.Parallel()
	rend, seg := healthyStreaming(t)
	orphan := segKey(t, otherHash, "720p", "00000.m4s")
	seg.objects[orphan] = 1300
	encodingHash := "3333333333333333333333333333333333333333333333333333333333333333"
	inFlight := segKey(t, encodingHash, hls.Rendition1080p, hls.InitName)
	seg.objects[inFlight] = 999
	rend.encoding = []string{encodingHash}

	res, err := streamingService(1, rend, seg).Repair(
		context.Background(), RepairOptions{DeleteOrphanSegments: true}, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.OrphanSegmentsDeleted != 1 || res.OrphanSegmentsKept != 1 {
		t.Errorf("deleted=%d kept=%d, want 1/1", res.OrphanSegmentsDeleted, res.OrphanSegmentsKept)
	}
	if len(seg.deleted) != 1 || seg.deleted[0] != orphan {
		t.Errorf("deleted = %v, want [%s]", seg.deleted, orphan)
	}
	if _, ok := seg.objects[inFlight]; !ok {
		t.Error("the sweep deleted an object an unfinished encode is still writing")
	}
	if _, ok := seg.objects[segKey(t, videoHash, hls.Rendition1080p, hls.InitName)]; !ok {
		t.Error("the sweep deleted a recorded rendition's object")
	}
}

// TestScanReportsOrphansTheSweepWouldNotDelete verifies the scan reports orphan
// objects without removing any of them: reporting is what it does, deleting has
// to be asked for.
func TestScanReportsOrphansTheSweepWouldNotDelete(t *testing.T) {
	t.Parallel()
	rend, seg := healthyStreaming(t)
	orphan := segKey(t, otherHash, "720p", "00000.m4s")
	seg.objects[orphan] = 1300

	if _, err := streamingService(1, rend, seg).Scan(context.Background()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, ok := seg.objects[orphan]; !ok {
		t.Error("the scan deleted an orphan object; it must only report")
	}
	if len(seg.deleted) != 0 {
		t.Errorf("the scan deleted %v; a scan is read-only", seg.deleted)
	}
}

// TestStreamingRepairsRefuseWithoutStreaming verifies both streaming repairs
// refuse on an instance that does not stream, rather than acting on a feature
// whose jobs nobody runs.
func TestStreamingRepairsRefuseWithoutStreaming(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts RepairOptions
	}{
		{name: "drop broken rows", opts: RepairOptions{MissingRenditions: true}},
		{name: "sweep orphans", opts: RepairOptions{DeleteOrphanSegments: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := streamingService(1, &fakeRenditions{}, &fakeSegments{})
			svc.streaming = Streaming{}
			_, err := svc.Repair(context.Background(), tt.opts, audit.Meta{})
			if !errors.Is(err, ErrStreamingUnavailable) {
				t.Errorf("Repair error = %v, want ErrStreamingUnavailable", err)
			}
		})
	}
}
