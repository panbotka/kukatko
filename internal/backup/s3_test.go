package backup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseEndpoint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		endpoint   string
		wantHost   string
		wantSecure bool
		wantErr    bool
	}{
		{name: "bare host defaults to TLS", endpoint: "s3.amazonaws.com", wantHost: "s3.amazonaws.com", wantSecure: true},
		{name: "https scheme", endpoint: "https://s3.eu.example.com", wantHost: "s3.eu.example.com", wantSecure: true},
		{name: "http scheme", endpoint: "http://localhost:9000", wantHost: "localhost:9000", wantSecure: false},
		{name: "host with port", endpoint: "minio:9000", wantHost: "minio:9000", wantSecure: true},
		{name: "bad scheme", endpoint: "ftp://nope", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			host, secure, err := parseEndpoint(tt.endpoint)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidEndpoint) {
					t.Fatalf("parseEndpoint(%q) error = %v, want ErrInvalidEndpoint", tt.endpoint, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEndpoint(%q) error = %v", tt.endpoint, err)
			}
			if host != tt.wantHost || secure != tt.wantSecure {
				t.Errorf("parseEndpoint(%q) = (%q, %v), want (%q, %v)",
					tt.endpoint, host, secure, tt.wantHost, tt.wantSecure)
			}
		})
	}
}

func TestNewS3Store_notConfigured(t *testing.T) {
	t.Parallel()
	tests := []S3Options{
		{Endpoint: "", Bucket: "b"},
		{Endpoint: "s3", Bucket: ""},
	}
	for _, opts := range tests {
		if _, err := NewS3Store(opts); !errors.Is(err, ErrNotConfigured) {
			t.Errorf("NewS3Store(%+v) error = %v, want ErrNotConfigured", opts, err)
		}
	}
}

func TestNewS3Store_pathStyle(t *testing.T) {
	t.Parallel()
	store, err := NewS3Store(S3Options{
		Endpoint:  "http://localhost:9000",
		Bucket:    "kukatko",
		AccessKey: "key",
		SecretKey: "secret",
		PathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewS3Store() error = %v", err)
	}
	if store.bucket != "kukatko" {
		t.Errorf("bucket = %q, want kukatko", store.bucket)
	}
}

// newTestStore points an s3Store at the given test server URL using path-style
// addressing, with a fixed region so the client makes no bucket-location call.
func newTestStore(t *testing.T, serverURL string) *s3Store {
	t.Helper()
	store, err := NewS3Store(S3Options{
		Endpoint:  serverURL,
		Region:    "us-east-1",
		Bucket:    "backups",
		AccessKey: "key",
		SecretKey: "secret",
		PathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewS3Store() error = %v", err)
	}
	return store
}

func TestS3Store_Stat(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodHead {
			t.Errorf("Stat used method %s, want HEAD", r.Method)
		}
		if strings.HasSuffix(r.URL.Path, "/missing.jpg") {
			http.Error(w, "", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", "42")
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	store := newTestStore(t, srv.URL)

	obj, ok, err := store.Stat(context.Background(), "2026/01/a.jpg")
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !ok || obj.Size != 42 {
		t.Errorf("Stat() = (%+v, %v), want present with size 42", obj, ok)
	}
	// Path-style addressing must place the bucket in the path.
	if !strings.HasPrefix(gotPath, "/backups/") {
		t.Errorf("request path = %q, want path-style /backups/...", gotPath)
	}

	_, ok, err = store.Stat(context.Background(), "missing.jpg")
	if err != nil {
		t.Fatalf("Stat() missing error = %v, want nil", err)
	}
	if ok {
		t.Error("Stat() ok = true for a missing object")
	}
}

func TestS3Store_List(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") != "2" {
			t.Errorf("List did not use ListObjectsV2: query=%s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>backups</Name>
  <Prefix>db/</Prefix>
  <KeyCount>2</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
  <Contents><Key>db/kukatko-20260101T000000Z.dump</Key><Size>10</Size><ETag>"e1"</ETag><LastModified>2026-01-01T00:00:00.000Z</LastModified></Contents>
  <Contents><Key>db/kukatko-20260102T000000Z.dump</Key><Size>20</Size><ETag>"e2"</ETag><LastModified>2026-01-02T00:00:00.000Z</LastModified></Contents>
</ListBucketResult>`)
	}))
	defer srv.Close()
	store := newTestStore(t, srv.URL)

	objects, err := store.List(context.Background(), "db/")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(objects) != 2 {
		t.Fatalf("List() returned %d objects, want 2", len(objects))
	}
	if objects[0].Key != "db/kukatko-20260101T000000Z.dump" || objects[0].Size != 10 {
		t.Errorf("objects[0] = %+v, want first dump size 10", objects[0])
	}
}

func TestS3Store_Remove(t *testing.T) {
	t.Parallel()
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	store := newTestStore(t, srv.URL)

	if err := store.Remove(context.Background(), "db/old.dump"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("Remove used method %s, want DELETE", gotMethod)
	}
	if gotPath != "/backups/db/old.dump" {
		t.Errorf("Remove path = %q, want /backups/db/old.dump", gotPath)
	}
}
