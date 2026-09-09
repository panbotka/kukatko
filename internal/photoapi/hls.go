package photoapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/photos"
)

// playlistContentType is the media type both playlists are served as. It is the
// registered type for HLS; the older `application/x-mpegURL` spelling is what
// some servers still emit, but every player understands this one and Safari
// prefers it.
const playlistContentType = "application/vnd.apple.mpegurl"

// playlistCacheControl keeps a playlist out of every cache. The bytes are cheap
// to rebuild, and — unlike the segments they point at — they are not immutable
// content: the URIs in them carry the caller's download token when one was used
// to fetch the playlist, so a shared cache holding them would hand one session's
// token to the next reader.
const playlistCacheControl = "private, no-store"

// segmentCacheControl caches a directly-streamed segment for a long time: its
// bytes are a fragment of an encode keyed by the clip's content hash and are
// never rewritten in place. It stays private to the authenticated caller.
const segmentCacheControl = "private, max-age=31536000, immutable"

const (
	// initContentType is the media type of the fragmented-MP4 initialisation
	// segment, and segmentContentType that of a CMAF media segment — the same two
	// the encoder publishes them under (see internal/hlsjob).
	initContentType    = "video/mp4"
	segmentContentType = "video/iso.segment"
)

// downloadTokenParam is the query parameter the download guard reads a session's
// media token from (auth's own constant, which is unexported). The playlists
// repeat it onto every URI they emit, because a player fetching a media playlist
// or a segment issues a plain GET with whatever the URI says and nothing else: a
// `<video>` tag pointed at a token-carrying master would otherwise walk into a
// 401 on its first segment.
const downloadTokenParam = "t"

// HLSRenditions reads what the streaming encode recorded for a photo. It is
// satisfied by *hlsjob.Store. A nil HLSRenditions makes all three HLS routes
// answer 404 and the photo payload report no streaming, which is what an
// instance with the feature switched off looks like to a player.
type HLSRenditions interface {
	// ListForPhoto returns every rendition recorded for the photo, widest picture
	// first — the order a master playlist advertises them in.
	ListForPhoto(ctx context.Context, photoUID string) ([]hlsjob.Encoded, error)
	// Get returns one recorded rendition, or hlsjob.ErrRenditionNotFound.
	Get(ctx context.Context, photoUID, rendition string) (hlsjob.Encoded, error)
	// Has reports whether the photo has been encoded into the named rendition,
	// reading none of its columns.
	Has(ctx context.Context, photoUID, rendition string) (bool, error)
	// HasAny reports whether the photo has any rendition at all.
	HasAny(ctx context.Context, photoUID string) (bool, error)
}

// handleHLSMaster serves the master playlist of a photo's streaming renditions:
// what qualities exist, what each of them demands and where its media playlist
// is fetched from. A photo with no rendition — a still, a video the encode has
// not reached, an instance with streaming off — is answered with 404, which the
// player reads as "this clip is not streamable" and falls back to the plain
// video endpoint.
//
// The variant URIs are relative, so they resolve against this playlist's own
// address whatever base path the API is mounted under, and they point at this
// application rather than at the object store. That is the whole design: the
// backend authorises and signs each object as it is asked for, so no signature
// can expire mid-playback and no long-lived token is ever written into a file a
// player keeps.
func (a *API) handleHLSMaster(w http.ResponseWriter, r *http.Request) {
	if a.hls == nil {
		writeError(w, http.StatusNotFound, "no streaming renditions for this photo")
		return
	}
	uid := chi.URLParam(r, "uid")
	list, err := a.hls.ListForPhoto(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading streaming renditions failed")
		return
	}
	if len(list) == 0 {
		writeError(w, http.StatusNotFound, "no streaming renditions for this photo")
		return
	}
	playlist, err := hls.BuildMaster(masterVariants(list, mediaPlaylistURI(r)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "building master playlist failed")
		return
	}
	writePlaylist(w, playlist)
}

// masterVariants projects the recorded renditions onto the master playlist's
// variants, asking uri where each rendition's media playlist is served from.
func masterVariants(list []hlsjob.Encoded, uri func(rendition string) string) []hls.Variant {
	variants := make([]hls.Variant, 0, len(list))
	for _, enc := range list {
		variants = append(variants, hls.Variant{
			URL:       uri(enc.Rendition),
			Bandwidth: enc.Bandwidth,
			Width:     enc.Width,
			Height:    enc.Height,
			Codecs:    enc.Codecs,
		})
	}
	return variants
}

// handleHLSMedia serves one rendition's media playlist: the segments, their
// measured durations and where each of them is fetched from. The text is the one
// ffmpeg wrote, with only its URIs rewritten (hls.RewriteMedia) — the durations
// are never synthesised, because a playlist that disagreed with the bytes a
// player receives is worse than none.
//
// An unknown rendition name, and a well-formed one this photo was never encoded
// into, are both 404: neither exists, and telling them apart would only describe
// the encoder's plan to a caller who cannot use the answer.
func (a *API) handleHLSMedia(w http.ResponseWriter, r *http.Request) {
	rendition, ok := a.hlsRendition(w, r)
	if !ok {
		return
	}
	enc, err := a.hls.Get(r.Context(), chi.URLParam(r, "uid"), rendition)
	if err != nil {
		if errors.Is(err, hlsjob.ErrRenditionNotFound) {
			writeError(w, http.StatusNotFound, "no such streaming rendition")
			return
		}
		writeError(w, http.StatusInternalServerError, "reading streaming rendition failed")
		return
	}
	segment := segmentURI(r)
	playlist, err := hls.RewriteMedia(enc.Playlist, func(name string) (string, error) {
		return segment(name), nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "building media playlist failed")
		return
	}
	writePlaylist(w, playlist)
}

// handleHLSSegment answers for one object of a rendition — the initialisation
// segment or one media fragment — with a redirect to a freshly signed object URL,
// or with the bytes themselves when the storage backend publishes no URLs.
//
// The signature is minted per request and the redirect is marked no-store, so it
// is never reused: a cached redirect would outlive its signature and eventually
// send a player to a 403 mid-clip. This is the same treatment the video endpoint
// gives an original, for the same reason.
//
// The segment name is validated against the object layout before it is allowed
// anywhere near a key, so a traversal or a stray file name is a 404 here rather
// than a request against the store.
func (a *API) handleHLSSegment(w http.ResponseWriter, r *http.Request) {
	rendition, ok := a.hlsRendition(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "segment")
	if err := hls.ValidateName(name); err != nil {
		writeError(w, http.StatusNotFound, "no such segment")
		return
	}
	photo, err := a.store.GetByUID(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writePhotoError(w, err, "fetching photo failed")
		return
	}
	has, err := a.hls.Has(r.Context(), photo.UID, rendition)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading streaming rendition failed")
		return
	}
	key, keyErr := hls.Key(photo.FileHash, rendition, name)
	if !has || keyErr != nil {
		writeError(w, http.StatusNotFound, "no such segment")
		return
	}
	if signed := a.media.Object(key); signed != "" {
		redirectToMedia(w, r, signed)
		return
	}
	a.streamSegment(w, r, photo, key, name)
}

// streamSegment serves a segment's bytes from the store, for the backend that
// publishes no URLs (the filesystem one). A segment gone from storage is a 404:
// the row promised objects that are not there, and there is nothing to serve.
func (a *API) streamSegment(
	w http.ResponseWriter, r *http.Request, photo photos.Photo, key, name string,
) {
	reader, err := a.storage.Open(r.Context(), key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "no such segment")
			return
		}
		writeError(w, http.StatusInternalServerError, "opening segment failed")
		return
	}
	defer func() { _ = reader.Close() }()

	w.Header().Set("Content-Type", segmentMIME(name))
	w.Header().Set("Cache-Control", segmentCacheControl)
	streamMedia(w, r, reader, strconv.Quote(photo.FileHash+"-"+key), 0)
}

// hlsRendition reads and validates the rendition name in the request path,
// writing the 404 itself when streaming is not wired or the name is not one the
// object layout could ever hold. The bool reports whether the caller may go on.
func (a *API) hlsRendition(w http.ResponseWriter, r *http.Request) (string, bool) {
	if a.hls == nil {
		writeError(w, http.StatusNotFound, "no streaming renditions for this photo")
		return "", false
	}
	rendition := chi.URLParam(r, "rendition")
	if err := hls.ValidateRendition(rendition); err != nil {
		writeError(w, http.StatusNotFound, "no such streaming rendition")
		return "", false
	}
	return rendition, true
}

// segmentMIME returns the media type of one HLS object by its name: the
// initialisation segment is a plain fragmented MP4, everything else a CMAF media
// segment.
func segmentMIME(name string) string {
	if name == hls.InitName {
		return initContentType
	}
	return segmentContentType
}

// writePlaylist writes an .m3u8 body with the HLS media type and the no-store
// directive both playlists are served under.
func writePlaylist(w http.ResponseWriter, playlist string) {
	w.Header().Set("Content-Type", playlistContentType)
	w.Header().Set("Cache-Control", playlistCacheControl)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(playlist)); err != nil {
		// The status line is already sent; nothing to do but log.
		log.Printf("photoapi: writing playlist: %v", err)
	}
}

// mediaPlaylistURI returns the relative URI a rendition's media playlist is
// served from, as the master playlist must spell it: `<rendition>/index.m3u8`,
// carrying the request's download token when it had one.
func mediaPlaylistURI(r *http.Request) func(rendition string) string {
	suffix := tokenQuery(r)
	return func(rendition string) string {
		return url.PathEscape(rendition) + "/" + hls.EncodePlaylistName + suffix
	}
}

// segmentURI returns the relative URI one object of a rendition is served from,
// as its media playlist must spell it: the bare object name, carrying the
// request's download token when it had one. The name has already been validated
// by hls.RewriteMedia, which is the only caller.
func segmentURI(r *http.Request) func(name string) string {
	suffix := tokenQuery(r)
	return func(name string) string {
		return url.PathEscape(name) + suffix
	}
}

// tokenQuery returns the query string repeating this request's download token
// onto a playlist URI, or "" when the caller authenticated with a session cookie
// (which the browser sends along by itself).
func tokenQuery(r *http.Request) string {
	token := r.URL.Query().Get(downloadTokenParam)
	if token == "" {
		return ""
	}
	return "?" + downloadTokenParam + "=" + url.QueryEscape(token)
}

// resolveHLS reports whether the photo can be streamed — it has at least one
// recorded rendition — for the flag on the detail payload, so a player knows
// before it asks. A lookup failure reads as "no streaming": the photo is worth
// showing either way, and the player still has the plain video endpoint.
func (a *API) resolveHLS(ctx context.Context, photoUID string) bool {
	if a.hls == nil {
		return false
	}
	has, err := a.hls.HasAny(ctx, photoUID)
	return err == nil && has
}
