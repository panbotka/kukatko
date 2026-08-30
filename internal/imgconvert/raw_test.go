package imgconvert

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The three real-world shapes the ranking has to get right, measured with
// exiftool against the RAW files in the production library (see
// docs/specs/task-9f65bf54…): a Nikon body that hides the near-full-resolution
// image in JpgFromRaw and leaves PreviewImage thumbnail-sized, a Sony body that
// carries no JpgFromRaw at all, and a file that only has JpgFromRaw.
var (
	previewTag   = rawPreviewTags[0]
	jpgFromRaw   = rawPreviewTags[1]
	thumbnailTag = rawPreviewTags[2]
)

// TestRankPreviewCandidates covers the selection itself: which embedded image
// wins for each real-world shape, and how candidates whose JPEG header could not
// be parsed are ordered. It drives the tag table directly, so no RAW file is
// needed to pin the decision.
func TestRankPreviewCandidates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidates []rawPreviewCandidate
		want       []string
	}{
		{
			name: "nikon d3300: small PreviewImage loses to full-size JpgFromRaw",
			candidates: []rawPreviewCandidate{
				{tag: previewTag, size: 145759, pixels: 640 * 424},
				{tag: jpgFromRaw, size: 1438287, pixels: 6000 * 4000},
			},
			want: []string{"JpgFromRaw", "PreviewImage"},
		},
		{
			name: "nikon d5100: same shape at the other measured resolution",
			candidates: []rawPreviewCandidate{
				{tag: previewTag, size: 92000, pixels: 570 * 375},
				{tag: jpgFromRaw, size: 1900000, pixels: 4928 * 3264},
			},
			want: []string{"JpgFromRaw", "PreviewImage"},
		},
		{
			name: "sony arw: PreviewImage wins over the 160x120 thumbnail",
			candidates: []rawPreviewCandidate{
				{tag: previewTag, size: 376176, pixels: 1616 * 1080},
				{tag: thumbnailTag, size: 2594, pixels: 160 * 120},
			},
			want: []string{"PreviewImage", "ThumbnailImage"},
		},
		{
			name:       "only JpgFromRaw present",
			candidates: []rawPreviewCandidate{{tag: jpgFromRaw, size: 1438287, pixels: 6000 * 4000}},
			want:       []string{"JpgFromRaw"},
		},
		{
			name: "an unreadable header ranks below every measured candidate",
			candidates: []rawPreviewCandidate{
				{tag: previewTag, size: 9_000_000, pixels: 0},
				{tag: jpgFromRaw, size: 145759, pixels: 640 * 424},
			},
			want: []string{"JpgFromRaw", "PreviewImage"},
		},
		{
			name: "unreadable headers fall back to byte size among themselves",
			candidates: []rawPreviewCandidate{
				{tag: previewTag, size: 2594, pixels: 0},
				{tag: jpgFromRaw, size: 145759, pixels: 0},
			},
			want: []string{"JpgFromRaw", "PreviewImage"},
		},
		{
			name: "equal pixel counts keep the preference order",
			candidates: []rawPreviewCandidate{
				{tag: previewTag, size: 1000, pixels: 640 * 424},
				{tag: jpgFromRaw, size: 1000, pixels: 640 * 424},
			},
			want: []string{"PreviewImage", "JpgFromRaw"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rankPreviewCandidates(tc.candidates)
			got := make([]string, 0, len(tc.candidates))
			for _, candidate := range tc.candidates {
				got = append(got, candidate.tag.data)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("ranked order = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestParsePreviewLocations feeds the parser the exact answers exiftool gives
// for the production RAWs — one line per requested tag, "-" where the file
// carries none — plus the malformed shapes that must be rejected so a garbled
// answer falls back to the plain preference order.
func TestParsePreviewLocations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		out     string
		want    []previewLocation
		wantErr bool
	}{
		{
			name: "nikon nef: PreviewImage and JpgFromRaw, no thumbnail",
			out:  "27648\n145759\n896512\n1438287\n-\n-\n",
			want: []previewLocation{
				{offset: 27648, size: 145759},
				{offset: 896512, size: 1438287},
				{},
			},
		},
		{
			name: "sony arw: PreviewImage and thumbnail, no JpgFromRaw",
			out:  "163891\n376176\n-\n-\n47796\n2594\n",
			want: []previewLocation{
				{offset: 163891, size: 376176},
				{},
				{offset: 47796, size: 2594},
			},
		},
		{
			name: "a non-numeric value reads as absent",
			out:  "-\n-\n(unknown)\n1438287\n-\n-\n",
			want: []previewLocation{{}, {size: 1438287}, {}},
		},
		{
			name:    "too few lines is an error",
			out:     "27648\n145759\n",
			wantErr: true,
		},
		{
			name:    "empty output is an error",
			out:     "",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parsePreviewLocations(tc.out)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parsePreviewLocations(%q) = %v, want an error", tc.out, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePreviewLocations(%q) error = %v", tc.out, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d locations, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("location[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestJpegPixelsAt verifies the pixel count is read from the JPEG header sitting
// at an arbitrary offset inside a bigger file — the way an embedded preview sits
// inside a RAW — and that a location holding no JPEG measures as unknown.
func TestJpegPixelsAt(t *testing.T) {
	t.Parallel()

	small := encodeJPEG(t, 32, 16)
	large := encodeJPEG(t, 64, 48)
	var buf bytes.Buffer
	buf.Write(bytes.Repeat([]byte{0x00}, 512))
	smallOffset := int64(buf.Len())
	buf.Write(small)
	largeOffset := int64(buf.Len())
	buf.Write(large)
	src := bytes.NewReader(buf.Bytes())

	if got := jpegPixelsAt(src, smallOffset, int64(len(small))); got != 32*16 {
		t.Errorf("small preview pixels = %d, want %d", got, 32*16)
	}
	if got := jpegPixelsAt(src, largeOffset, int64(len(large))); got != 64*48 {
		t.Errorf("large preview pixels = %d, want %d", got, 64*48)
	}
	if got := jpegPixelsAt(src, 0, 512); got != 0 {
		t.Errorf("padding pixels = %d, want 0 (no JPEG there)", got)
	}
}

// TestExtractPreview_picksLargestAndExtractsOnce drives the whole path against a
// stub exiftool: a Nikon-shaped file whose PreviewImage is small and whose
// JpgFromRaw is the big one. The extracted image must be the large one, and
// exactly one image may be extracted — a per-loser extraction would write
// megabytes on every RAW ingest.
func TestExtractPreview_picksLargestAndExtractsOnce(t *testing.T) {
	small := encodeJPEG(t, 32, 16)
	large := encodeJPEG(t, 64, 48)
	dir := t.TempDir()
	rawPath, meta := writeFakeRAW(t, dir, map[string][]byte{
		"PreviewImage": small,
		"JpgFromRaw":   large,
	})
	logPath := stubExiftool(t, dir, meta)

	dstPath := filepath.Join(dir, "out.jpg")
	if err := os.WriteFile(dstPath, nil, 0o600); err != nil {
		t.Fatalf("create destination: %v", err)
	}
	if err := extractPreview(context.Background(), rawPath, dstPath); err != nil {
		t.Fatalf("extractPreview: %v", err)
	}

	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if !bytes.Equal(got, large) {
		t.Errorf("extracted %d bytes, want the %d-byte JpgFromRaw image", len(got), len(large))
	}
	if n := countExtractions(t, logPath); n != 1 {
		t.Errorf("extracted %d images, want exactly 1 (the winner)", n)
	}
}

// TestExtractPreview_sonyShapeKeepsPreviewImage pins the file the fix must not
// change: an ARW carries no JpgFromRaw, so its PreviewImage stays the winner
// over the 160x120 thumbnail.
func TestExtractPreview_sonyShapeKeepsPreviewImage(t *testing.T) {
	preview := encodeJPEG(t, 64, 48)
	thumb := encodeJPEG(t, 16, 12)
	dir := t.TempDir()
	rawPath, meta := writeFakeRAW(t, dir, map[string][]byte{
		"PreviewImage":   preview,
		"ThumbnailImage": thumb,
	})
	stubExiftool(t, dir, meta)

	dstPath := filepath.Join(dir, "out.jpg")
	if err := os.WriteFile(dstPath, nil, 0o600); err != nil {
		t.Fatalf("create destination: %v", err)
	}
	if err := extractPreview(context.Background(), rawPath, dstPath); err != nil {
		t.Fatalf("extractPreview: %v", err)
	}
	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if !bytes.Equal(got, preview) {
		t.Error("extracted image is not the PreviewImage the ARW shape must keep")
	}
}

// TestExtractPreview_noPreview confirms a RAW that advertises no embedded image
// at all still reports the typed ErrNoEmbeddedPreview.
func TestExtractPreview_noPreview(t *testing.T) {
	dir := t.TempDir()
	rawPath, meta := writeFakeRAW(t, dir, nil)
	stubExiftool(t, dir, meta)

	dstPath := filepath.Join(dir, "out.jpg")
	if err := os.WriteFile(dstPath, nil, 0o600); err != nil {
		t.Fatalf("create destination: %v", err)
	}
	err := extractPreview(context.Background(), rawPath, dstPath)
	if !errors.Is(err, ErrNoEmbeddedPreview) {
		t.Errorf("error = %v, want ErrNoEmbeddedPreview", err)
	}
}

// encodeJPEG returns a real JPEG of the given dimensions, so a test can assert
// on pixel counts read from an actual frame header.
func encodeJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 0xFF, A: 0xFF})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode %dx%d jpeg: %v", width, height, err)
	}
	return buf.Bytes()
}

// writeFakeRAW builds a file that looks like a RAW to the code under test: some
// padding, then each named embedded JPEG at a known offset. It returns the file's
// path plus the metadata answer a stub exiftool should give for it — one
// offset/length line pair per rawPreviewTags entry, "-" for the tags the file
// does not carry.
func writeFakeRAW(t *testing.T, dir string, images map[string][]byte) (string, string) {
	t.Helper()
	var body bytes.Buffer
	body.Write(bytes.Repeat([]byte{0x49, 0x49, 0x2A, 0x00}, 64)) // a TIFF-ish head
	var meta strings.Builder
	for _, tag := range rawPreviewTags {
		img, ok := images[tag.data]
		if !ok {
			meta.WriteString("-\n-\n")
			continue
		}
		meta.WriteString(strconv.Itoa(body.Len()) + "\n")
		meta.WriteString(strconv.Itoa(len(img)) + "\n")
		body.Write(img)
	}
	path := filepath.Join(dir, "shot.nef")
	if err := os.WriteFile(path, body.Bytes(), 0o600); err != nil {
		t.Fatalf("write fake raw: %v", err)
	}
	return path, meta.String()
}

// stubExiftool puts a fake exiftool on PATH for the duration of the test: it
// answers the metadata query with the given canned lines and serves `-b <tag>`
// by copying the bytes at the offset that answer advertises. Every invocation is
// appended to a log file, whose path is returned, so a test can count how many
// images were actually extracted.
func stubExiftool(t *testing.T, dir, meta string) string {
	t.Helper()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o750); err != nil {
		t.Fatalf("create stub bin dir: %v", err)
	}
	metaPath := filepath.Join(dir, "meta.txt")
	if err := os.WriteFile(metaPath, []byte(meta), 0o600); err != nil {
		t.Fatalf("write stub metadata: %v", err)
	}
	logPath := filepath.Join(dir, "calls.log")
	script := stubExiftoolScript(metaPath, logPath, meta)
	if err := os.WriteFile(filepath.Join(binDir, exiftoolBinary), []byte(script), 0o700); err != nil {
		t.Fatalf("write stub exiftool: %v", err)
	}
	// The stub shells out to dd/cat, so the real PATH stays reachable behind it.
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// stubExiftoolScript renders the shell stub: `-b <tag>` writes the advertised
// byte range of the source file to stdout (nothing at all for a tag the file
// lacks, exactly as exiftool does), anything else prints the canned metadata.
func stubExiftoolScript(metaPath, logPath, meta string) string {
	lines := strings.Split(strings.TrimRight(meta, "\n"), "\n")
	var cases strings.Builder
	for i, tag := range rawPreviewTags {
		if lines[2*i] == "-" {
			continue
		}
		cases.WriteString("    -" + tag.data + ") dd if=\"$src\" bs=1 skip=" + lines[2*i] +
			" count=" + lines[2*i+1] + " 2>/dev/null; exit 0 ;;\n")
	}
	return `#!/bin/sh
echo "$@" >> "` + logPath + `"
if [ "$1" = "-b" ]; then
  src="$3"
  case "$2" in
` + cases.String() + `    *) exit 0 ;;
  esac
fi
cat "` + metaPath + `"
`
}

// countExtractions reports how many `-b` (image extraction) invocations the stub
// exiftool log recorded.
func countExtractions(t *testing.T, logPath string) int {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read stub log: %v", err)
	}
	n := 0
	for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.HasPrefix(line, "-b ") {
			n++
		}
	}
	return n
}
