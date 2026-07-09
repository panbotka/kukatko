package storage

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

// validR2Options returns options that build a client successfully. No network
// call is made by NewR2, so a fictional endpoint is fine.
func validR2Options(t *testing.T) R2Options {
	t.Helper()
	return R2Options{
		Endpoint:         "https://account.r2.cloudflarestorage.com",
		Region:           "auto",
		Bucket:           "kukatko",
		AccessKey:        "access",
		SecretKey:        "secret",
		MediaBaseURL:     testBaseURL,
		URLSigningSecret: testSecret,
		URLTTL:           time.Hour,
		TempPath:         t.TempDir(),
	}
}

func TestNewR2RequiresConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		blank func(*R2Options)
	}{
		{name: "endpoint", blank: func(o *R2Options) { o.Endpoint = "" }},
		{name: "bucket", blank: func(o *R2Options) { o.Bucket = "" }},
		{name: "access key", blank: func(o *R2Options) { o.AccessKey = "" }},
		{name: "secret key", blank: func(o *R2Options) { o.SecretKey = "" }},
		{name: "temp path", blank: func(o *R2Options) { o.TempPath = "" }},
	}
	for _, tt := range tests {
		t.Run("missing "+tt.name, func(t *testing.T) {
			t.Parallel()
			opts := validR2Options(t)
			tt.blank(&opts)
			if _, err := NewR2(opts); !errors.Is(err, ErrR2NotConfigured) {
				t.Errorf("NewR2(missing %s) = %v, want ErrR2NotConfigured", tt.name, err)
			}
		})
	}
}

func TestNewR2RejectsBadEndpoint(t *testing.T) {
	t.Parallel()
	opts := validR2Options(t)
	opts.Endpoint = "ftp://nope"
	if _, err := NewR2(opts); !errors.Is(err, ErrInvalidEndpoint) {
		t.Errorf("NewR2(ftp endpoint) = %v, want ErrInvalidEndpoint", err)
	}
}

func TestNewR2RejectsMediaBaseURLWithoutSecret(t *testing.T) {
	t.Parallel()
	opts := validR2Options(t)
	opts.URLSigningSecret = ""
	if _, err := NewR2(opts); !errors.Is(err, ErrMissingSigningSecret) {
		t.Errorf("NewR2(no signing secret) = %v, want ErrMissingSigningSecret", err)
	}
}

func TestNewR2ErrorsHideCredentials(t *testing.T) {
	t.Parallel()
	opts := validR2Options(t)
	opts.Endpoint = "ftp://nope"
	_, err := NewR2(opts)
	if err == nil {
		t.Fatal("NewR2(ftp endpoint) = nil error, want failure")
	}
	for _, secret := range []string{opts.SecretKey, opts.AccessKey, opts.URLSigningSecret} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("NewR2 error leaks a credential: %v", err)
		}
	}
}

func TestR2URLSignsObjectKey(t *testing.T) {
	t.Parallel()
	store, err := NewR2(validR2Options(t))
	if err != nil {
		t.Fatalf("NewR2: %v", err)
	}
	// A leading slash is not part of the object key, so it must not change the URL.
	if got, want := store.URL("/"+testKey), store.URL(testKey); got != want {
		t.Errorf("URL(%q) = %q, want %q", "/"+testKey, got, want)
	}
	parsed, err := url.Parse(store.URL(testKey))
	if err != nil {
		t.Fatalf("parsing URL: %v", err)
	}
	if got, want := parsed.Path, "/"+testKey; got != want {
		t.Errorf("URL path = %q, want %q", got, want)
	}
	if parsed.Query().Get(QuerySignature) == "" {
		t.Error("URL carries no signature")
	}
}

func TestR2URLWithoutMediaBaseURL(t *testing.T) {
	t.Parallel()
	opts := validR2Options(t)
	opts.MediaBaseURL = ""
	opts.URLSigningSecret = ""
	store, err := NewR2(opts)
	if err != nil {
		t.Fatalf("NewR2: %v", err)
	}
	if got := store.URL(testKey); got != "" {
		t.Errorf("URL without media_base_url = %q, want empty (application serves the bytes)", got)
	}
}

func TestR2URLRejectsInvalidPath(t *testing.T) {
	t.Parallel()
	store, err := NewR2(validR2Options(t))
	if err != nil {
		t.Fatalf("NewR2: %v", err)
	}
	for _, relPath := range []string{"", "/", "..", "../.."} {
		if got := store.URL(relPath); got != "" {
			t.Errorf("URL(%q) = %q, want empty for an unusable path", relPath, got)
		}
	}
}

func TestParseR2Endpoint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		endpoint   string
		wantHost   string
		wantSecure bool
		wantErr    bool
	}{
		{
			name:     "bare host defaults to TLS",
			endpoint: "account.r2.cloudflarestorage.com",
			wantHost: "account.r2.cloudflarestorage.com", wantSecure: true,
		},
		{
			name:     "https scheme",
			endpoint: "https://account.r2.cloudflarestorage.com",
			wantHost: "account.r2.cloudflarestorage.com", wantSecure: true,
		},
		{name: "http scheme", endpoint: "http://localhost:19100", wantHost: "localhost:19100"},
		{name: "bad scheme", endpoint: "ftp://nope", wantErr: true},
		{name: "missing host", endpoint: "https://", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			host, secure, err := parseR2Endpoint(tt.endpoint)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidEndpoint) {
					t.Fatalf("parseR2Endpoint(%q) error = %v, want ErrInvalidEndpoint", tt.endpoint, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseR2Endpoint(%q) error = %v", tt.endpoint, err)
			}
			if host != tt.wantHost || secure != tt.wantSecure {
				t.Errorf("parseR2Endpoint(%q) = (%q, %t), want (%q, %t)",
					tt.endpoint, host, secure, tt.wantHost, tt.wantSecure)
			}
		})
	}
}

func TestObjectKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		relPath string
		want    string
		wantErr bool
	}{
		{relPath: "2024/05/IMG_0001.jpg", want: "2024/05/IMG_0001.jpg"},
		{relPath: "/2024/05/IMG_0001.jpg", want: "2024/05/IMG_0001.jpg"},
		{relPath: "2024/05/Šťastné Vánoce.jpg", want: "2024/05/Šťastné Vánoce.jpg"},
		{relPath: "../../etc/passwd", want: "etc/passwd"},
		{relPath: "", wantErr: true},
		{relPath: "/", wantErr: true},
		{relPath: "../..", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.relPath, func(t *testing.T) {
			t.Parallel()
			got, err := objectKey(tt.relPath)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidPath) {
					t.Fatalf("objectKey(%q) error = %v, want ErrInvalidPath", tt.relPath, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("objectKey(%q) error = %v", tt.relPath, err)
			}
			if got != tt.want {
				t.Errorf("objectKey(%q) = %q, want %q", tt.relPath, got, tt.want)
			}
		})
	}
}

func TestObjectHash(t *testing.T) {
	t.Parallel()
	const digest = "b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c"
	tests := []struct {
		name string
		meta map[string]string
		want string
	}{
		// minio-go strips the x-amz-meta- prefix and canonicalises the header case.
		{name: "canonical case", meta: map[string]string{"Sha256": digest}, want: digest},
		{name: "lowercase", meta: map[string]string{"sha256": digest}, want: digest},
		{name: "other metadata only", meta: map[string]string{"Origin": "import"}, want: ""},
		{name: "no metadata", meta: nil, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := objectHash(minio.ObjectInfo{UserMetadata: tt.meta}); got != tt.want {
				t.Errorf("objectHash(%v) = %q, want %q", tt.meta, got, tt.want)
			}
		})
	}
}

func TestMaterializePattern(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  string
		want string
	}{
		{key: "2024/05/IMG_0001.jpg", want: "materialize-*.jpg"},
		{key: "2024/05/IMG_0001.CR2", want: "materialize-*.CR2"},
		{key: "2024/05/clip.mp4", want: "materialize-*.mp4"},
		{key: "2024/05/noext", want: "materialize-*"},
		{key: "2024/05/name.thisextensionismuchtoolong", want: "materialize-*"},
		{key: "2024/05/name.we*rd", want: "materialize-*"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			if got := materializePattern(tt.key); got != tt.want {
				t.Errorf("materializePattern(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestObjectInfoAdaptsToFileInfo(t *testing.T) {
	t.Parallel()
	modTime := time.Date(2024, time.May, 1, 10, 0, 0, 0, time.UTC)
	info := objectInfo{name: "IMG_0001.jpg", size: 4096, modTime: modTime}

	if info.Name() != "IMG_0001.jpg" || info.Size() != 4096 || !info.ModTime().Equal(modTime) {
		t.Errorf("objectInfo = (%q, %d, %s), want (IMG_0001.jpg, 4096, %s)",
			info.Name(), info.Size(), info.ModTime(), modTime)
	}
	if info.IsDir() {
		t.Error("objectInfo.IsDir() = true, want false")
	}
	if info.Mode() != r2FilePerm {
		t.Errorf("objectInfo.Mode() = %s, want %s", info.Mode(), r2FilePerm)
	}
	if info.Sys() != nil {
		t.Errorf("objectInfo.Sys() = %v, want nil", info.Sys())
	}
}

func TestIsNotFound(t *testing.T) {
	t.Parallel()
	if isNotFound(errors.New("connection refused")) {
		t.Error("isNotFound(network error) = true, want false")
	}
	notFound := minio.ErrorResponse{StatusCode: 404, Code: "NoSuchKey"}
	if !isNotFound(notFound) {
		t.Error("isNotFound(NoSuchKey) = false, want true")
	}
}
