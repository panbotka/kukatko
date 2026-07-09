package ctl

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestParseFormat verifies the two supported output formats and the rejection of
// everything else — yaml included, which this CLI deliberately does not emit.
func TestParseFormat(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"table", "json"} {
		got, err := ParseFormat(in)
		if err != nil || string(got) != in {
			t.Errorf("ParseFormat(%q) = %q, %v; want %q, nil", in, got, err, in)
		}
	}
	for _, in := range []string{"", "yaml", "wide", "JSON"} {
		if _, err := ParseFormat(in); !errors.Is(err, ErrInvalidFormat) {
			t.Errorf("ParseFormat(%q) error = %v, want ErrInvalidFormat", in, err)
		}
	}
}

// TestWriteJSON verifies the API's bytes are echoed unchanged — no re-marshal,
// no key reordering, no reindenting — so a machine consumer sees exactly what
// the server sent.
func TestWriteJSON(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"photos":[],  "total":0,"next_offset":null}`)
	var buf bytes.Buffer
	if err := WriteJSON(&buf, raw); err != nil {
		t.Fatalf("WriteJSON returned %v", err)
	}
	if got := buf.String(); got != string(raw)+"\n" {
		t.Errorf("WriteJSON wrote %q, want the input bytes plus a newline", got)
	}
}

// takenAt is a fixed timestamp for deterministic table assertions.
func takenAt(t *testing.T) *time.Time {
	t.Helper()
	ts := time.Date(2024, time.May, 1, 10, 22, 33, 0, time.UTC)
	return &ts
}

// TestWritePhotoPage verifies the table carries a header, one row per photo, and
// a summary line naming the page, the total and the next offset.
func TestWritePhotoPage(t *testing.T) {
	t.Parallel()

	next := 2
	page := PhotoPage{
		Photos: []Photo{
			{UID: "pht01", FileName: "a.jpg", FileSize: 2 << 20, TakenAt: takenAt(t), Title: "Lake"},
			{UID: "pht02", FileName: "b.mp4", FileSize: 10 << 20},
		},
		Total: 42, Limit: 2, Offset: 0, NextOffset: &next,
	}
	var buf bytes.Buffer
	if err := WritePhotoPage(&buf, page); err != nil {
		t.Fatalf("WritePhotoPage returned %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"UID", "TAKEN", "TITLE", "FILE", "SIZE",
		"pht01", "2024-05-01 10:22", "Lake", "a.jpg", "2.0 MiB",
		"pht02", "b.mp4", "10.0 MiB",
		"2 of 42 photos", "offset 0", "next offset 2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table output does not contain %q:\n%s", want, out)
		}
	}
	// The untitled photo shows a dash rather than an empty cell.
	if !strings.Contains(out, "-") {
		t.Errorf("empty title was not rendered as a dash:\n%s", out)
	}
}

// TestWritePhotoPage_empty verifies an empty result prints one line and no table
// header, so an agent reading the output cannot mistake a header for a row.
func TestWritePhotoPage_empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WritePhotoPage(&buf, PhotoPage{Photos: []Photo{}, Limit: 100}); err != nil {
		t.Fatalf("WritePhotoPage returned %v", err)
	}
	if got := buf.String(); got != "no photos found\n" {
		t.Errorf("empty page printed %q, want a single no-photos line", got)
	}
}

// TestWritePhotoPage_searchSummary verifies a search result names its effective
// ranking mode and says so when the sidecar was offline.
func TestWritePhotoPage_searchSummary(t *testing.T) {
	t.Parallel()

	page := PhotoPage{
		Photos: []Photo{{UID: "pht01", FileName: "a.jpg"}},
		Total:  1, Limit: 100, Mode: SearchFulltext, Degraded: true,
	}
	var buf bytes.Buffer
	if err := WritePhotoPage(&buf, page); err != nil {
		t.Fatalf("WritePhotoPage returned %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "mode fulltext") || !strings.Contains(out, "degraded") {
		t.Errorf("search summary %q does not report the degraded mode", out)
	}
	if strings.Contains(out, "next offset") {
		t.Errorf("summary %q advertises a next page that does not exist", out)
	}
}

// TestWritePhotoPage_elidesLongCells verifies an overlong title is truncated so
// a row stays inside a terminal.
func TestWritePhotoPage_elidesLongCells(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", titleWidth+20)
	var buf bytes.Buffer
	page := PhotoPage{Photos: []Photo{{UID: "pht01", Title: long, FileName: "a.jpg"}}, Total: 1}
	if err := WritePhotoPage(&buf, page); err != nil {
		t.Fatalf("WritePhotoPage returned %v", err)
	}
	if strings.Contains(buf.String(), long) {
		t.Error("an overlong title was not elided")
	}
	if !strings.Contains(buf.String(), "…") {
		t.Error("the elided title is not marked with an ellipsis")
	}
}

// TestWritePhotoDetail verifies the key/value table names every field the detail
// endpoint returns.
func TestWritePhotoDetail(t *testing.T) {
	t.Parallel()

	lat, lng := 50.08750, 14.42111
	detail := PhotoDetail{
		Photo: Photo{
			UID: "pht01", Title: "Lake", FileName: "a.jpg", FileSize: 1536,
			FileMime: "image/jpeg", FileWidth: 800, FileHeight: 600,
			MediaType: "image", TakenAt: takenAt(t), IsFavorite: true, Rating: 4, Flag: "pick",
		},
		Description: "an evening",
		CameraMake:  "Canon", CameraModel: "R6", LensModel: "RF 24-70",
		Lat: &lat, Lng: &lng,
		Files:  []PhotoFile{{FilePath: "2024/05/a.jpg", IsPrimary: true}},
		Albums: []NamedRef{{UID: "alb1", Title: "Trip"}},
		Labels: []NamedRef{{UID: "lbl1", Name: "lake"}},
	}
	var buf bytes.Buffer
	if err := WritePhotoDetail(&buf, detail); err != nil {
		t.Fatalf("WritePhotoDetail returned %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"UID", "pht01", "TITLE", "Lake", "DESCRIPTION", "an evening",
		"TAKEN", "2024-05-01 10:22", "SIZE", "1.5 KiB", "DIMENSIONS", "800×600",
		"CAMERA", "Canon R6", "LENS", "RF 24-70", "GPS", "50.08750, 14.42111",
		"FAVORITE", "true", "RATING", "4", "FLAG", "pick",
		"FILES", "ALBUMS", "Trip", "LABELS", "lake",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output does not contain %q:\n%s", want, out)
		}
	}
}

// TestWritePhotoDetail_sparse verifies absent optional fields render as dashes
// rather than as empty cells or "<nil>".
func TestWritePhotoDetail_sparse(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WritePhotoDetail(&buf, PhotoDetail{Photo: Photo{UID: "pht01"}}); err != nil {
		t.Fatalf("WritePhotoDetail returned %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "<nil>") {
		t.Errorf("a nil field leaked into the output:\n%s", out)
	}
	for _, want := range []string{"TAKEN", "GPS", "DIMENSIONS"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output does not contain %q:\n%s", want, out)
		}
	}
}

// TestWriteContexts verifies the context table marks the current context and
// never prints a token, only whether one is stored.
func TestWriteContexts(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		CurrentContext: "prod",
		Contexts: []Context{
			{Name: "prod", Server: "https://prod.example.com", Token: "kkt_p_supersecret"},
			{Name: "dev", Server: "http://localhost:8080"},
		},
	}
	var buf bytes.Buffer
	if err := WriteContexts(&buf, cfg); err != nil {
		t.Fatalf("WriteContexts returned %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "supersecret") {
		t.Errorf("WriteContexts leaked a token:\n%s", out)
	}
	for _, want := range []string{"CURRENT", "NAME", "SERVER", "TOKEN", "prod", "dev", "stored", "not set", "*"} {
		if !strings.Contains(out, want) {
			t.Errorf("context table does not contain %q:\n%s", want, out)
		}
	}
}

// TestWriteContexts_empty verifies an unconfigured client says so.
func TestWriteContexts_empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteContexts(&buf, &Config{}); err != nil {
		t.Fatalf("WriteContexts returned %v", err)
	}
	if got := buf.String(); got != "no contexts configured\n" {
		t.Errorf("empty context table printed %q", got)
	}
	buf.Reset()
	if err := WriteContexts(&buf, nil); err != nil {
		t.Fatalf("WriteContexts(nil) returned %v", err)
	}
	if got := buf.String(); got != "no contexts configured\n" {
		t.Errorf("nil context table printed %q", got)
	}
}

// TestFormatSize verifies byte counts step through binary units.
func TestFormatSize(t *testing.T) {
	t.Parallel()

	tests := map[int64]string{
		0: "-", -1: "-", 1: "1 B", 1023: "1023 B", 1024: "1.0 KiB",
		1536: "1.5 KiB", 2 << 20: "2.0 MiB", 3 << 30: "3.0 GiB", 5 << 40: "5.0 TiB",
	}
	for in, want := range tests {
		if got := formatSize(in); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", in, got, want)
		}
	}
}

// TestFormatTime verifies a nil or zero timestamp renders as a dash and a real
// one as a minute-precision UTC stamp.
func TestFormatTime(t *testing.T) {
	t.Parallel()

	if got := formatTime(nil); got != "-" {
		t.Errorf("formatTime(nil) = %q, want a dash", got)
	}
	var zero time.Time
	if got := formatTime(&zero); got != "-" {
		t.Errorf("formatTime(zero) = %q, want a dash", got)
	}
	ts := time.Date(2024, time.May, 1, 12, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	if got := formatTime(&ts); got != "2024-05-01 10:00" {
		t.Errorf("formatTime(CEST noon) = %q, want the UTC stamp", got)
	}
}

// TestFormatDimensions verifies an unknown side renders as a dash.
func TestFormatDimensions(t *testing.T) {
	t.Parallel()

	if got := formatDimensions(800, 600); got != "800×600" {
		t.Errorf("formatDimensions(800, 600) = %q", got)
	}
	if got := formatDimensions(0, 600); got != "-" {
		t.Errorf("formatDimensions(0, 600) = %q, want a dash", got)
	}
}

// TestFormatGPS verifies a photo without a position renders as a dash.
func TestFormatGPS(t *testing.T) {
	t.Parallel()

	lat := 50.0
	if got := formatGPS(&lat, nil); got != "-" {
		t.Errorf("formatGPS with a missing longitude = %q, want a dash", got)
	}
	lng := 14.5
	if got := formatGPS(&lat, &lng); got != "50.00000, 14.50000" {
		t.Errorf("formatGPS = %q", got)
	}
}

// TestElide verifies multi-byte strings are cut by rune, not by byte.
func TestElide(t *testing.T) {
	t.Parallel()

	if got := elide("krátký", 10); got != "krátký" {
		t.Errorf("elide of a short string = %q, want it untouched", got)
	}
	got := elide("ěščřžýáíé", 5)
	if []rune(got)[4] != '…' || len([]rune(got)) != 5 {
		t.Errorf("elide(%q, 5) = %q, want 5 runes ending in an ellipsis", "ěščřžýáíé", got)
	}
}

// TestDash verifies an empty cell becomes a dash.
func TestDash(t *testing.T) {
	t.Parallel()

	if got := dash(""); got != "-" {
		t.Errorf("dash(\"\") = %q", got)
	}
	if got := dash("x"); got != "x" {
		t.Errorf("dash(\"x\") = %q", got)
	}
}

// TestJoinRefs verifies references render as a comma-separated list of names.
func TestJoinRefs(t *testing.T) {
	t.Parallel()

	if got := joinRefs(nil); got != "" {
		t.Errorf("joinRefs(nil) = %q, want empty", got)
	}
	refs := []NamedRef{{Title: "Trip"}, {Name: "lake"}}
	if got := joinRefs(refs); got != "Trip, lake" {
		t.Errorf("joinRefs = %q, want \"Trip, lake\"", got)
	}
}
