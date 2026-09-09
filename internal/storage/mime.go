package storage

import (
	"mime"
	"net/http"
	"path"
	"strings"
)

// octetStream is the generic media type http.DetectContentType returns when it
// cannot recognise the content from its leading bytes.
const octetStream = "application/octet-stream"

// mediaTypeByExt maps lowercase file extensions (with leading dot) to media
// types that http.DetectContentType does not recognise — chiefly camera RAW
// formats, HEIF/HEIC, and container video formats. It is the extension-based
// fallback consulted only when content sniffing is inconclusive.
var mediaTypeByExt = map[string]string{
	".heic": "image/heic",
	".heif": "image/heif",
	".avif": "image/avif",
	".dng":  "image/x-adobe-dng",
	".cr2":  "image/x-canon-cr2",
	".cr3":  "image/x-canon-cr3",
	".nef":  "image/x-nikon-nef",
	".arw":  "image/x-sony-arw",
	".raf":  "image/x-fuji-raf",
	".orf":  "image/x-olympus-orf",
	".rw2":  "image/x-panasonic-rw2",
	".mov":  "video/quicktime",
	".mp4":  "video/mp4",
	".m4v":  "video/x-m4v",
	".avi":  "video/x-msvideo",
	".mkv":  "video/x-matroska",
	".webm": "video/webm",
	".3gp":  "video/3gpp",
}

// mediaTypeByExtFirst maps lowercase file extensions whose media type must be
// decided by the extension alone, before any content is looked at. It holds the
// streaming formats, where sniffing is not merely inconclusive but actively
// wrong: an HLS playlist is a UTF-8 text file, which http.DetectContentType
// happily and confidently calls text/plain — a type no player will load a
// playlist from — and a fragmented-MP4 segment begins with a styp/moof box that
// sniffing does not recognise as video at all.
//
// Nothing else belongs here. An extension that is only unknown to sniffing goes
// in mediaTypeByExt, which stays the fallback, so a mislabelled file's real
// content still wins wherever its content can be recognised.
var mediaTypeByExtFirst = map[string]string{
	".m3u8": "application/vnd.apple.mpegurl",
	".m4s":  "video/iso.segment",
}

// detectMIME determines the media type of a file from its leading bytes, using
// the filename only as a hint. The extensions in mediaTypeByExtFirst are decided
// by name before anything is sniffed; otherwise content sniffing wins whenever it
// is conclusive, and when http.DetectContentType falls back to the generic
// octet-stream type, the extension is consulted (first the curated media table,
// then the system mime database) before the generic type is returned.
func detectMIME(header []byte, name string) string {
	if mediaType, ok := mediaTypeByExtFirst[strings.ToLower(path.Ext(name))]; ok {
		return mediaType
	}
	contentType := http.DetectContentType(header)
	if contentType != octetStream {
		return contentType
	}
	return mimeByExtension(name)
}

// mimeByExtension resolves a media type from name's extension, preferring the
// curated mediaTypeByExt table and falling back to the system mime database; it
// returns the generic octet-stream type when the extension is unknown.
func mimeByExtension(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ext == "" {
		return octetStream
	}
	if mediaType, ok := mediaTypeByExt[ext]; ok {
		return mediaType
	}
	if byExt := mime.TypeByExtension(ext); byExt != "" {
		return byExt
	}
	return octetStream
}
