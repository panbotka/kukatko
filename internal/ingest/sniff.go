package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/video"
)

// ErrNotMedia indicates an uploaded file whose content is neither a recognisable
// image nor a recognisable video, whatever its name claims. It is refused before
// anything about it is stored: a file that only *named* itself a photo used to
// become a 0×0 application/octet-stream "photo" with no thumbnail, whose
// face/embedding/OCR jobs retried and failed for ever.
var ErrNotMedia = errors.New("ingest: not a photo or video")

// CodeNotMedia is the stable FileResult.Code of a file refused as ErrNotMedia,
// for a client to translate (and to know a retry would fail again).
const CodeNotMedia = "not_media"

// ErrUnsupportedType indicates a file of a type Kukátko recognises but does not
// take. Today that is AVIF: no decoder a CGO-free binary can reach opens it, so
// an AVIF original would never get a thumbnail, a pHash or any of the sidecar's
// ML, and its jobs would fail until they died. It is refused on every path —
// the signed-in upload, the upload link, `import dir` — by content as well as
// by name, so an AVIF renamed .jpg is refused the same way.
var ErrUnsupportedType = errors.New("ingest: this type of file is not supported")

// CodeUnsupportedType is the stable FileResult.Code of a file refused for its
// type — ErrUnsupportedType here, and the upload link's own extension check.
const CodeUnsupportedType = "unsupported_type"

// sniffLen is how many leading bytes are read to recognise a file. 512 covers
// every signature below, the MPEG transport stream's second sync byte (offset
// 188, or 196 in an M2TS) included.
const sniffLen = 512

// mpegTSPacket and m2tsPacket are the packet sizes of an MPEG transport stream
// and of its timestamped Blu-ray/AVCHD variant (.m2ts/.mts).
const (
	mpegTSPacket = 188
	m2tsPacket   = 192
)

// isoBMFFOffset is where the four-character box type of an ISO Base Media /
// QuickTime file's first box sits, after its 32-bit size.
const isoBMFFOffset = 4

// admit decides whether the staged file at path is media at all, before any of
// it is stored or catalogued. The magic bytes settle the common case for free:
// every format the pipeline ingests has a signature (see looksLikeMedia). Only a
// file whose leading bytes match none of them is asked the slower question — can
// the metadata tools read it as an image or a video — so a format with a
// signature this list does not know (an exotic camera RAW, say) still gets in as
// long as exiftool or ffprobe understands it. It returns ErrNotMedia for a file
// that passes neither test, and ErrUnsupportedType for an AVIF (see isAVIF),
// which would pass both.
func admit(ctx context.Context, path, filename string) error {
	head, err := readHead(path)
	if err != nil {
		return err
	}
	if isAVIF(head, filename) {
		return fmt.Errorf("%w: %s", ErrUnsupportedType, originalName(filename))
	}
	if looksLikeMedia(head) {
		return nil
	}
	if probedAsMedia(ctx, path, filename) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrNotMedia, originalName(filename))
}

// isAVIF reports whether a file is an AVIF by its content or by its name. The
// content alone would do for a real one, but a ".avif" whose bytes say nothing
// recognisable is still not something the pipeline should probe its way into
// taking.
func isAVIF(head []byte, filename string) bool {
	return imgconvert.MagicFormat(head) == imgconvert.FormatAVIF ||
		strings.EqualFold(path.Ext(filename), ".avif")
}

// readHead returns up to sniffLen leading bytes of the file at path.
func readHead(path string) ([]byte, error) {
	file, err := os.Open(path) //nolint:gosec // G304: path is the pipeline's own staged temp file.
	if err != nil {
		return nil, fmt.Errorf("ingest: reopening staged file: %w", err)
	}
	defer func() { _ = file.Close() }()
	head := make([]byte, sniffLen)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("ingest: reading staged file: %w", err)
	}
	return head[:n], nil
}

// looksLikeMedia reports whether head — the first bytes of a file — starts like
// an image or video container the pipeline ingests: a raster or HEIC (via
// imgconvert's signatures, TIFF covering most camera RAWs), a RAW with a header
// of its own, or a video container or elementary stream.
func looksLikeMedia(head []byte) bool {
	return imgconvert.MagicFormat(head) != imgconvert.FormatUnknown || isRAWMagic(head) || isVideoMagic(head)
}

// rawPrefixes are the camera RAW signatures that are not plain TIFF: Olympus ORF
// ("IIRO"/"IIRS"/"MMOR"), Panasonic RW2 ("IIU\0"), Fujifilm RAF, Sigma X3F
// ("FOVb") and Minolta MRW ("\0MRM"). Canon's CR3 is an ISO Base Media file and
// is recognised by its ftyp box with the videos.
var rawPrefixes = [][]byte{
	[]byte("IIRO"), []byte("IIRS"), []byte("MMOR"),
	[]byte("IIU\x00"),
	[]byte("FUJIFILMCCD-RAW"),
	[]byte("FOVb"),
	[]byte("\x00MRM"),
}

// isRAWMagic reports whether head starts with one of rawPrefixes.
func isRAWMagic(head []byte) bool {
	for _, prefix := range rawPrefixes {
		if bytes.HasPrefix(head, prefix) {
			return true
		}
	}
	return false
}

// videoPrefixes are the container signatures found at offset 0: Matroska/WebM
// (EBML), ASF/WMV (its header GUID), FLV, and the MPEG program stream / Annex-B
// elementary stream start codes (.mpg, raw .h264/.hevc).
var videoPrefixes = [][]byte{
	{0x1A, 0x45, 0xDF, 0xA3},
	{0x30, 0x26, 0xB2, 0x75, 0x8E, 0x66, 0xCF, 0x11},
	[]byte("FLV\x01"),
	{0x00, 0x00, 0x01},
	{0x00, 0x00, 0x00, 0x01},
}

// quickTimeBoxes are the box types an ISO Base Media / QuickTime file may open
// with: "ftyp" (MP4, MOV, 3GP, M4V — and the HEIC/AVIF/CR3 stills, which share
// the container) and the top-level atoms of an older QuickTime file that has no
// ftyp at all.
var quickTimeBoxes = []string{"ftyp", "moov", "mdat", "wide", "free", "skip", "pnot"}

// isVideoMagic reports whether head starts like a video container: one of
// videoPrefixes, an ISO Base Media / QuickTime box, a RIFF AVI, or an MPEG
// transport stream (plain or M2TS).
func isVideoMagic(head []byte) bool {
	for _, prefix := range videoPrefixes {
		if bytes.HasPrefix(head, prefix) {
			return true
		}
	}
	if len(head) >= isoBMFFOffset+4 && slices.Contains(quickTimeBoxes, string(head[isoBMFFOffset:isoBMFFOffset+4])) {
		return true
	}
	if len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "AVI " {
		return true
	}
	return isTransportStream(head, 0, mpegTSPacket) || isTransportStream(head, 4, m2tsPacket)
}

// isTransportStream reports whether head carries the MPEG-TS sync byte (0x47) at
// offset and again one packet later. A single 0x47 is one random byte in 256;
// two a packet apart is a stream.
func isTransportStream(head []byte, offset, packet int) bool {
	const syncByte = 0x47
	return len(head) > offset+packet && head[offset] == syncByte && head[offset+packet] == syncByte
}

// probedAsMedia asks the metadata tools whether a file with no recognised
// signature is media after all: ffprobe (or its exiftool fallback) for a file
// named as a video, which must yield a duration, a codec or a frame size;
// exiftool (or the pure-Go fallback) otherwise, which must report pixel
// dimensions or an image/video media type. Random bytes yield none of these.
func probedAsMedia(ctx context.Context, path, filename string) bool {
	if video.IsVideoPath(filename) {
		vm, err := video.Probe(ctx, path)
		return err == nil && vm.HasContainerMetadata()
	}
	meta, err := exif.ExtractNamed(ctx, path, filename)
	if err != nil {
		return false
	}
	return recognisedImage(meta)
}

// recognisedImage reports whether meta is the reading of an actual image or
// video: it has pixel dimensions, or the extractor named an image/video type.
// http.DetectContentType's generic answers (application/octet-stream,
// text/plain) are not recognition.
func recognisedImage(meta exif.Metadata) bool {
	if meta.Width > 0 && meta.Height > 0 {
		return true
	}
	return strings.HasPrefix(meta.Mime, "image/") || strings.HasPrefix(meta.Mime, "video/")
}
