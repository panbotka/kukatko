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

// detectMIME determines the media type of a file from its leading bytes, using
// the filename only as a hint. Content sniffing wins whenever it is conclusive;
// when http.DetectContentType falls back to the generic octet-stream type, the
// extension is consulted (first the curated media table, then the system mime
// database) before the generic type is returned.
func detectMIME(header []byte, name string) string {
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
