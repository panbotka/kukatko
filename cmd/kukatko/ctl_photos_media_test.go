package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCtlPhotosImage verifies a rendition is saved where --output-file asks and
// that the command prints the path, which is what an agent feeds to its next step.
func TestCtlPhotosImage(t *testing.T) {
	var gotPath string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("jpeg-bytes"))
	})

	dest := filepath.Join(t.TempDir(), "shot.jpg")
	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "image", "pht01",
		"--output-file", dest)
	if err != nil {
		t.Fatalf("photos image returned %v", err)
	}
	if gotPath != "/api/v1/photos/pht01/thumb/fit_720" {
		t.Errorf("path = %q, want the default fit_720 thumbnail", gotPath)
	}
	if strings.TrimSpace(out) != dest {
		t.Errorf("output = %q, want the saved path", out)
	}
	content, err := os.ReadFile(dest)
	if err != nil || string(content) != "jpeg-bytes" {
		t.Errorf("file = %q, %v; want the streamed bytes", content, err)
	}
}

// TestCtlPhotosImage_original verifies --size original reaches the download route,
// so an agent can ask for the file itself and not only a preview.
func TestCtlPhotosImage_original(t *testing.T) {
	var gotPath string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("x"))
	})

	dest := filepath.Join(t.TempDir(), "orig.jpg")
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "image", "pht01",
		"--size", "original", "--output-file", dest); err != nil {
		t.Fatalf("photos image --size original returned %v", err)
	}
	if gotPath != "/api/v1/photos/pht01/download" {
		t.Errorf("path = %q, want the download route", gotPath)
	}
}

// TestCtlPhotosGet_people verifies "read the photo whole" means what it says: the
// roll-call is asked for by default and the table names who is on the photo,
// alongside the text the recogniser read in it.
func TestCtlPhotosGet_people(t *testing.T) {
	var gotQuery string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"uid":"pht01","title":"Lake","ocr_text":"ZAVŘENO","people":[
			{"subject_uid":"su1","subject_name":"Alice","marker_uid":"mk1","det_score":0.9},
			{"det_score":0.7}]}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "get", "pht01")
	if err != nil {
		t.Fatalf("photos get returned %v", err)
	}
	if gotQuery != "people=true" {
		t.Errorf("query = %q, want people=true by default", gotQuery)
	}
	for _, want := range []string{"Alice", "1 unassigned", "ZAVŘENO"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
}

// TestCtlPhotosGet_peopleOff verifies --people=false leaves the parameter off, so
// a caller that only wants the metadata never makes the server do the match.
func TestCtlPhotosGet_peopleOff(t *testing.T) {
	var gotQuery string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"uid":"pht01","title":"Lake"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "get", "pht01",
		"--people=false")
	if err != nil {
		t.Fatalf("photos get --people=false returned %v", err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want no parameters at all", gotQuery)
	}
	if !strings.Contains(out, "not reported") {
		t.Errorf("output claims something about the people it never asked for:\n%s", out)
	}
}

// TestCtlPhotosImage_json verifies the two machine formats confirm a saved
// rendition with its size and type, which a pipeline would otherwise have to stat
// for, while the table stays the bare path.
func TestCtlPhotosImage_json(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("jpeg-bytes"))
	})

	dest := filepath.Join(t.TempDir(), "shot.jpg")
	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "json",
		"photos", "image", "pht01", "--output-file", dest)
	if err != nil {
		t.Fatalf("photos image -o json returned %v", err)
	}
	var saved struct {
		Path      string `json:"path"`
		Bytes     int64  `json:"bytes"`
		MediaType string `json:"media_type"`
	}
	if err := json.Unmarshal([]byte(out), &saved); err != nil {
		t.Fatalf("json output %q does not parse: %v", out, err)
	}
	if saved.Path != dest || saved.Bytes != int64(len("jpeg-bytes")) || saved.MediaType != "image/jpeg" {
		t.Errorf("saved = %+v, want the path, the size and the type", saved)
	}
}

// TestCtlPhotos_llmOutput verifies -o llm is available on an ordinary command and
// that --fields narrows it, reaching through the list envelope.
func TestCtlPhotos_llmOutput(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(listBody))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "llm",
		"--fields", "uid,title", "photos", "list")
	if err != nil {
		t.Fatalf("photos list -o llm returned %v", err)
	}
	trimmed := strings.TrimSpace(out)
	if !strings.Contains(trimmed, `"uid":"pht01"`) || !strings.Contains(trimmed, `"title":"Lake"`) {
		t.Errorf("llm output does not carry the named fields:\n%s", out)
	}
	for _, unwanted := range []string{"file_name", "total", "is_favorite"} {
		if strings.Contains(trimmed, unwanted) {
			t.Errorf("llm output still carries %q:\n%s", unwanted, out)
		}
	}
}
