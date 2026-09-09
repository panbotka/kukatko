package hlsjob

import (
	"errors"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/panbotka/kukatko/internal/hls"
)

// ErrUnusablePlaylist indicates the media playlist ffmpeg left behind is not one
// this package can publish: no initialisation segment, no media segment, a
// segment whose duration is missing or unreadable, or a URI naming something the
// object layout cannot hold. Every one of them means the encode did not produce
// a rendition, so it is a failure of the run rather than something to store and
// discover later, when a player asks for a segment that was never uploaded.
var ErrUnusablePlaylist = errors.New("hlsjob: unusable media playlist")

// extinfPrefix introduces a segment's duration in a media playlist.
const extinfPrefix = "#EXTINF:"

// mapTagPrefix names the tag carrying the initialisation segment's URI.
const mapTagPrefix = "#EXT-X-MAP:"

// uriAttrPrefix is how the EXT-X-MAP tag introduces its quoted URI attribute.
const uriAttrPrefix = `URI="`

// millisPerSecond converts the playlist's fractional seconds to the whole
// milliseconds the catalogue stores.
const millisPerSecond = 1000

// encoded is what one run of ffmpeg left in its output directory, as read back
// from the media playlist it wrote: the names of the objects to publish and the
// two numbers the master playlist needs about them.
//
// It is deliberately derived from the playlist rather than from a directory
// listing. The playlist is ffmpeg's own statement of what it produced and in
// which order, so a stray file in the directory cannot become a segment, and a
// segment ffmpeg wrote but did not list — the tail of an interrupted run — is not
// published as if it were part of the rendition.
type encoded struct {
	// initName is the initialisation segment's object name (init.mp4).
	initName string
	// segments are the media segment object names, in playback order.
	segments []string
	// durationMs is the rendition's total length, summed from the EXTINF values.
	durationMs int
}

// parsePlaylist reads the media playlist ffmpeg wrote and reports the objects it
// names and how long they run to. Every URI is reduced to its base name and
// validated against the object layout, so a name that could never become a key —
// a traversal segment, a stray file, the playlist itself — fails the run here
// rather than at the store.
//
// It returns ErrUnusablePlaylist when the playlist names no initialisation
// segment, no media segment, or a segment whose EXTINF is missing or unreadable.
func parsePlaylist(playlist string) (encoded, error) {
	var out encoded
	seconds := 0.0
	pending := false
	for raw := range strings.SplitSeq(playlist, "\n") {
		line := strings.TrimRight(raw, "\r")
		switch {
		case strings.HasPrefix(line, mapTagPrefix):
			name, err := mapTagName(line)
			if err != nil {
				return encoded{}, err
			}
			out.initName = name
		case strings.HasPrefix(line, extinfPrefix):
			value, err := extinfSeconds(line)
			if err != nil {
				return encoded{}, err
			}
			seconds += value
			pending = true
		case line == "" || strings.HasPrefix(line, "#"):
			// A tag this package does not read, or a blank line.
		default:
			name, err := segmentName(line, pending)
			if err != nil {
				return encoded{}, err
			}
			out.segments = append(out.segments, name)
			pending = false
		}
	}
	out.durationMs = int(math.Round(seconds * millisPerSecond))
	return out, validate(out)
}

// validate reports whether the parsed playlist describes a publishable
// rendition: an initialisation segment, at least one media segment and a
// positive total duration.
func validate(out encoded) error {
	switch {
	case out.initName == "":
		return fmt.Errorf("%w: no EXT-X-MAP initialisation segment", ErrUnusablePlaylist)
	case len(out.segments) == 0:
		return fmt.Errorf("%w: no media segments", ErrUnusablePlaylist)
	case out.durationMs <= 0:
		return fmt.Errorf("%w: total duration is %d ms", ErrUnusablePlaylist, out.durationMs)
	default:
		return nil
	}
}

// mapTagName returns the object name the EXT-X-MAP tag's quoted URI attribute
// points at, or ErrUnusablePlaylist when the tag carries no usable URI.
func mapTagName(line string) (string, error) {
	attr := strings.Index(line, uriAttrPrefix)
	if attr < 0 {
		return "", fmt.Errorf("%w: EXT-X-MAP without a URI: %q", ErrUnusablePlaylist, line)
	}
	start := attr + len(uriAttrPrefix)
	end := strings.IndexByte(line[start:], '"')
	if end < 0 {
		return "", fmt.Errorf("%w: unterminated EXT-X-MAP URI: %q", ErrUnusablePlaylist, line)
	}
	name := path.Base(line[start : start+end])
	if err := hls.ValidateName(name); err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnusablePlaylist, err)
	}
	return name, nil
}

// extinfSeconds returns the duration an EXTINF tag declares, in seconds, or
// ErrUnusablePlaylist when it is not a number. The tag's optional title after
// the comma is ignored.
func extinfSeconds(line string) (float64, error) {
	value := strings.TrimPrefix(line, extinfPrefix)
	if comma := strings.IndexByte(value, ','); comma >= 0 {
		value = value[:comma]
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("%w: unreadable EXTINF: %q", ErrUnusablePlaylist, line)
	}
	return seconds, nil
}

// segmentName returns the object name a segment URI line names. preceded says
// whether an EXTINF was seen for it: a segment with no declared duration would
// make the total length a lie, so it is refused rather than counted as zero.
func segmentName(line string, preceded bool) (string, error) {
	if !preceded {
		return "", fmt.Errorf("%w: segment %q has no EXTINF", ErrUnusablePlaylist, line)
	}
	name := path.Base(strings.TrimSpace(line))
	if err := hls.ValidateName(name); err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnusablePlaylist, err)
	}
	return name, nil
}
