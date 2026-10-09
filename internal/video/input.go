package video

import "strings"

// DemuxerAllowlist is the comma-separated list of libavformat demuxers an
// ffprobe/ffmpeg run over a user's file may pick. It covers exactly the
// containers behind the extensions Kukátko ingests (see videoExts): mov
// (mp4/m4v/mov/3gp), matroska (mkv/webm), avi, asf (wmv), flv, mpeg (the
// program stream of .mpg) and mpegvideo (a raw MPEG-1/2 elementary stream),
// mpegts (.mts/.m2ts) and the raw h264/hevc elementary streams. A demuxer's
// name matches when any of its own comma-separated aliases is listed, so "mov"
// covers "mov,mp4,m4a,3gp,3g2,mj2" and "matroska" covers "matroska,webm".
//
// Its whole point is what it leaves out: the manifest demuxers — dash, hls,
// concat and friends — fetch or open every URL their input names. Without the
// list, a DASH manifest uploaded as clip.mp4 made libavformat send a GET to
// whatever host its BaseURL named, during admission and again on every poster,
// encode and re-probe (SEC-017 in docs/SECURITY_AUDIT.md).
const DemuxerAllowlist = "mov,matroska,avi,asf,flv,mpeg,mpegvideo,mpegts,h264,hevc"

// localProtocols is the protocol allowlist for an input on the local disk:
// the file itself and nothing it could point onward to.
const localProtocols = "file"

// remoteProtocols is the protocol allowlist for an input that is a signed
// object-store URL (the transcode and the encode read a remote original straight
// from it): plain HTTP for a development MinIO, HTTPS for R2.
const remoteProtocols = "http,https,tls,tcp"

// InputArgs returns the input options every ffprobe/ffmpeg run over a user's
// media places before its input src: the demuxer allowlist and a protocol
// allowlist chosen by what src is — "file" alone for a local path, the HTTP(S)
// stack for an http(s) URL. Both options apply to the input that follows them.
//
//	args := append([]string{"-nostdin"}, video.InputArgs(src)...)
//	args = append(args, "-i", src, …)
func InputArgs(src string) []string {
	return []string{
		"-format_whitelist", DemuxerAllowlist,
		"-protocol_whitelist", inputProtocols(src),
	}
}

// inputProtocols picks the protocol allowlist for src: the HTTP(S) stack for an
// http(s) URL, the local file protocol for anything else.
func inputProtocols(src string) string {
	lower := strings.ToLower(src)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return remoteProtocols
	}
	return localProtocols
}
