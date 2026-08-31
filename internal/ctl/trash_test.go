package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// trashServer answers the two endpoints a trash listing reads: the retention
// window, and the archived photos in pages of the given size. pages are the
// photo rows of GET /photos?archived=only, in the order the server would return
// them (which is deliberately not archived_at order).
func trashServer(t *testing.T, retentionDays int, rows []string) (*Client, *[]string) {
	t.Helper()

	var seen []string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		if r.URL.Path == "/api/v1/trash/info" {
			fmt.Fprintf(w, `{"retention_days":%d}`, retentionDays)
			return
		}
		fmt.Fprintf(w, `{"photos":[%s],"total":%d,"limit":500,"offset":0,"next_offset":null}`,
			strings.Join(rows, ","), len(rows))
	})
	return client, &seen
}

// archivedRow renders one archived photo row of a /photos listing.
func archivedRow(uid, name, archivedAt string, size int) string {
	return fmt.Sprintf(`{"uid":%q,"file_name":%q,"file_size":%d,"archived_at":%q}`,
		uid, name, size, archivedAt)
}

// TestClient_FetchTrash verifies the listing is read through the ordinary
// archived-only filter, sorted oldest-archived first — the order retention takes
// them in — and stamped with each photo's purge date.
func TestClient_FetchTrash(t *testing.T) {
	t.Parallel()

	rows := []string{
		archivedRow("pht02", "b.jpg", "2026-08-20T10:00:00Z", 1024),
		archivedRow("pht01", "a.jpg", "2026-01-02T10:00:00Z", 2048),
		`{"uid":"pht03","file_name":"c.jpg","file_size":512}`,
	}
	client, seen := trashServer(t, 30, rows)

	trash, err := client.FetchTrash(t.Context())
	if err != nil {
		t.Fatalf("FetchTrash returned %v", err)
	}
	if trash.RetentionDays != 30 || !trash.RetentionEnabled() {
		t.Errorf("retention = %d, want the configured 30 days", trash.RetentionDays)
	}
	if trash.Total != 3 || len(trash.Photos) != 3 || trash.Truncated {
		t.Fatalf("trash = %+v, want all three photos", trash)
	}
	gotOrder := []string{trash.Photos[0].UID, trash.Photos[1].UID, trash.Photos[2].UID}
	if strings.Join(gotOrder, ",") != "pht01,pht02,pht03" {
		t.Errorf("order = %v, want oldest archived first with the undated one last", gotOrder)
	}
	want := time.Date(2026, time.February, 1, 10, 0, 0, 0, time.UTC)
	if trash.Photos[0].PurgeAt == nil || !trash.Photos[0].PurgeAt.Equal(want) {
		t.Errorf("purge date = %v, want archived_at + 30 days (%v)", trash.Photos[0].PurgeAt, want)
	}
	if trash.Photos[2].PurgeAt != nil {
		t.Error("a photo with no archived_at was given a purge date it cannot have")
	}
	if !strings.Contains(strings.Join(*seen, " "), "archived=only") {
		t.Errorf("requests = %v, want the archived-only listing", *seen)
	}
}

// TestClient_FetchTrash_retentionDisabled verifies that with retention off no
// photo is given a purge date — nothing goes away on its own, so a countdown
// would be a promise the instance does not make.
func TestClient_FetchTrash_retentionDisabled(t *testing.T) {
	t.Parallel()

	client, _ := trashServer(t, 0, []string{archivedRow("pht01", "a.jpg", "2026-01-02T10:00:00Z", 2048)})

	trash, err := client.FetchTrash(t.Context())
	if err != nil {
		t.Fatalf("FetchTrash returned %v", err)
	}
	if trash.RetentionEnabled() {
		t.Error("retention reads as enabled with retention_days = 0")
	}
	if trash.Photos[0].PurgeAt != nil {
		t.Errorf("purge date = %v, want none with retention off", trash.Photos[0].PurgeAt)
	}
}

// TestClient_FetchTrash_pages verifies the listing follows next_offset instead
// of reporting the first page as if it were the whole trash.
func TestClient_FetchTrash_pages(t *testing.T) {
	t.Parallel()

	var offsets []string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/trash/info" {
			w.Write([]byte(`{"retention_days":365}`))
			return
		}
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		if offset == "" {
			fmt.Fprintf(w, `{"photos":[%s],"total":2,"limit":500,"offset":0,"next_offset":1}`,
				archivedRow("pht01", "a.jpg", "2026-01-02T10:00:00Z", 1))
			return
		}
		fmt.Fprintf(w, `{"photos":[%s],"total":2,"limit":500,"offset":1,"next_offset":null}`,
			archivedRow("pht02", "b.jpg", "2026-01-03T10:00:00Z", 1))
	})

	trash, err := client.FetchTrash(t.Context())
	if err != nil {
		t.Fatalf("FetchTrash returned %v", err)
	}
	if len(trash.Photos) != 2 {
		t.Errorf("photos = %+v, want both pages", trash.Photos)
	}
	if strings.Join(offsets, ",") != ",1" {
		t.Errorf("offsets = %v, want the first page then next_offset", offsets)
	}
}

// TestTrash_ArchivedBefore verifies the age filter keeps exactly the photos an
// age-bounded purge would destroy, and nothing that is merely undated.
func TestTrash_ArchivedBefore(t *testing.T) {
	t.Parallel()

	old := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	trash := Trash{Total: 3, Photos: []TrashItem{
		{UID: "pht01", ArchivedAt: &old},
		{UID: "pht02", ArchivedAt: &recent},
		{UID: "pht03"},
	}}
	cutoff := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	got := trash.ArchivedBefore(cutoff)
	if len(got.Photos) != 1 || got.Photos[0].UID != "pht01" || got.Total != 1 {
		t.Errorf("ArchivedBefore = %+v, want only the photo archived before the cutoff", got)
	}
}

// TestPurgeCutoff verifies the cutoff is the same arithmetic the server does.
func TestPurgeCutoff(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	want := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	if got := PurgeCutoff(now, 30); !got.Equal(want) {
		t.Errorf("PurgeCutoff(30 days) = %v, want %v", got, want)
	}
	if got := PurgeCutoff(now, 0); !got.Equal(now) {
		t.Errorf("PurgeCutoff(0 days) = %v, want the whole trash (now)", got)
	}
}

// TestClient_PurgePhoto verifies the single purge carries the API's own
// confirm=true guard and refuses a blank uid locally.
func TestClient_PurgePhoto(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotQuery string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.PurgePhoto(t.Context(), "pht01"); err != nil {
		t.Fatalf("PurgePhoto returned %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/photos/pht01/purge" || gotQuery != "confirm=true" {
		t.Errorf("request = %s %s?%s, want the confirmed purge", gotMethod, gotPath, gotQuery)
	}
	if err := client.PurgePhoto(t.Context(), " "); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("PurgePhoto(blank) error = %v, want ErrEmptyUID", err)
	}
}

// TestClient_EmptyTrash verifies emptying the trash is confirmed to the server
// and its counts come back decodable.
func TestClient_EmptyTrash(t *testing.T) {
	t.Parallel()

	var gotPath, gotQuery string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Write([]byte(`{"purged":7,"failed":1}`))
	})

	raw, err := client.EmptyTrash(t.Context())
	if err != nil {
		t.Fatalf("EmptyTrash returned %v", err)
	}
	if gotPath != "/api/v1/trash/empty" || gotQuery != "confirm=true" {
		t.Errorf("request = %s?%s, want the confirmed empty", gotPath, gotQuery)
	}
	result, err := DecodePurgeResult(raw)
	if err != nil {
		t.Fatalf("DecodePurgeResult returned %v", err)
	}
	if result.Purged != 7 || result.Failed != 1 {
		t.Errorf("result = %+v, want 7 purged and 1 failed", result)
	}
}

// TestClient_PurgeOlderThan verifies the age parameter travels alongside the
// confirmation, and that a negative window is refused before any request.
func TestClient_PurgeOlderThan(t *testing.T) {
	t.Parallel()

	var gotQuery string
	called := 0
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		called++
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"purged":3,"failed":0}`))
	})

	if _, err := client.PurgeOlderThan(t.Context(), 30); err != nil {
		t.Fatalf("PurgeOlderThan returned %v", err)
	}
	if !strings.Contains(gotQuery, "days=30") || !strings.Contains(gotQuery, "confirm=true") {
		t.Errorf("query = %q, want the window and the confirmation", gotQuery)
	}
	if _, err := client.PurgeOlderThan(t.Context(), -1); !errors.Is(err, ErrInvalidRetentionDays) {
		t.Errorf("PurgeOlderThan(-1) error = %v, want ErrInvalidRetentionDays", err)
	}
	if called != 1 {
		t.Errorf("the server was called %d times, want only the valid purge", called)
	}
}

// TestWriteTrash verifies the listing names every photo at risk, says when
// retention will take it, and closes with a summary that reports the retention
// window and the dry run.
func TestWriteTrash(t *testing.T) {
	t.Parallel()

	archived := time.Date(2026, time.January, 2, 10, 0, 0, 0, time.UTC)
	purge := time.Date(2026, time.February, 1, 10, 0, 0, 0, time.UTC)
	view := TrashView{
		Heading: "dry run: emptying the trash would permanently delete these photos:",
		DryRun:  true,
		Trash: Trash{RetentionDays: 30, Total: 1, Photos: []TrashItem{{
			UID: "pht01", FileName: "a.jpg", Title: "Lake", FileSize: 2097152,
			ArchivedAt: &archived, PurgeAt: &purge,
		}}},
	}
	var buf bytes.Buffer
	if err := WriteTrash(&buf, Output{Format: FormatTable}, view); err != nil {
		t.Fatalf("WriteTrash returned %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"dry run: emptying the trash", "pht01", "a.jpg", "Lake", "2.0 MiB",
		"2026-01-02 10:00", "2026-02-01 10:00",
		"1 of 1 photos", "retention 30 days", "dry run: nothing was deleted",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("trash listing is missing %q:\n%s", want, out)
		}
	}
}

// TestWriteTrash_empty verifies an empty trash says so under its heading.
func TestWriteTrash_empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	view := TrashView{Heading: "in the trash now, oldest first:"}
	if err := WriteTrash(&buf, Output{Format: FormatTable}, view); err != nil {
		t.Fatalf("WriteTrash returned %v", err)
	}
	if !strings.Contains(buf.String(), "the trash is empty") {
		t.Errorf("output = %q, want the empty line", buf.String())
	}
}

// TestWriteTrash_emptyDryRun verifies a rehearsal that matched nothing says so,
// rather than claiming the trash is empty when it is not.
func TestWriteTrash_emptyDryRun(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	view := TrashView{Heading: "dry run:", DryRun: true, Trash: Trash{Total: 0}}
	if err := WriteTrash(&buf, Output{Format: FormatTable}, view); err != nil {
		t.Fatalf("WriteTrash returned %v", err)
	}
	if !strings.Contains(buf.String(), "nothing would be deleted") {
		t.Errorf("output = %q, want the rehearsal's own empty line", buf.String())
	}
	if strings.Contains(buf.String(), "the trash is empty") {
		t.Errorf("output = %q, claims an empty trash a filtered rehearsal cannot know", buf.String())
	}
}

// TestWriteTrash_retentionOff verifies the summary says outright that nothing in
// the trash goes away on its own.
func TestWriteTrash_retentionOff(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	view := TrashView{Heading: "x", Trash: Trash{Total: 1, Photos: []TrashItem{{UID: "pht01"}}}}
	if err := WriteTrash(&buf, Output{Format: FormatTable}, view); err != nil {
		t.Fatalf("WriteTrash returned %v", err)
	}
	if !strings.Contains(buf.String(), "retention off") {
		t.Errorf("summary = %q, want retention reported as off", buf.String())
	}
}

// TestWriteTrash_llm verifies the machine formats emit the composed view, which
// has no server bytes to pass through, as JSON an agent can read.
func TestWriteTrash_llm(t *testing.T) {
	t.Parallel()

	archived := time.Date(2026, time.January, 2, 10, 0, 0, 0, time.UTC)
	view := TrashView{
		Heading: "dry run:",
		DryRun:  true,
		Trash: Trash{RetentionDays: 30, Total: 1, Photos: []TrashItem{
			{UID: "pht01", FileName: "a.jpg", ArchivedAt: &archived},
		}},
	}
	var buf bytes.Buffer
	if err := WriteTrash(&buf, Output{Format: FormatLLM}, view); err != nil {
		t.Fatalf("WriteTrash returned %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("llm output is not JSON: %v (%s)", err, buf.String())
	}
	if decoded["dry_run"] != true {
		t.Errorf("llm output = %s, want the dry run marked", buf.String())
	}
	if _, ok := decoded["photos"]; !ok {
		t.Errorf("llm output = %s, want the photos at risk named", buf.String())
	}
}

// TestWritePurgeResult verifies a partial failure is stated, not buried in a
// count: an operator who just emptied the trash will otherwise assume it is empty.
func TestWritePurgeResult(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WritePurgeResult(&buf, Output{Format: FormatTable}, PurgeResult{Purged: 7, Failed: 2}); err != nil {
		t.Fatalf("WritePurgeResult returned %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "7 photos permanently deleted") || !strings.Contains(out, "still in the trash") {
		t.Errorf("output = %q, want both the deletions and the survivors", out)
	}

	buf.Reset()
	if err := WritePurgeResult(&buf, Output{Format: FormatTable}, PurgeResult{Purged: 7}); err != nil {
		t.Fatalf("WritePurgeResult returned %v", err)
	}
	if strings.Contains(buf.String(), "still in the trash") {
		t.Errorf("output = %q, want no failure clause when nothing failed", buf.String())
	}
}
