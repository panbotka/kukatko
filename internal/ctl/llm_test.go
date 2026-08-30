package ctl

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// renderLLM runs WriteLLM over a body and decodes what it wrote, so a test can
// assert on the shape rather than on byte-for-byte JSON.
func renderLLM(t *testing.T, body string, fields []string) map[string]any {
	t.Helper()
	var buf strings.Builder
	if err := WriteLLM(&buf, json.RawMessage(body), fields); err != nil {
		t.Fatalf("WriteLLM returned %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(buf.String()), &decoded); err != nil {
		t.Fatalf("llm output %q does not parse: %v", buf.String(), err)
	}
	return decoded
}

// TestParseFormat_llm verifies the third format is accepted, so every ctl command
// gets it without each one opting in.
func TestParseFormat_llm(t *testing.T) {
	t.Parallel()

	format, err := ParseFormat("llm")
	if err != nil {
		t.Fatalf("ParseFormat(llm) returned %v", err)
	}
	if format != FormatLLM {
		t.Errorf("format = %q, want %q", format, FormatLLM)
	}
	if _, err := ParseFormat("yaml"); err == nil {
		t.Error("ParseFormat(yaml) succeeded, want ErrInvalidFormat")
	}
}

// TestWriteLLM_dropsNoiseKeepsMeaning verifies the llm rendering keeps what a
// reader learns from — identity, texts, the date with its precision, the location
// with its origin, people, albums, labels, media type and dimensions — and drops
// the raw EXIF blob, the signed media URLs and the machine-derived columns.
func TestWriteLLM_dropsNoiseKeepsMeaning(t *testing.T) {
	t.Parallel()

	body := `{
		"uid":"pht01","title":"Lake","description":"","notes":"a note",
		"taken_at":"2024-05-01T10:00:00Z","taken_at_precision":"year","taken_at_estimated":true,
		"lat":50.1,"lng":14.4,"location_source":"estimate",
		"media_type":"image","file_width":800,"file_height":600,"file_name":"a.jpg",
		"rating":0,"is_favorite":false,
		"exif":{"Make":"Canon","LensInfo":"24-70"},
		"thumb_url":"https://edge.example/signed/very/long","download_url":"https://edge.example/dl",
		"file_hash":"deadbeef","file_path":"2024/05/a.jpg","file_orientation":1,
		"software":"Lightroom","color_profile":"sRGB","created_at":"2024-06-01T00:00:00Z",
		"files":[{"file_path":"2024/05/a.jpg","is_primary":true}],
		"albums":[{"uid":"alb1","title":"Trip"}],
		"people":[{"subject_name":"Alice","subject_uid":"su1","det_score":0.9}]
	}`
	got := renderLLM(t, body, nil)

	for _, key := range []string{
		"uid", "title", "notes", "taken_at", "taken_at_precision", "taken_at_estimated",
		"lat", "lng", "location_source", "media_type", "file_width", "file_height",
		"file_name", "albums", "people",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("key %q was dropped, want it kept", key)
		}
	}
	for _, key := range []string{
		"exif", "thumb_url", "download_url", "file_hash", "file_path", "file_orientation",
		"software", "color_profile", "created_at", "files",
	} {
		if _, ok := got[key]; ok {
			t.Errorf("key %q survived, want it dropped as noise", key)
		}
	}
	// Empty and zero-valued fields say exactly as much as their absence.
	for _, key := range []string{"description", "rating", "is_favorite"} {
		if _, ok := got[key]; ok {
			t.Errorf("zero-valued %q survived, want it dropped", key)
		}
	}
}

// TestWriteLLM_isSmallerThanJSON verifies the point of the format: the same body
// costs materially less than the server's own bytes.
func TestWriteLLM_isSmallerThanJSON(t *testing.T) {
	t.Parallel()

	body := `{"uid":"pht01","title":"Lake","description":"","notes":"","rating":0,
		"exif":{"Make":"Canon","Model":"R6","LensModel":"RF 24-70mm F2.8 L IS USM","ISO":100},
		"thumb_url":"https://edge.example/signed/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	var buf strings.Builder
	if err := WriteLLM(&buf, json.RawMessage(body), nil); err != nil {
		t.Fatalf("WriteLLM returned %v", err)
	}
	if len(buf.String()) >= len(body) {
		t.Errorf("llm output is %d bytes for a %d-byte body, want it smaller", len(buf.String()), len(body))
	}
}

// TestWriteLLM_fieldsAllowlist verifies --fields narrows the result and reaches
// through a list envelope without the caller having to name the envelope.
func TestWriteLLM_fieldsAllowlist(t *testing.T) {
	t.Parallel()

	body := `{"photos":[{"uid":"a","title":"one","file_name":"a.jpg"},
		{"uid":"b","title":"two","file_name":"b.jpg"}],"total":2,"offset":0}`
	got := renderLLM(t, body, []string{"uid", "title"})

	if _, ok := got["total"]; ok {
		t.Error("total survived, want only the named keys and the road to them")
	}
	rows, ok := got["photos"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("photos = %#v, want the two rows", got["photos"])
	}
	first, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("first row = %#v, want an object", rows[0])
	}
	want := map[string]any{"uid": "a", "title": "one"}
	if !reflect.DeepEqual(first, want) {
		t.Errorf("first row = %#v, want %#v", first, want)
	}
}

// TestWriteLLM_fieldsMatchingNothing verifies an allowlist nothing matches prints
// an empty object rather than the unfiltered body — a silent fallback to
// everything is exactly the surprise a token budget cannot absorb.
func TestWriteLLM_fieldsMatchingNothing(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	if err := WriteLLM(&buf, json.RawMessage(`{"uid":"a","title":"one"}`), []string{"nope"}); err != nil {
		t.Fatalf("WriteLLM returned %v", err)
	}
	if strings.TrimSpace(buf.String()) != "{}" {
		t.Errorf("output = %q, want an empty object", buf.String())
	}
}

// TestWriteLLM_emptyResult verifies a body that loses everything still prints
// valid JSON, so a pipeline never has to special-case an empty line.
func TestWriteLLM_emptyResult(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	if err := WriteLLM(&buf, json.RawMessage(`{"notes":"","rating":0,"exif":{}}`), nil); err != nil {
		t.Fatalf("WriteLLM returned %v", err)
	}
	if strings.TrimSpace(buf.String()) != "{}" {
		t.Errorf("output = %q, want an empty object", buf.String())
	}
}

// TestWriteLLM_keepsNumbersVerbatim verifies a number is re-encoded as the server
// wrote it, not through a float that would round a coordinate.
func TestWriteLLM_keepsNumbersVerbatim(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	body := `{"lat":49.123456789012345,"total":100}`
	if err := WriteLLM(&buf, json.RawMessage(body), nil); err != nil {
		t.Fatalf("WriteLLM returned %v", err)
	}
	if !strings.Contains(buf.String(), "49.123456789012345") {
		t.Errorf("output = %q, want the coordinate unrounded", buf.String())
	}
}

// TestWriteLLM_malformedBody verifies a body that is not JSON is reported rather
// than printed back as if it were a result.
func TestWriteLLM_malformedBody(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	if err := WriteLLM(&buf, json.RawMessage("<html>nope</html>"), nil); err == nil {
		t.Error("WriteLLM accepted a non-JSON body, want an error")
	}
}

// TestParseFields verifies the allowlist is split on commas with the blanks
// forgiven, so a copied-and-pasted list still works.
func TestParseFields(t *testing.T) {
	t.Parallel()

	got := ParseFields(" uid, title ,,")
	want := []string{"uid", "title"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseFields = %#v, want %#v", got, want)
	}
	if ParseFields("  ") != nil {
		t.Error("a blank --fields should mean no allowlist at all")
	}
}

// TestWriteAck_llm verifies the synthesized 204 confirmation is available in the
// llm format too, so a pipeline reading llm output never gets a bare prose line.
func TestWriteAck_llm(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	if err := WriteAck(&buf, Output{Format: FormatLLM}, "photo pht01 favorited"); err != nil {
		t.Fatalf("WriteAck returned %v", err)
	}
	var ack Ack
	if err := json.Unmarshal([]byte(buf.String()), &ack); err != nil {
		t.Fatalf("llm ack %q does not parse: %v", buf.String(), err)
	}
	if ack.Status != "ok" || ack.Message != "photo pht01 favorited" {
		t.Errorf("ack = %+v, want an ok status and the message", ack)
	}
}
