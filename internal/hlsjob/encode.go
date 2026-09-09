package hlsjob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/video"
)

const (
	// ffmpegBinary is the encoder. It is the same binary internal/video shells
	// out to for posters and on-the-fly transcodes.
	ffmpegBinary = "ffmpeg"
	// tempDirPattern names the throwaway directory one rendition is encoded into.
	tempDirPattern = "kukatko-hls-*"
	// initMIME is the media type of the fragmented-MP4 initialisation segment.
	initMIME = "video/mp4"
	// segmentMIME is the media type of a CMAF media segment, as HLS players and
	// storage.detectMIME both expect it.
	segmentMIME = "video/iso.segment"
)

// encodeOne encodes photo into one rendition and records it, leaving the store
// holding exactly the objects the new rendition consists of.
//
// The order is deliberate: publish first, write the row last. A row is a promise
// that the objects it describes can be fetched, so it may only be made once they
// can be; the reverse order would let a player follow a playlist into segments
// that do not exist. If anything fails after some objects have been uploaded,
// the ones this run created are deleted again — objects a previous encode of the
// same rendition had already published are left alone, because they are what its
// still-valid row points at.
func (s *Service) encodeOne(ctx context.Context, photo photos.Photo, srcPath string, r hls.Rendition) error {
	dir, err := os.MkdirTemp(s.tempDir, tempDirPattern)
	if err != nil {
		return fmt.Errorf("hlsjob: creating encode directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	produced, row, err := s.prepare(ctx, photo, srcPath, dir, r)
	if err != nil {
		return err
	}
	prefix, err := renditionPrefix(photo.FileHash, r.Name)
	if err != nil {
		return err
	}
	before := s.existingKeys(ctx, prefix)
	published, err := s.publish(ctx, dir, prefix, produced)
	if err == nil {
		if _, saveErr := s.renditions.Save(ctx, row); saveErr != nil {
			err = fmt.Errorf("hlsjob: recording rendition %s of %s: %w", r.Name, photo.UID, saveErr)
		}
	}
	if err != nil {
		s.remove(ctx, without(published, before))
		return err
	}
	s.remove(ctx, without(before, published))
	return nil
}

// prepare runs ffmpeg into dir and reads back what it produced: the objects to
// publish, and the row that will describe them once they are published.
//
// The picture's dimensions are read off the initialisation segment rather than
// predicted from the source, because ffmpeg applies a container's rotation
// before the scale filter runs — a portrait clip stored as a rotated landscape
// one reaches the filter already upright, and a predicted resolution would
// describe a picture nobody encoded.
func (s *Service) prepare(
	ctx context.Context, photo photos.Photo, srcPath, dir string, r hls.Rendition,
) (encoded, Encoded, error) {
	if err := s.runFFmpeg(ctx, srcPath, dir, r, photo.DurationMs); err != nil {
		return encoded{}, Encoded{}, err
	}
	// #nosec G304 -- dir is our own temp directory and the name is a constant.
	written, err := os.ReadFile(filepath.Join(dir, hls.EncodePlaylistName))
	if err != nil {
		return encoded{}, Encoded{}, fmt.Errorf("hlsjob: reading the playlist ffmpeg wrote: %w", err)
	}
	produced, err := parsePlaylist(string(written))
	if err != nil {
		return encoded{}, Encoded{}, err
	}
	width, height, err := probeDimensions(ctx, filepath.Join(dir, produced.initName))
	if err != nil {
		return encoded{}, Encoded{}, err
	}
	return produced, Encoded{
		PhotoUID:     photo.UID,
		Rendition:    r.Name,
		Playlist:     string(written),
		Width:        width,
		Height:       height,
		Bandwidth:    r.Bandwidth(),
		Codecs:       r.Codecs,
		SegmentCount: len(produced.segments),
		DurationMs:   produced.durationMs,
	}, nil
}

// runFFmpeg encodes srcPath into dir according to r, under a deadline scaled to
// the clip's length. ffmpeg's stderr is carried into the error, since it is the
// only thing that ever explains why an encode failed.
func (s *Service) runFFmpeg(
	ctx context.Context, srcPath, dir string, r hls.Rendition, durationMs *int,
) error {
	cctx, cancel := context.WithTimeout(ctx, encodeTimeout(durationMs))
	defer cancel()

	var stderr bytes.Buffer
	// #nosec G204 -- srcPath is a file this application stored or materialized,
	// dir is our own temp directory, and the remaining arguments are constant
	// flags and integers from the encoding plan.
	cmd := exec.CommandContext(cctx, ffmpegBinary, hls.EncodeArgs(srcPath, dir, r, s.segment)...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hlsjob: ffmpeg %s into %s: %w (stderr: %s)",
			filepath.Base(srcPath), r.Name, err, stderr.String())
	}
	return nil
}

// probeDimensions reads the encoded picture's real dimensions off the
// initialisation segment, which carries the track's sample description and so
// answers without any media segment being read.
func probeDimensions(ctx context.Context, initPath string) (width, height int, err error) {
	meta, err := video.Probe(ctx, initPath)
	if err != nil {
		return 0, 0, fmt.Errorf("hlsjob: probing the encoded stream: %w", err)
	}
	if meta.Width <= 0 || meta.Height <= 0 {
		return 0, 0, fmt.Errorf("hlsjob: the encoded stream reports no picture size (%dx%d)",
			meta.Width, meta.Height)
	}
	return meta.Width, meta.Height, nil
}

// publish uploads the initialisation segment and every media segment from dir
// into the rendition's prefix, returning the keys it wrote — including on
// failure, so the caller can undo exactly what this run created.
func (s *Service) publish(ctx context.Context, dir, prefix string, produced encoded) ([]string, error) {
	names := append([]string{produced.initName}, produced.segments...)
	written := make([]string, 0, len(names))
	for _, name := range names {
		key := prefix + name
		if err := s.putObject(ctx, filepath.Join(dir, name), key, mimeFor(name)); err != nil {
			return written, err
		}
		written = append(written, key)
	}
	return written, nil
}

// putObject uploads one local file to key. The file is digested first and then
// streamed, because the store verifies an upload against the identity it is
// given up front — which is what makes a nil error mean the promised bytes are
// durably in place rather than merely sent.
func (s *Service) putObject(ctx context.Context, path, key, mime string) error {
	sum, size, err := digest(path)
	if err != nil {
		return err
	}
	file, err := os.Open(path) // #nosec G304 -- path is a file ffmpeg wrote into our own temp directory.
	if err != nil {
		return fmt.Errorf("hlsjob: opening %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = file.Close() }()

	if err := s.objects.Put(ctx, file, storage.StoredFile{
		Hash: sum, RelPath: key, Size: size, MIME: mime,
	}); err != nil {
		return fmt.Errorf("hlsjob: publishing %s: %w", key, err)
	}
	return nil
}

// digest returns the SHA256 digest and byte count of the file at path, streaming
// it rather than reading it into memory: a media segment of a long clip is
// megabytes, and there are one per segment length of video.
func digest(path string) (string, int64, error) {
	file, err := os.Open(path) // #nosec G304 -- path is a file ffmpeg wrote into our own temp directory.
	if err != nil {
		return "", 0, fmt.Errorf("hlsjob: opening %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = file.Close() }()

	sum := sha256.New()
	size, err := io.Copy(sum, file)
	if err != nil {
		return "", 0, fmt.Errorf("hlsjob: digesting %s: %w", filepath.Base(path), err)
	}
	return hex.EncodeToString(sum.Sum(nil)), size, nil
}

// mimeFor returns the media type an HLS object is stored and served with. It is
// decided by name rather than sniffed, because a fragmented-MP4 segment's
// leading box is not something content sniffing recognises as video at all.
func mimeFor(name string) string {
	if name == hls.InitName {
		return initMIME
	}
	return segmentMIME
}

// renditionPrefix returns the key prefix holding one rendition's objects,
// hls/<file_hash>/<rendition>/.
func renditionPrefix(fileHash, rendition string) (string, error) {
	prefix, err := hls.PrefixFor(fileHash)
	if err != nil {
		return "", fmt.Errorf("hlsjob: %w", err)
	}
	if err := hls.ValidateRendition(rendition); err != nil {
		return "", fmt.Errorf("hlsjob: %w", err)
	}
	return prefix + rendition + "/", nil
}

// existingKeys returns the keys the store already holds under prefix — what an
// earlier encode of the same rendition published.
//
// It answers with nothing when the store cannot list a prefix, and that is not
// an error: the listing only ever makes the cleanup more precise. Without it a
// failed re-encode may remove objects the previous one had published (they are
// re-created by the retry) and a shorter re-encode may leave its predecessor's
// surplus segments behind (unreferenced by any playlist, and removed with the
// rest of the prefix when the photo is purged).
func (s *Service) existingKeys(ctx context.Context, prefix string) []string {
	lister, ok := s.objects.(storage.PrefixLister)
	if !ok {
		return nil
	}
	var keys []string
	if err := lister.KeysWithPrefix(ctx, prefix, func(key string) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		return nil
	}
	return keys
}

// remove deletes the given keys, best effort. A deletion that fails leaves an
// object nothing references, which is worth neither failing a successful encode
// over nor masking the error that is already being reported by a failing one.
func (s *Service) remove(ctx context.Context, keys []string) {
	for _, key := range keys {
		_ = s.objects.Delete(ctx, key)
	}
}

// without returns the keys of a that are not in b.
func without(a, b []string) []string {
	out := make([]string, 0, len(a))
	for _, key := range a {
		if !slices.Contains(b, key) {
			out = append(out, key)
		}
	}
	return out
}
