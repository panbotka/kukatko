package hls

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// Errors returned by the playlist functions.
var (
	// ErrNoSegmentURL indicates RewriteMedia was called without a URL resolver.
	// Rewriting is the whole point of the call, so a nil resolver is a wiring
	// mistake rather than a request to pass the playlist through.
	ErrNoSegmentURL = errors.New("hls: no segment URL resolver")
	// ErrMalformedPlaylist indicates a line this package must understand — an
	// EXT-X-MAP tag — that it cannot: no quoted URI attribute where the format
	// requires one.
	ErrMalformedPlaylist = errors.New("hls: malformed playlist")
	// ErrNoVariants indicates a master playlist was asked for with nothing to put
	// in it. A master with no variant is not a degenerate playlist, it is one no
	// player can play.
	ErrNoVariants = errors.New("hls: master playlist without variants")
	// ErrInvalidVariant indicates a variant missing something the master playlist
	// must advertise: its URL, bandwidth, resolution or codecs.
	ErrInvalidVariant = errors.New("hls: invalid variant")
)

// mapTagPrefix starts the tag naming a rendition's initialisation segment. It is
// the only tag whose value this package rewrites.
const mapTagPrefix = "#EXT-X-MAP:"

// uriAttrPrefix is how the EXT-X-MAP tag introduces its quoted URI attribute.
const uriAttrPrefix = `URI="`

// masterVersion is the EXT-X-VERSION a master playlist declares. Seven is what
// fragmented-MP4 segments require, and a master must not claim less than the
// media playlists it points at.
const masterVersion = 7

// SegmentURL resolves one object name inside a rendition's prefix — InitName or a
// five-digit media segment — to the URL a player should fetch it from. It returns
// an error when no URL can be produced, e.g. because signing failed; the error
// aborts the rewrite rather than yielding a playlist with a hole in it.
type SegmentURL func(name string) (string, error)

// Variant is one entry of a master playlist: a rendition as it is advertised to a
// player, pointing at the URL its media playlist is served from. Bandwidth is in
// bits per second and is the peak (see Rendition.Bandwidth), Width and Height are
// the encoded picture's real dimensions — not the rendition's box, which a
// portrait or an already-small source never fills — and Codecs is the RFC 6381
// string from the rendition.
type Variant struct {
	// URL is where this variant's media playlist is served from. It is supplied by
	// the caller because only the caller knows whether the bytes come from a signed
	// object URL or from one of Kukátko's own routes.
	URL string
	// Bandwidth is the peak bandwidth of the variant in bits per second.
	Bandwidth int
	// Width is the encoded picture width in pixels.
	Width int
	// Height is the encoded picture height in pixels.
	Height int
	// Codecs is the RFC 6381 codecs string of the variant's streams.
	Codecs string
}

// RewriteMedia returns the media playlist ffmpeg wrote with every segment URI and
// the EXT-X-MAP URI replaced by the URL that url resolves the object's name to.
//
//	served, err := hls.RewriteMedia(written, func(name string) (string, error) {
//	    return store.SignedURL(ctx, key(name))
//	})
//
// Everything else is passed through byte for byte, line endings included: the
// durations, the target duration, the version, the playlist type, the end marker
// and any tag this package has never heard of. That is deliberate — ffmpeg has
// already measured what it wrote, and a playlist whose EXTINF values were
// synthesised from what the catalogue believes would drift from the segments a
// player actually receives. The only thing changed is where the bytes live.
//
// The name handed to url is the URI's base name, which ValidateName must accept:
// how ffmpeg spells the path in front of it depends on flags this package does not
// want the URL builder to know about, while the object it names is always one in
// the rendition's own prefix. A URI naming something that is not an object of this
// layout is an error, not a URL request.
//
// It returns ErrNoSegmentURL for a nil resolver, ErrMalformedPlaylist for an
// EXT-X-MAP without a quoted URI, ErrInvalidName for an unexpected URI, and
// whatever url returned, wrapped.
func RewriteMedia(playlist string, url SegmentURL) (string, error) {
	if url == nil {
		return "", ErrNoSegmentURL
	}
	var out strings.Builder
	out.Grow(len(playlist))
	for _, chunk := range strings.SplitAfter(playlist, "\n") {
		if chunk == "" {
			// The empty tail SplitAfter yields when the playlist ends in a newline.
			continue
		}
		line, ending := splitLineEnding(chunk)
		rewritten, err := rewriteLine(line, url)
		if err != nil {
			return "", err
		}
		out.WriteString(rewritten)
		out.WriteString(ending)
	}
	return out.String(), nil
}

// BuildMaster returns the master playlist advertising the given variants, in the
// order they are given: a player picks the first variant it can play, so the
// caller's order is the preference order. It returns ErrNoVariants for an empty
// list and ErrInvalidVariant for one that is missing anything a player needs to
// choose it without fetching it first.
func BuildMaster(variants []Variant) (string, error) {
	if len(variants) == 0 {
		return "", ErrNoVariants
	}
	var out strings.Builder
	out.WriteString("#EXTM3U\n")
	fmt.Fprintf(&out, "#EXT-X-VERSION:%d\n", masterVersion)
	out.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")
	for _, variant := range variants {
		if err := variant.validate(); err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,CODECS=%q\n",
			variant.Bandwidth, variant.Width, variant.Height, variant.Codecs)
		out.WriteString(variant.URL)
		out.WriteString("\n")
	}
	return out.String(), nil
}

// validate reports whether the variant carries everything the master playlist
// must advertise, returning ErrInvalidVariant naming the first missing part.
func (v Variant) validate() error {
	switch {
	case v.URL == "":
		return fmt.Errorf("%w: no URL", ErrInvalidVariant)
	case v.Bandwidth <= 0:
		return fmt.Errorf("%w: bandwidth %d", ErrInvalidVariant, v.Bandwidth)
	case v.Width <= 0 || v.Height <= 0:
		return fmt.Errorf("%w: resolution %dx%d", ErrInvalidVariant, v.Width, v.Height)
	case v.Codecs == "":
		return fmt.Errorf("%w: no codecs", ErrInvalidVariant)
	default:
		return nil
	}
}

// rewriteLine returns the line as it should be served: an EXT-X-MAP with its URI
// attribute resolved, a segment URI replaced by its URL, and anything else — a
// blank line or any other tag — returned unchanged.
func rewriteLine(line string, url SegmentURL) (string, error) {
	switch {
	case strings.HasPrefix(line, mapTagPrefix):
		return rewriteMapTag(line, url)
	case line == "" || strings.HasPrefix(line, "#"):
		return line, nil
	default:
		return resolveURI(line, url)
	}
}

// rewriteMapTag returns the EXT-X-MAP tag with the value of its URI attribute
// replaced by the resolved URL, every other byte of the tag — a BYTERANGE
// attribute, the attribute order, the spacing — left as it was found. It returns
// ErrMalformedPlaylist when the tag carries no quoted URI.
func rewriteMapTag(line string, url SegmentURL) (string, error) {
	attr := strings.Index(line, uriAttrPrefix)
	if attr < 0 {
		return "", fmt.Errorf("%w: EXT-X-MAP without a URI: %q", ErrMalformedPlaylist, line)
	}
	start := attr + len(uriAttrPrefix)
	end := strings.IndexByte(line[start:], '"')
	if end < 0 {
		return "", fmt.Errorf("%w: unterminated EXT-X-MAP URI: %q", ErrMalformedPlaylist, line)
	}
	resolved, err := resolveURI(line[start:start+end], url)
	if err != nil {
		return "", err
	}
	return line[:start] + resolved + line[start+end:], nil
}

// resolveURI returns the URL for the object the playlist URI names. The URI is
// reduced to its base name and validated against the layout before url is asked
// for anything, so a resolver is only ever handed a name ValidateName accepts.
func resolveURI(uri string, url SegmentURL) (string, error) {
	name := path.Base(uri)
	if err := ValidateName(name); err != nil {
		return "", err
	}
	resolved, err := url(name)
	if err != nil {
		return "", fmt.Errorf("hls: URL for %q: %w", name, err)
	}
	return resolved, nil
}

// splitLineEnding splits one chunk of a playlist into its content and its line
// ending, so a rewritten line can be written back with the ending it arrived
// with — CRLF stays CRLF, and a playlist whose last line has no newline does not
// grow one.
func splitLineEnding(chunk string) (line, ending string) {
	if !strings.HasSuffix(chunk, "\n") {
		return chunk, ""
	}
	body := chunk[:len(chunk)-1]
	if strings.HasSuffix(body, "\r") {
		return body[:len(body)-1], "\r\n"
	}
	return body, "\n"
}
