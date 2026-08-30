package ctl

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// decodeEdit marshals an edit and decodes it back into raw JSON values, so a test
// can assert on presence and on an explicit null — the distinction the whole
// command turns on, and one that a struct of pointers would erase.
func decodeEdit(t *testing.T, edit *PhotoEdit) map[string]json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(edit)
	if err != nil {
		t.Fatalf("marshalling the edit returned %v", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("edit body %q does not parse: %v", encoded, err)
	}
	return body
}

// TestPhotoEdit_omittedVersusCleared verifies the three states a field can be in:
// absent from the body, present with a value, and present as an explicit null.
//
// This is the command's whole contract. An untouched taken_at must not appear at
// all — the server would stamp it "manual" and the photo would lose the fact that
// its date came out of the file — while a cleared one must appear as null, which
// no amount of omission can express.
func TestPhotoEdit_omittedVersusCleared(t *testing.T) {
	t.Parallel()

	edit := &PhotoEdit{}
	edit.Set("title", "Lake")
	edit.Clear("taken_at")

	body := decodeEdit(t, edit)
	if _, ok := body["description"]; ok {
		t.Error("an untouched field appeared in the body")
	}
	if string(body["title"]) != `"Lake"` {
		t.Errorf("title = %s, want the value", body["title"])
	}
	raw, ok := body["taken_at"]
	if !ok {
		t.Fatal("a cleared field was omitted; the server would leave the date alone")
	}
	if string(raw) != "null" {
		t.Errorf("cleared taken_at = %s, want null", raw)
	}
}

// TestPhotoEdit_emptyStringIsAnEdit verifies emptying a text column is expressed
// by the empty string and is not mistaken for omission.
func TestPhotoEdit_emptyStringIsAnEdit(t *testing.T) {
	t.Parallel()

	edit := &PhotoEdit{}
	edit.Set("notes", "")

	body := decodeEdit(t, edit)
	raw, ok := body["notes"]
	if !ok {
		t.Fatal("an emptied text field was omitted")
	}
	if string(raw) != `""` {
		t.Errorf("emptied notes = %s, want an empty string", raw)
	}
}

// TestPhotoEdit_setTime verifies a timestamp travels in the RFC 3339 spelling the
// API decodes.
func TestPhotoEdit_setTime(t *testing.T) {
	t.Parallel()

	edit := &PhotoEdit{}
	edit.SetTime("taken_at", time.Date(1978, time.June, 3, 14, 30, 0, 0, time.UTC))

	body := decodeEdit(t, edit)
	if string(body["taken_at"]) != `"1978-06-03T14:30:00Z"` {
		t.Errorf("taken_at = %s, want an RFC 3339 timestamp", body["taken_at"])
	}
}

// TestPhotoEdit_emptyIsRefused verifies an edit carrying nothing is refused
// client-side: the server would accept it and write an audit entry for a change
// nobody made.
func TestPhotoEdit_emptyIsRefused(t *testing.T) {
	t.Parallel()

	edit := &PhotoEdit{}
	if !edit.IsEmpty() {
		t.Error("a fresh edit is not empty")
	}
	if err := edit.Validate(); !errors.Is(err, ErrNoEdits) {
		t.Errorf("Validate on an empty edit = %v, want ErrNoEdits", err)
	}
}

// TestPhotoEdit_namesAndBody verifies the two reporting helpers: the sorted field
// names, and the indented body --dry-run prints.
func TestPhotoEdit_namesAndBody(t *testing.T) {
	t.Parallel()

	edit := &PhotoEdit{}
	edit.Set("title", "Lake")
	edit.Set("ai_note", "a boat")

	if got, want := edit.Names(), []string{"ai_note", "title"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Names = %#v, want %#v", got, want)
	}
	body, err := edit.Body()
	if err != nil {
		t.Fatalf("Body returned %v", err)
	}
	if !strings.Contains(body, `"title": "Lake"`) || !strings.Contains(body, "\n") {
		t.Errorf("Body = %q, want indented JSON", body)
	}
}

// TestParseTakenAt verifies the spellings --taken-at accepts, and that a bare
// date means midnight UTC of that day.
func TestParseTakenAt(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"2024-05-01T10:00:00Z":  "2024-05-01T10:00:00Z",
		"2024-05-01T10:00:00":   "2024-05-01T10:00:00Z",
		" 2024-05-01 10:00:00 ": "2024-05-01T10:00:00Z",
		"2024-05-01 10:00":      "2024-05-01T10:00:00Z",
		"2024-05-01":            "2024-05-01T00:00:00Z",
	}
	for in, want := range cases {
		got, err := ParseTakenAt(in)
		if err != nil {
			t.Errorf("ParseTakenAt(%q) returned %v", in, err)
			continue
		}
		if got.UTC().Format(time.RFC3339) != want {
			t.Errorf("ParseTakenAt(%q) = %s, want %s", in, got.UTC().Format(time.RFC3339), want)
		}
	}
	if _, err := ParseTakenAt("last summer"); !errors.Is(err, ErrInvalidTimestamp) {
		t.Errorf("ParseTakenAt(prose) error = %v, want ErrInvalidTimestamp", err)
	}
}

// TestClient_EditPhoto verifies the PATCH carries exactly the edit's body and
// asks for the optional people block when the caller wanted it.
func TestClient_EditPhoto(t *testing.T) {
	t.Parallel()

	var (
		gotMethod string
		gotPath   string
		gotQuery  string
		gotBody   string
	)
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Write([]byte(`{"uid":"pht01","title":"Lake"}`))
	})

	edit := &PhotoEdit{}
	edit.Set("title", "Lake")
	edit.Clear("lat")
	edit.Clear("lng")

	raw, err := client.EditPhoto(t.Context(), "pht01", edit, PhotoDetailOptions{People: true})
	if err != nil {
		t.Fatalf("EditPhoto returned %v", err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/api/v1/photos/pht01" {
		t.Errorf("request = %s %s, want PATCH on the photo", gotMethod, gotPath)
	}
	if gotQuery != "people=true" {
		t.Errorf("query = %q, want people=true", gotQuery)
	}
	if gotBody != `{"lat":null,"lng":null,"title":"Lake"}` {
		t.Errorf("body = %s, want exactly the edited fields", gotBody)
	}
	detail, err := DecodePhotoDetail(raw)
	if err != nil || detail.UID != "pht01" {
		t.Errorf("refreshed detail = %+v, %v", detail, err)
	}
}

// TestClient_EditPhoto_refusals verifies the two things checked before a request
// is made: a blank uid and an edit that would change nothing.
func TestClient_EditPhoto_refusals(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted for an edit that should have been refused")
	})
	edit := &PhotoEdit{}
	edit.Set("title", "Lake")
	if _, err := client.EditPhoto(t.Context(), "", edit, PhotoDetailOptions{}); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("blank uid error = %v, want ErrEmptyUID", err)
	}
	if _, err := client.EditPhoto(t.Context(), "pht01", &PhotoEdit{}, PhotoDetailOptions{}); !errors.Is(err, ErrNoEdits) {
		t.Errorf("empty edit error = %v, want ErrNoEdits", err)
	}
}

// TestClient_EditPhoto_serverRule verifies a rule the CLI deliberately does not
// re-implement — the credit length caps live on the server — is reported as the
// server's own sentence.
func TestClient_EditPhoto_serverRule(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"artist must be at most 255 characters"}`))
	})
	edit := &PhotoEdit{}
	edit.Set("artist", strings.Repeat("x", 300))

	_, err := client.EditPhoto(t.Context(), "pht01", edit, PhotoDetailOptions{})
	var status *StatusError
	if !errors.As(err, &status) || status.Status != http.StatusBadRequest {
		t.Fatalf("error = %v, want a 400 StatusError", err)
	}
	if !strings.Contains(status.Message, "at most 255 characters") {
		t.Errorf("message = %q, want the server's own explanation", status.Message)
	}
}
