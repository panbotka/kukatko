package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	// metaSHA256 is the user-metadata key (sent as x-amz-meta-sha256) under which
	// every object written by this backend records the SHA256 digest of its
	// content. It is what lets Store recognise a byte-identical re-upload without
	// downloading the object: S3 ETags are MD5-based for single-part uploads and
	// opaque for multipart ones, so they cannot answer that question. An object
	// lacking the key — one written by some other tool — is treated as different
	// content and stored beside under a numeric suffix rather than overwritten.
	metaSHA256 = "sha256"
	// materializePrefix names the temporary files Materialize downloads into.
	materializePrefix = "materialize-*"
	// maxTempExtLen caps the extension copied from an object key onto a
	// materialized temp file. The extension matters — imgconvert dispatches RAW
	// and video on it — but an absurd one must not shape a filename.
	maxTempExtLen = 16
	// r2FilePerm is the mode reported for a materialized object. Objects carry no
	// permissions of their own; they are read-only as far as callers are concerned.
	r2FilePerm fs.FileMode = 0o444
)

// R2 backend errors, matchable with errors.Is.
var (
	// ErrR2NotConfigured indicates the R2 destination is incomplete, so no backend
	// can be built. The message names the missing keys, never their values.
	ErrR2NotConfigured = errors.New(
		"storage: R2 backend not configured (endpoint, bucket, access_key, secret_key and temp_path required)")
	// ErrInvalidEndpoint indicates the configured endpoint is not a usable host or
	// http(s) URL.
	ErrInvalidEndpoint = errors.New("storage: invalid R2 endpoint")
)

// R2Options configures the Cloudflare R2 backend. It mirrors the storage.r2.*
// config keys but is local to this package so the package stays decoupled from
// the config types. AccessKey, SecretKey and the signing secrets are credentials:
// they are never logged and never appear in a returned error.
type R2Options struct {
	// Endpoint is the S3 API host, optionally with an http:// or https:// scheme.
	// For R2 that is <accountid>.r2.cloudflarestorage.com. A bare host defaults to
	// TLS.
	Endpoint string
	// Region is the bucket region ("auto" for R2, often empty for MinIO).
	Region string
	// Bucket is the bucket holding originals and thumbnails.
	Bucket string
	// AccessKey and SecretKey are the S3 credentials.
	AccessKey string
	SecretKey string
	// MediaBaseURL is the domain of the edge Worker that serves the objects. When
	// empty the backend mints no URLs and the application serves media itself.
	MediaBaseURL string
	// URLSigningSecret signs media URLs; URLSigningSecretPrevious is additionally
	// accepted on verification so the secret can be rotated. Required whenever
	// MediaBaseURL is set.
	URLSigningSecret         string
	URLSigningSecretPrevious string
	// URLTTL is the lifetime of a signed URL. Non-positive means DefaultURLTTL.
	URLTTL time.Duration
	// TempPath is the local directory that staged uploads and materialized
	// downloads pass through. It must have room for the largest single file.
	TempPath string
}

// R2 is a Cloudflare R2 (S3-compatible) Storage, reached over minio-go. Objects
// are private: a client fetches one through the edge Worker with a signed URL,
// never straight from the bucket.
//
// Uploads stream through a staged temp file rather than straight into PutObject,
// because the object key depends on the content: an incoming file must be hashed
// before the store can tell a byte-identical re-upload (a deduplicated no-op)
// from a same-name-different-content file (which takes a numeric suffix, and must
// never overwrite the original already there). Nothing is buffered in memory, and
// the staged file is removed on every path.
//
// The hard-link publish that makes the FS backend crash-safe has no object-storage
// equivalent and needs none: PutObject is atomic, so no half-written object is
// ever visible at its key, and catalogue-wide deduplication is enforced by the
// unique constraint on photos.file_hash.
type R2 struct {
	client   *minio.Client
	bucket   string
	tempPath string
	// signer is nil when no media base URL is configured, in which case URL
	// returns the empty string and callers serve the bytes themselves.
	signer *URLSigner
}

// compile-time assertion that *R2 satisfies Storage.
var _ Storage = (*R2)(nil)

// NewR2 returns an R2 backend for the destination in opts, creating the temp
// directory if it does not exist. It returns ErrR2NotConfigured when a required
// field is empty, ErrInvalidEndpoint when the endpoint cannot be parsed, and
// ErrInvalidBaseURL or ErrMissingSigningSecret when the URL signing settings are
// inconsistent. No credential value appears in any of those errors.
func NewR2(opts R2Options) (*R2, error) {
	if opts.Endpoint == "" || opts.Bucket == "" || opts.AccessKey == "" ||
		opts.SecretKey == "" || opts.TempPath == "" {
		return nil, ErrR2NotConfigured
	}
	host, secure, err := parseR2Endpoint(opts.Endpoint)
	if err != nil {
		return nil, err
	}
	signer, err := newSignerFor(opts)
	if err != nil {
		return nil, err
	}
	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(opts.AccessKey, opts.SecretKey, ""),
		Secure: secure,
		Region: opts.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: initialising R2 client: %w", err)
	}
	if err := os.MkdirAll(opts.TempPath, dirPerm); err != nil {
		return nil, fmt.Errorf("storage: creating temp directory %s: %w", opts.TempPath, err)
	}
	return &R2{client: client, bucket: opts.Bucket, tempPath: opts.TempPath, signer: signer}, nil
}

// newSignerFor builds the URL signer for opts, or nil when no media base URL is
// configured. A base URL without a signing secret is an error rather than an
// unsigned URL, which the edge Worker would reject anyway.
func newSignerFor(opts R2Options) (*URLSigner, error) {
	if opts.MediaBaseURL == "" {
		return nil, nil //nolint:nilnil // "no signer configured" is the zero case, not an error.
	}
	signer, err := NewURLSigner(
		opts.MediaBaseURL, opts.URLSigningSecret, opts.URLSigningSecretPrevious, opts.URLTTL)
	if err != nil {
		return nil, err
	}
	return signer, nil
}

// parseR2Endpoint splits an endpoint into the bare host[:port] minio-go expects
// and a TLS flag. A scheme-qualified endpoint takes its TLS setting from the
// scheme; a bare host defaults to TLS, matching R2's own https endpoint. It
// returns ErrInvalidEndpoint for an unparseable or non-HTTP scheme.
func parseR2Endpoint(endpoint string) (host string, secure bool, err error) {
	if !strings.Contains(endpoint, "://") {
		return endpoint, true, nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", false, fmt.Errorf("%w: %w", ErrInvalidEndpoint, err)
	}
	switch parsed.Scheme {
	case schemeHTTPS:
		secure = true
	case schemeHTTP:
		secure = false
	default:
		return "", false, fmt.Errorf("%w: scheme %q must be http or https", ErrInvalidEndpoint, parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", false, fmt.Errorf("%w: missing host", ErrInvalidEndpoint)
	}
	return parsed.Host, secure, nil
}

// Store streams src into the bucket under YYYY/MM/<originalName> and returns the
// resulting StoredFile. See the Storage interface for the full contract,
// including the ErrAlreadyExists duplicate signal.
func (r *R2) Store(
	ctx context.Context, src io.Reader, takenAt time.Time, originalName string,
) (StoredFile, error) {
	tmp, err := streamToTemp(ctx, r.tempPath, src)
	if err != nil {
		return StoredFile{}, err
	}
	defer func() { _ = os.Remove(tmp.Path) }()

	name := sanitizeName(originalName, tmp.Hash)
	key, existed, err := r.resolveKey(ctx, relDirFor(takenAt), name, tmp.Hash)
	if err != nil {
		return StoredFile{}, err
	}

	stored := StoredFile{Hash: tmp.Hash, RelPath: key, Size: tmp.Size, MIME: detectMIME(tmp.Header, name)}
	if existed {
		return stored, ErrAlreadyExists
	}
	if err := r.upload(ctx, key, tmp, stored.MIME); err != nil {
		return StoredFile{}, err
	}
	return stored, nil
}

// resolveKey picks the object key for a file of the given hash landing in relDir
// under name: the first candidate that is either free (existed=false) or already
// holds byte-identical content (existed=true), walking numeric suffixes past any
// occupant whose content differs.
func (r *R2) resolveKey(ctx context.Context, relDir, name, hash string) (string, bool, error) {
	for attempt := range maxCollisionAttempts {
		candidate := path.Join(relDir, suffixName(name, attempt))
		info, err := r.client.StatObject(ctx, r.bucket, candidate, minio.StatObjectOptions{})
		if err != nil {
			if isNotFound(err) {
				return candidate, false, nil
			}
			return "", false, fmt.Errorf("storage: stat %s: %w", candidate, err)
		}
		if objectHash(info) == hash {
			return candidate, true, nil
		}
	}
	return "", false, fmt.Errorf("%w: %s", ErrTooManyCollisions, name)
}

// upload puts the staged temp file at key with its content type and SHA256
// recorded as user metadata. The known size lets minio-go pick its part sizes;
// the body streams from disk and is never held in memory.
func (r *R2) upload(ctx context.Context, key string, tmp tempFile, contentType string) error {
	file, err := os.Open(tmp.Path)
	if err != nil {
		return fmt.Errorf("storage: reopening staged upload: %w", err)
	}
	defer func() { _ = file.Close() }()

	_, err = r.client.PutObject(ctx, r.bucket, key, file, tmp.Size, minio.PutObjectOptions{
		ContentType:  contentType,
		UserMetadata: map[string]string{metaSHA256: tmp.Hash},
	})
	if err != nil {
		return fmt.Errorf("storage: putting %s: %w", key, err)
	}
	return nil
}

// Open opens the object at relPath for reading. The GET is issued eagerly so a
// missing object fails here, wrapping os.ErrNotExist, rather than on first read.
func (r *R2) Open(ctx context.Context, relPath string) (io.ReadCloser, error) {
	key, err := objectKey(relPath)
	if err != nil {
		return nil, err
	}
	obj, err := r.client.GetObject(ctx, r.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, objectError("opening", relPath, err)
	}
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, objectError("opening", relPath, err)
	}
	return obj, nil
}

// Stat returns file information for the object at relPath. The returned FileInfo
// carries the object's size and last-modified time; a missing object yields an
// error wrapping os.ErrNotExist.
func (r *R2) Stat(ctx context.Context, relPath string) (os.FileInfo, error) {
	key, err := objectKey(relPath)
	if err != nil {
		return nil, err
	}
	info, err := r.client.StatObject(ctx, r.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, objectError("stat", relPath, err)
	}
	return objectInfo{name: path.Base(key), size: info.Size, modTime: info.LastModified}, nil
}

// Delete removes the object at relPath. S3 deletes are idempotent and report no
// error for an absent key, so the object is stat'ed first to honour Storage's
// contract that deleting a missing file wraps os.ErrNotExist.
func (r *R2) Delete(ctx context.Context, relPath string) error {
	key, err := objectKey(relPath)
	if err != nil {
		return err
	}
	if _, err := r.client.StatObject(ctx, r.bucket, key, minio.StatObjectOptions{}); err != nil {
		return objectError("deleting", relPath, err)
	}
	if err := r.client.RemoveObject(ctx, r.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: deleting %s: %w", relPath, err)
	}
	return nil
}

// URL returns the signed, short-lived URL at which the edge Worker serves the
// object at relPath, or the empty string when no media base URL is configured (in
// which case the application serves the bytes itself). See Storage.URL.
func (r *R2) URL(relPath string) string {
	if r.signer == nil {
		return ""
	}
	key, err := objectKey(relPath)
	if err != nil {
		return ""
	}
	return r.signer.SignedURL(key)
}

// Materialize downloads the object at relPath to a temporary file under the
// configured temp path and returns its path together with a cleanup that removes
// it. The temp file keeps the object's extension, which imgconvert needs to
// dispatch RAW and video. The download is streamed, and a failure part-way
// through removes the partial file before returning. See Storage.Materialize.
func (r *R2) Materialize(ctx context.Context, relPath string) (string, func(), error) {
	key, err := objectKey(relPath)
	if err != nil {
		return "", noopCleanup, err
	}
	file, err := os.CreateTemp(r.tempPath, materializePattern(key))
	if err != nil {
		return "", noopCleanup, fmt.Errorf("storage: creating temp file: %w", err)
	}
	cleanup := removeOnce(file.Name())
	if err := r.download(ctx, key, file); err != nil {
		_ = file.Close()
		cleanup()
		return "", noopCleanup, objectError("materializing", relPath, err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", noopCleanup, fmt.Errorf("storage: closing materialized %s: %w", relPath, err)
	}
	return file.Name(), cleanup, nil
}

// download streams the object at key into dst. It returns the raw minio error so
// the caller can map a missing object onto os.ErrNotExist.
func (r *R2) download(ctx context.Context, key string, dst io.Writer) error {
	obj, err := r.client.GetObject(ctx, r.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return err //nolint:wrapcheck // objectError wraps it with the operation and path.
	}
	defer func() { _ = obj.Close() }()

	if _, err := io.Copy(dst, obj); err != nil {
		return err //nolint:wrapcheck // objectError wraps it with the operation and path.
	}
	return nil
}

// objectKey confines relPath to a canonical, slash-separated object key with no
// leading slash and no "../" escape, rejecting the empty result with
// ErrInvalidPath. The key is the photos.file_path value verbatim.
func objectKey(relPath string) (string, error) {
	key := confine(relPath)
	if key == "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidPath, relPath)
	}
	return key, nil
}

// objectHash returns the SHA256 digest this backend recorded on the object, or
// the empty string when the object carries none. minio-go strips the x-amz-meta-
// prefix and canonicalises the header case, so the lookup is case-insensitive.
func objectHash(info minio.ObjectInfo) string {
	for name, value := range info.UserMetadata {
		if strings.EqualFold(name, metaSHA256) {
			return value
		}
	}
	return ""
}

// objectError wraps err as a failure of op on relPath, translating a missing
// object into an error wrapping os.ErrNotExist so callers can branch on it with
// errors.Is exactly as they do for the filesystem backend.
func objectError(op, relPath string, err error) error {
	if isNotFound(err) {
		return fmt.Errorf("storage: %s %s: %w", op, relPath, os.ErrNotExist)
	}
	return fmt.Errorf("storage: %s %s: %w", op, relPath, err)
}

// isNotFound reports whether err is an S3 "object does not exist" response (HTTP
// 404, or a NoSuchKey code).
func isNotFound(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey"
}

// materializePattern returns the os.CreateTemp pattern for an object key,
// preserving the key's extension so extension-dispatching tools see the format
// they expect. An extension that is absurdly long or would corrupt the pattern
// is dropped.
func materializePattern(key string) string {
	ext := path.Ext(key)
	if len(ext) > maxTempExtLen || strings.ContainsAny(ext, `*/\`) {
		return materializePrefix
	}
	return materializePrefix + ext
}

// objectInfo adapts an S3 object's metadata to os.FileInfo, which is what the
// Storage interface promises. Objects have no permissions and are never
// directories, so Mode is a constant read-only file mode.
type objectInfo struct {
	name    string
	size    int64
	modTime time.Time
}

// compile-time assertion that objectInfo satisfies os.FileInfo.
var _ os.FileInfo = objectInfo{}

// Name returns the last element of the object key.
func (o objectInfo) Name() string { return o.name }

// Size returns the object size in bytes.
func (o objectInfo) Size() int64 { return o.size }

// Mode returns a constant read-only regular-file mode.
func (o objectInfo) Mode() fs.FileMode { return r2FilePerm }

// ModTime returns the object's last-modified time.
func (o objectInfo) ModTime() time.Time { return o.modTime }

// IsDir reports false: an object is never a directory.
func (o objectInfo) IsDir() bool { return false }

// Sys returns nil; there is no underlying data source to expose.
func (o objectInfo) Sys() any { return nil }
