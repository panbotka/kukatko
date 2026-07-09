package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Sentinel errors for the S3 store.
var (
	// ErrNotConfigured indicates the S3 destination is incomplete (endpoint or
	// bucket missing), so no store can be built.
	ErrNotConfigured = errors.New("backup: S3 destination not configured (endpoint and bucket required)")
	// ErrInvalidEndpoint indicates the configured endpoint is not a usable host or
	// HTTP(S) URL.
	ErrInvalidEndpoint = errors.New("backup: invalid S3 endpoint")
)

// S3Options configures an S3-compatible backup destination. It mirrors the
// backup.s3.* config keys but is local to this package so the package stays
// decoupled from the config types.
type S3Options struct {
	// Endpoint is the S3 host, optionally with an http:// or https:// scheme. A
	// bare host defaults to TLS.
	Endpoint string
	// Region is the bucket region (may be empty for MinIO).
	Region string
	// Bucket is the destination bucket name.
	Bucket string
	// AccessKey and SecretKey are the S3 credentials, kept off any log line.
	AccessKey string
	SecretKey string
	// PathStyle selects path-style addressing (bucket in the path, required by
	// most MinIO setups) instead of virtual-hosted style.
	PathStyle bool
}

// s3Store is an ObjectStore backed by an S3-compatible service via minio-go. All
// uploads stream from the supplied reader; the secret key lives only inside the
// minio client and never appears in an error or log line.
type s3Store struct {
	client *minio.Client
	bucket string
}

// compile-time assertion that s3Store satisfies ObjectStore.
var _ ObjectStore = (*s3Store)(nil)

// NewS3Store builds an ObjectStore for the S3 destination in opts. It returns
// ErrNotConfigured when the endpoint or bucket is missing and ErrInvalidEndpoint
// when the endpoint cannot be parsed. Path-style addressing and a streamed,
// non-buffering upload path are used so it works with AWS, MinIO, Backblaze and
// Wasabi alike.
func NewS3Store(opts S3Options) (*s3Store, error) {
	if opts.Endpoint == "" || opts.Bucket == "" {
		return nil, ErrNotConfigured
	}
	host, secure, err := parseEndpoint(opts.Endpoint)
	if err != nil {
		return nil, err
	}
	clientOpts := &minio.Options{
		Creds:  credentials.NewStaticV4(opts.AccessKey, opts.SecretKey, ""),
		Secure: secure,
		Region: opts.Region,
	}
	if opts.PathStyle {
		clientOpts.BucketLookup = minio.BucketLookupPath
	}
	client, err := minio.New(host, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("backup: initialising S3 client: %w", err)
	}
	return &s3Store{client: client, bucket: opts.Bucket}, nil
}

// parseEndpoint splits an endpoint into the bare host[:port] minio-go expects and
// a TLS flag. A scheme-qualified endpoint takes its TLS setting from the scheme;
// a bare host defaults to TLS (secure=true), matching AWS. It returns
// ErrInvalidEndpoint for an unparseable or non-HTTP scheme.
func parseEndpoint(endpoint string) (host string, secure bool, err error) {
	if !strings.Contains(endpoint, "://") {
		return endpoint, true, nil
	}
	parsed, parseErr := url.Parse(endpoint)
	if parseErr != nil {
		return "", false, fmt.Errorf("%w: %w", ErrInvalidEndpoint, parseErr)
	}
	switch parsed.Scheme {
	case "https":
		secure = true
	case "http":
		secure = false
	default:
		return "", false, fmt.Errorf("%w: scheme %q must be http or https", ErrInvalidEndpoint, parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", false, fmt.Errorf("%w: missing host", ErrInvalidEndpoint)
	}
	return parsed.Host, secure, nil
}

// Stat returns the object at key, mapping a not-found response to ok=false with
// a nil error so the caller can treat it as "absent" during the incremental sync.
func (s *s3Store) Stat(ctx context.Context, key string) (Object, bool, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return Object{}, false, nil
		}
		return Object{}, false, fmt.Errorf("backup: stat %s: %w", key, err)
	}
	return Object{Key: key, Size: info.Size, ETag: info.ETag}, true, nil
}

// Put streams size bytes from reader to key. A negative size streams the body via
// multipart upload without buffering it whole, which is how database dumps (of
// unknown length) are uploaded.
func (s *s3Store) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("backup: put %s: %w", key, err)
	}
	return nil
}

// CopyFrom copies srcKey out of srcBucket into key, server-side: the request is
// issued against this store's endpoint with srcBucket named as the copy source,
// so the service moves the bytes itself and none of them cross this process.
// ComposeObject rather than CopyObject is used because a plain copy is capped at
// 5 GiB, and a single video can exceed that; with one source and no metadata
// rewrite ComposeObject degrades to exactly that plain copy, and only reaches for
// a multipart server-side copy when the object is too large for one.
//
// The credentials this store was built with must be able to read srcBucket, which
// in practice means both buckets are served by the same S3 service (or that the
// backup account has been granted read on the primary bucket).
func (s *s3Store) CopyFrom(ctx context.Context, srcBucket, srcKey, key string) error {
	dst := minio.CopyDestOptions{Bucket: s.bucket, Object: key}
	src := minio.CopySrcOptions{Bucket: srcBucket, Object: srcKey}
	if _, err := s.client.ComposeObject(ctx, dst, src); err != nil {
		return fmt.Errorf("backup: copy %s/%s to %s: %w", srcBucket, srcKey, key, err)
	}
	return nil
}

// Open opens the object at key for streaming reads. The returned *minio.Object
// is lazy: the first Read performs the GET, and a missing object surfaces as a
// read error rather than here. The caller must close it.
func (s *s3Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("backup: open %s: %w", key, err)
	}
	return obj, nil
}

// List returns every object whose key begins with prefix, reading the recursive
// listing channel to completion.
func (s *s3Store) List(ctx context.Context, prefix string) ([]Object, error) {
	var objects []Object
	for info := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if info.Err != nil {
			return nil, fmt.Errorf("backup: list %q: %w", prefix, info.Err)
		}
		objects = append(objects, Object{Key: info.Key, Size: info.Size, ETag: info.ETag})
	}
	return objects, nil
}

// Remove deletes the object at key, treating a not-found response as success so
// pruning is idempotent.
func (s *s3Store) Remove(ctx context.Context, key string) error {
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("backup: remove %s: %w", key, err)
	}
	return nil
}

// isNotFound reports whether err is an S3 "object does not exist" response (HTTP
// 404 or a NoSuchKey code).
func isNotFound(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey"
}
