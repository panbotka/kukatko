package photoapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/video"
)

// videoCacheControl caches a directly-streamed video for a long time: the bytes
// are immutable per content hash. It is private to the authenticated caller.
const videoCacheControl = "private, max-age=31536000"

// videoSource identifies the on-disk file to stream for a video and the MIME
// type to advertise for it.
type videoSource struct {
	relPath string
	mime    string
}

// handleVideo streams a photo's video for inline HTML5 playback. The response
// supports HTTP range requests (seeking without downloading the whole clip) via
// http.ServeContent, which is also memory-bounded — it copies in fixed-size
// chunks from a seekable file rather than buffering it. A live photo streams its
// motion clip. When on-the-fly transcoding is enabled and the codec is not
// browser-friendly the clip is transcoded to H.264/MP4 instead (no range
// support). A photo with no playable video is answered with 404.
func (a *API) handleVideo(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	photo, err := a.store.GetByUID(r.Context(), uid)
	if err != nil {
		writePhotoError(w, err, "fetching photo failed")
		return
	}
	src, ok, err := a.videoFile(r.Context(), photo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolving video file failed")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "photo has no playable video")
		return
	}
	if a.shouldTranscode(photo) {
		a.streamTranscoded(w, r, photo, src)
		return
	}
	a.streamVideoFile(w, r, photo, src)
}

// shouldTranscode reports whether the photo's video should be transcoded on the
// fly: only when transcoding is enabled, the codec is positively known and not
// browser-friendly, and ffmpeg is actually installed to do it. An unknown
// (empty) codec is served as-is — we do not burn CPU transcoding a video we
// cannot prove the browser will reject.
func (a *API) shouldTranscode(photo photos.Photo) bool {
	return a.videoTranscode &&
		photo.VideoCodec != "" &&
		!video.IsWebFriendlyCodec(photo.VideoCodec) &&
		video.FFmpegAvailable()
}

// videoFile resolves the file to stream for the photo: the original for a
// standalone video, or the motion clip sidecar for a live photo. It returns
// ok=false when the photo is a still image (no video to play).
func (a *API) videoFile(ctx context.Context, photo photos.Photo) (videoSource, bool, error) {
	switch photo.MediaType {
	case photos.MediaVideo:
		return videoSource{relPath: photo.FilePath, mime: photo.FileMime}, true, nil
	case photos.MediaLive:
		files, err := a.store.ListFiles(ctx, photo.UID)
		if err != nil {
			return videoSource{}, false, fmt.Errorf("photoapi: listing photo files: %w", err)
		}
		clip, ok := pickMotionClip(files)
		if !ok {
			return videoSource{}, false, nil
		}
		return videoSource{relPath: clip.FilePath, mime: clip.FileMime}, true, nil
	default:
		return videoSource{}, false, nil
	}
}

// pickMotionClip returns the motion clip among a photo's files: the first file
// whose MIME or extension identifies it as a video. It is a standalone function
// so the selection can be unit-tested without a database.
func pickMotionClip(files []photos.PhotoFile) (photos.PhotoFile, bool) {
	for _, f := range files {
		if strings.HasPrefix(f.FileMime, "video/") || video.IsVideoPath(f.FilePath) {
			return f, true
		}
	}
	return photos.PhotoFile{}, false
}

// streamVideoFile serves the stored video file with HTTP range support via
// http.ServeContent, so the browser can seek and resume. The ETag (content hash)
// and modtime let ServeContent answer conditional and If-Range requests. A file
// gone from storage is answered with 404, any other open error with 500.
func (a *API) streamVideoFile(w http.ResponseWriter, r *http.Request, photo photos.Photo, src videoSource) {
	absPath := a.storage.AbsPath(src.relPath)
	file, err := os.Open(absPath) //nolint:gosec // path is confined by storage.AbsPath.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "video file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "opening video failed")
		return
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading video failed")
		return
	}
	if src.mime != "" {
		w.Header().Set("Content-Type", src.mime)
	}
	w.Header().Set("Cache-Control", videoCacheControl)
	w.Header().Set("ETag", strconv.Quote(photo.FileHash))
	// ServeContent honours Range/If-Range/If-None-Match/If-Modified-Since, emits
	// 206 with Content-Range for a partial fetch, advertises Accept-Ranges, and
	// streams the body in bounded chunks from the seekable file.
	http.ServeContent(w, r, path.Base(src.relPath), info.ModTime(), file)
}

// streamTranscoded transcodes the video to H.264/MP4 on the fly and streams the
// result for playback. The transcoded stream cannot be seeked (no range
// support) and is never cached. If ffmpeg fails to start, it falls back to
// streaming the original file so playback still works when the browser can
// decode it.
func (a *API) streamTranscoded(w http.ResponseWriter, r *http.Request, photo photos.Photo, src videoSource) {
	absPath := a.storage.AbsPath(src.relPath)
	stream, err := video.Transcode(r.Context(), absPath)
	if err != nil {
		log.Printf("photoapi: transcode start for %s: %v; serving original", photo.UID, err)
		a.streamVideoFile(w, r, photo, src)
		return
	}
	defer func() { _ = stream.Close() }()

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	if _, err := io.Copy(w, stream); err != nil {
		// The status line is already sent; nothing to do but log.
		log.Printf("photoapi: streaming transcoded video %s: %v", photo.UID, err)
	}
}
