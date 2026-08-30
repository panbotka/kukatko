package ctl

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestSaveRendition_thumbnail verifies a thumbnail is fetched from the size route
// and written where the caller asked, with the bytes intact.
func TestSaveRendition_thumbnail(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("jpeg-bytes"))
	})

	dest := filepath.Join(t.TempDir(), "shot.jpg")
	saved, err := client.SaveRendition(t.Context(), "pht01", DefaultRenditionSize, dest)
	if err != nil {
		t.Fatalf("SaveRendition returned %v", err)
	}
	if gotPath != "/api/v1/photos/pht01/thumb/fit_720" {
		t.Errorf("path = %q, want the thumbnail route", gotPath)
	}
	if saved.Path != dest || saved.Bytes != int64(len("jpeg-bytes")) || saved.MediaType != "image/jpeg" {
		t.Errorf("saved = %+v, want the destination and the whole body", saved)
	}
	content, err := os.ReadFile(dest)
	if err != nil || string(content) != "jpeg-bytes" {
		t.Errorf("file = %q, %v; want the streamed bytes", content, err)
	}
}

// TestSaveRendition_originalUsesTheDownloadRoute verifies "original" is the whole
// stored file, fetched from the download route rather than the thumbnail one.
func TestSaveRendition_originalUsesTheDownloadRoute(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("original-bytes"))
	})

	dest := filepath.Join(t.TempDir(), "orig.jpg")
	saved, err := client.SaveRendition(t.Context(), "pht01", RenditionOriginal, dest)
	if err != nil {
		t.Fatalf("SaveRendition returned %v", err)
	}
	if gotPath != "/api/v1/photos/pht01/download" {
		t.Errorf("path = %q, want the download route", gotPath)
	}
	if saved.Bytes != int64(len("original-bytes")) {
		t.Errorf("wrote %d bytes, want the whole body", saved.Bytes)
	}
}

// TestSaveRendition_defaultPath verifies that with no destination the file lands
// in the working directory under the name the response implies. It cannot run in
// parallel: it changes the working directory, which is process-wide.
func TestSaveRendition_defaultPath(t *testing.T) {
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Disposition", `attachment; filename="babicka.jpg"`)
		w.Write([]byte("original-bytes"))
	})

	dir := t.TempDir()
	t.Chdir(dir)
	saved, err := client.SaveRendition(t.Context(), "pht01", RenditionOriginal, "")
	if err != nil {
		t.Fatalf("SaveRendition returned %v", err)
	}
	if saved.Path != "babicka.jpg" {
		t.Errorf("path = %q, want the name from Content-Disposition", saved.Path)
	}
	if _, err := os.Stat(filepath.Join(dir, "babicka.jpg")); err != nil {
		t.Errorf("the file was not written: %v", err)
	}
}

// TestRenditionName verifies how a default file name is chosen: the download
// route names the original in its Content-Disposition and that name is used, a
// thumbnail (or a redirect that dropped the header) falls back to the uid and the
// size, and a header naming a path is reduced to its base so a server cannot
// steer the write out of the working directory.
func TestRenditionName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		disposition string
		size        string
		mediaType   string
		want        string
	}{
		{"the original keeps its own name", `attachment; filename="babicka.jpg"`,
			RenditionOriginal, "image/jpeg", "babicka.jpg"},
		{"a thumbnail is named after the size", "", "fit_1920", "image/jpeg", "pht01_fit_1920.jpg"},
		{"a video keeps its container", "", RenditionOriginal, "video/mp4", "pht01_original.mp4"},
		{"an unknown type falls back", "", RenditionOriginal, "application/x-nonsense", "pht01_original.bin"},
		{"a path in the header is reduced to its base", `attachment; filename="../../etc/passwd"`,
			RenditionOriginal, "image/jpeg", "passwd"},
		{"a bare traversal is refused outright", `attachment; filename=".."`,
			RenditionOriginal, "image/jpeg", "pht01_original.jpg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp := &http.Response{Header: http.Header{}}
			if tc.disposition != "" {
				resp.Header.Set("Content-Disposition", tc.disposition)
			}
			if got := renditionName(resp, "pht01", tc.size, tc.mediaType); got != tc.want {
				t.Errorf("renditionName = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSaveRendition_invalidSize verifies an unknown size is refused before a
// request is made — the server would answer 404 and the operator would have to
// guess why.
func TestSaveRendition_invalidSize(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted with an unknown size")
	})
	_, err := client.SaveRendition(t.Context(), "pht01", "huge", "")
	if !errors.Is(err, ErrInvalidRendition) {
		t.Errorf("error = %v, want ErrInvalidRendition", err)
	}
	if _, err := client.SaveRendition(t.Context(), "", "fit_720", ""); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("blank uid error = %v, want ErrEmptyUID", err)
	}
}

// TestSaveRendition_notFoundLeavesNoFile verifies an error response is reported as
// the CLI's typed error and that nothing is left on disk that looks like a photo.
func TestSaveRendition_notFoundLeavesNoFile(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"photo not found"}`))
	})

	dir := t.TempDir()
	dest := filepath.Join(dir, "shot.jpg")
	_, err := client.SaveRendition(t.Context(), "pht01", "fit_720", dest)
	var status *StatusError
	if !errors.As(err, &status) || status.Status != http.StatusNotFound {
		t.Fatalf("error = %v, want a 404 StatusError", err)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("reading the destination directory: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("directory holds %d entries, want nothing left behind", len(entries))
	}
}

// TestSaveRendition_unauthorized verifies the media routes report a bad token
// with the same actionable message as every other command.
func TestSaveRendition_unauthorized(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	_, err := client.SaveRendition(t.Context(), "pht01", "fit_720", filepath.Join(t.TempDir(), "a.jpg"))
	var unauthorized *UnauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Errorf("error = %v, want an UnauthorizedError", err)
	}
}

// TestRenditionSizes verifies every registered thumbnail size plus the original
// is offered, and that nothing else is accepted.
func TestRenditionSizes(t *testing.T) {
	t.Parallel()

	sizes := RenditionSizes()
	if len(sizes) < 2 || sizes[len(sizes)-1] != RenditionOriginal {
		t.Fatalf("sizes = %v, want the thumbnails plus the original last", sizes)
	}
	for _, size := range sizes {
		if !ValidRendition(size) {
			t.Errorf("offered size %q is not accepted", size)
		}
	}
	if ValidRendition("fit_99999") {
		t.Error("an unregistered size was accepted")
	}
}
