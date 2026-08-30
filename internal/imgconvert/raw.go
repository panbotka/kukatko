package imgconvert

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// exiftoolBinary extracts the embedded JPEG preview from a RAW file.
	exiftoolBinary = "exiftool"
	// rawTimeout caps a single exiftool invocation. Reading an embedded preview
	// is cheap (no demosaic); 60s is a generous backstop on a slow device.
	rawTimeout = 60 * time.Second
	// rawMissingValue is what exiftool -f prints for a tag the file does not
	// carry, so the answer keeps one line per requested tag.
	rawMissingValue = "-"
)

// rawPreviewTag names the three exiftool tags describing one embedded image: the
// binary tag that holds the JPEG itself, plus the offset/length pair that
// advertises it in the file's metadata. The pair is what makes picking the
// largest preview cheap — it is read without touching a single byte of image
// data, and the JPEG header at that offset gives the true pixel dimensions.
type rawPreviewTag struct {
	// data is the binary tag `exiftool -b` extracts the image from.
	data string
	// offset is the tag holding the image's absolute position in the file.
	offset string
	// length is the tag holding the image's size in bytes.
	length string
}

// rawPreviewTags lists the exiftool binary tags that may hold an embedded JPEG,
// in the order they are tried when the metadata says nothing about their size.
// Different vendors use different tags: most store PreviewImage, while some
// (e.g. Nikon bodies) put the near-full-resolution image in JpgFromRaw and leave
// PreviewImage thumbnail-sized. ThumbnailImage is a small last resort that still
// beats a full demosaic.
var rawPreviewTags = []rawPreviewTag{
	{data: "PreviewImage", offset: "PreviewImageStart", length: "PreviewImageLength"},
	{data: "JpgFromRaw", offset: "JpgFromRawStart", length: "JpgFromRawLength"},
	{data: "ThumbnailImage", offset: "ThumbnailOffset", length: "ThumbnailLength"},
}

// rawPreviewCandidate is one embedded JPEG a RAW file advertises: which tag
// holds it, how big it is in bytes, and — when its JPEG header could be read at
// the advertised offset — how many pixels it actually has.
type rawPreviewCandidate struct {
	// tag is the entry of rawPreviewTags this candidate came from.
	tag rawPreviewTag
	// size is the image's length in bytes as reported by the metadata.
	size int64
	// pixels is width×height of the JPEG at the advertised offset, or 0 when
	// the header could not be parsed there.
	pixels int64
}

// convertRAW extracts the largest available embedded JPEG preview from the RAW
// file at srcPath using exiftool, writes it to a temporary JPEG, and returns
// the path plus a once-only cleanup function. It deliberately avoids a full
// demosaic. If exiftool is not on PATH the returned error wraps
// ErrConverterMissing; if no preview tag yields data it wraps
// ErrNoEmbeddedPreview.
func convertRAW(ctx context.Context, srcPath string) (string, func(), error) {
	if _, err := exec.LookPath(exiftoolBinary); err != nil {
		return "", nil, fmt.Errorf("%w: %s lookup: %w", ErrConverterMissing, exiftoolBinary, err)
	}

	tmpPath, cleanup, err := createTempJPEG("kukatko-raw-*.jpg")
	if err != nil {
		return "", nil, err
	}

	if err := extractPreview(ctx, srcPath, tmpPath); err != nil {
		cleanup()
		return "", nil, err
	}
	return tmpPath, cleanup, nil
}

// extractPreview writes the largest embedded JPEG of srcPath to dstPath. It
// first asks the file's metadata how big each candidate is — cheap, no image
// data is read — and then extracts exactly one image: the winner. The remaining
// tags are only reached if extracting the winner fails, so the usual cost is a
// single write. It returns ErrNoEmbeddedPreview (wrapped) when no tag yields
// anything.
func extractPreview(ctx context.Context, srcPath, dstPath string) error {
	for _, tag := range previewOrder(ctx, srcPath) {
		n, err := runExiftoolToFile(ctx, srcPath, "-"+tag.data, dstPath)
		if err == nil && n > 0 {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrNoEmbeddedPreview, filepath.Base(srcPath))
}

// previewOrder returns the preview tags of srcPath to try, largest embedded
// image first. Tags the metadata says nothing about keep their rawPreviewTags
// order and are appended last, so a file whose previews are not advertised by an
// offset/length pair still gets exactly the behaviour it had before.
func previewOrder(ctx context.Context, srcPath string) []rawPreviewTag {
	candidates := previewCandidates(ctx, srcPath)
	rankPreviewCandidates(candidates)

	order := make([]rawPreviewTag, 0, len(rawPreviewTags))
	for _, candidate := range candidates {
		order = append(order, candidate.tag)
	}
	for _, tag := range rawPreviewTags {
		if !slices.ContainsFunc(order, func(t rawPreviewTag) bool { return t.data == tag.data }) {
			order = append(order, tag)
		}
	}
	return order
}

// rankPreviewCandidates sorts candidates in place, largest first: by pixel count
// when the JPEG header could be read, and below every measured one — by byte
// size — those whose header could not be parsed. The sort is stable, so equally
// sized candidates keep their rawPreviewTags preference order.
func rankPreviewCandidates(candidates []rawPreviewCandidate) {
	slices.SortStableFunc(candidates, func(a, b rawPreviewCandidate) int {
		switch {
		case a.pixels != b.pixels && (a.pixels == 0 || b.pixels == 0):
			// A measured candidate always outranks an unmeasurable one: a header
			// we cannot parse is not a JPEG we can trust to be bigger.
			if a.pixels == 0 {
				return 1
			}
			return -1
		case a.pixels != b.pixels:
			return cmpDesc(a.pixels, b.pixels)
		default:
			return cmpDesc(a.size, b.size)
		}
	})
}

// cmpDesc compares two counts for a descending sort: it reports -1 when a is
// the larger, +1 when b is, and 0 when they are equal.
func cmpDesc(a, b int64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

// previewCandidates reads the offset and length of every rawPreviewTags entry
// from srcPath's metadata and measures the pixel dimensions of the JPEG at each
// offset. Tags the file does not carry are skipped; a metadata read that fails
// altogether yields no candidates, which leaves the caller with the plain
// preference order.
func previewCandidates(ctx context.Context, srcPath string) []rawPreviewCandidate {
	locations, err := readPreviewLocations(ctx, srcPath)
	if err != nil {
		return nil
	}
	src, err := os.Open(srcPath) //nolint:gosec // G304: srcPath is the caller-supplied path EnsureDecodable stat'ed.
	if err != nil {
		return nil
	}
	defer func() { _ = src.Close() }()

	candidates := make([]rawPreviewCandidate, 0, len(locations))
	for i, loc := range locations {
		if loc.offset <= 0 || loc.size <= 0 {
			continue
		}
		candidates = append(candidates, rawPreviewCandidate{
			tag:    rawPreviewTags[i],
			size:   loc.size,
			pixels: jpegPixelsAt(src, loc.offset, loc.size),
		})
	}
	return candidates
}

// previewLocation is where one embedded image sits inside the RAW file, as the
// metadata advertises it. A zero value means the file does not carry that tag.
type previewLocation struct {
	// offset is the image's absolute byte position in the file.
	offset int64
	// size is the image's length in bytes.
	size int64
}

// readPreviewLocations runs a single metadata-only exiftool invocation and
// returns the offset/length pair of every rawPreviewTags entry, in that order.
// It reads no image data — only the numbers the IFDs already hold — so it costs
// one process start and no I/O over the previews themselves. It returns an error
// when exiftool fails or answers with an unexpected number of lines; callers
// treat that as "the metadata told us nothing".
func readPreviewLocations(ctx context.Context, srcPath string) ([]previewLocation, error) {
	// -s3 prints bare values, -f keeps a line for a tag the file lacks, and -n
	// keeps the numbers unformatted, so the answer is one line per requested tag
	// in the requested order.
	args := make([]string, 0, 4+2*len(rawPreviewTags))
	args = append(args, "-s3", "-f", "-n")
	for _, tag := range rawPreviewTags {
		args = append(args, "-"+tag.offset, "-"+tag.length)
	}
	args = append(args, srcPath)

	cctx, cancel := context.WithTimeout(ctx, rawTimeout)
	defer cancel()

	// #nosec G204 -- srcPath is the caller-supplied path EnsureDecodable stat'ed
	// before dispatch; every other argument is a constant from rawPreviewTags.
	cmd := exec.CommandContext(cctx, exiftoolBinary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("imgconvert: %s sizes %s: %w (stderr: %s)",
			exiftoolBinary, filepath.Base(srcPath), err, stderr.String())
	}
	return parsePreviewLocations(stdout.String())
}

// parsePreviewLocations turns the line-per-tag answer of the metadata query into
// one previewLocation per rawPreviewTags entry. Lines come in offset/length
// pairs in the order the tags were requested; "-" (and anything that is not a
// number) reads as absent, which leaves that entry zero. A line count other than
// two per tag means the answer is not the one we asked for, and is an error.
func parsePreviewLocations(out string) ([]previewLocation, error) {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2*len(rawPreviewTags) {
		return nil, fmt.Errorf("imgconvert: %s sizes: got %d lines, want %d",
			exiftoolBinary, len(lines), 2*len(rawPreviewTags))
	}
	locations := make([]previewLocation, len(rawPreviewTags))
	for i := range locations {
		locations[i] = previewLocation{
			offset: parseTagNumber(lines[2*i]),
			size:   parseTagNumber(lines[2*i+1]),
		}
	}
	return locations, nil
}

// parseTagNumber reads one exiftool value line as a non-negative count,
// returning 0 for the "-" placeholder of a missing tag and for anything that is
// not a plain number.
func parseTagNumber(line string) int64 {
	line = strings.TrimSpace(line)
	if line == rawMissingValue {
		return 0
	}
	n, err := strconv.ParseInt(line, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// jpegPixelsAt returns width×height of the JPEG stored in src at the given
// offset, or 0 when nothing decodable sits there. Only the JPEG header is read
// (image/jpeg stops at the frame header), so measuring a 2 MB preview costs a
// few kilobytes.
func jpegPixelsAt(src io.ReaderAt, offset, size int64) int64 {
	cfg, err := jpeg.DecodeConfig(io.NewSectionReader(src, offset, size))
	if err != nil {
		return 0
	}
	return int64(cfg.Width) * int64(cfg.Height)
}

// runExiftoolToFile runs `exiftool -b <tag> <src>` with stdout streamed
// directly to dstPath (truncating it first), so an arbitrarily large preview is
// never buffered in memory. It returns the number of bytes written. A non-nil
// error or a zero byte count signals the caller to try the next tag.
func runExiftoolToFile(ctx context.Context, srcPath, tag, dstPath string) (int64, error) {
	cctx, cancel := context.WithTimeout(ctx, rawTimeout)
	defer cancel()

	out, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // G304: dstPath is our own temp file.
	if err != nil {
		return 0, fmt.Errorf("imgconvert: open preview temp: %w", err)
	}
	defer func() { _ = out.Close() }()

	var stderr bytes.Buffer
	// #nosec G204 -- srcPath is the caller-supplied path EnsureDecodable stat'ed
	// before dispatch; tag is from the constant rawPreviewTags whitelist.
	cmd := exec.CommandContext(cctx, exiftoolBinary, "-b", tag, srcPath)
	cmd.Stdout = out
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return 0, fmt.Errorf("imgconvert: %s %s %s: %w (stderr: %s)",
			exiftoolBinary, tag, filepath.Base(srcPath), runErr, stderr.String())
	}

	info, statErr := out.Stat()
	if statErr != nil {
		return 0, fmt.Errorf("imgconvert: stat preview temp: %w", statErr)
	}
	return info.Size(), nil
}
