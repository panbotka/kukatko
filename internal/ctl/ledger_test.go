package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ledgerPageBody returns one listing page for the ledger tests: two rows, one
// with a comment and a coarse date, one bare, plus the paging fields given.
func ledgerPageBody(total int, next string) string {
	return `{"photos":[
{"uid":"ph1","file_name":"a.jpg","title":"Rekruti","taken_at":"1974-06-01T00:00:00Z",
 "taken_at_precision":"month","taken_at_source":"manual","taken_at_estimated":true,
 "last_comment":{"uid":"cm1","body":"Datum z popisku na zadní straně.\nDruhý řádek.",
   "author_uid":"us9","author_name":"Agent","created_at":"2026-09-19T10:00:00Z"}},
{"uid":"ph2","file_name":"b.jpg","title":"","taken_at_source":"","last_comment":null}
],"total":` + strconv.Itoa(total) + `,"limit":500,"offset":0,"next_offset":` + next + `}`
}

// TestTaskLedger_pages verifies the client scopes every request to the task,
// walks next_offset to the end and returns one envelope holding every row.
func TestTaskLedger_pages(t *testing.T) {
	t.Parallel()

	var gotQueries []string
	client := testClient(t, "tok", func(w http.ResponseWriter, r *http.Request) {
		gotQueries = append(gotQueries, r.URL.RawQuery)
		if r.URL.Query().Get("offset") == "" {
			_, _ = w.Write([]byte(ledgerPageBody(4, "2")))
			return
		}
		_, _ = w.Write([]byte(ledgerPageBody(4, "null")))
	})
	raw, err := client.TaskLedger(t.Context(), "tk1")
	if err != nil {
		t.Fatalf("TaskLedger: %v", err)
	}
	if len(gotQueries) != 2 {
		t.Fatalf("made %d requests, want 2 pages: %v", len(gotQueries), gotQueries)
	}
	for _, q := range gotQueries {
		if !strings.Contains(q, "task=tk1") || !strings.Contains(q, "limit=500") {
			t.Errorf("query %q lacks the task scope or the page size", q)
		}
	}
	if !strings.Contains(gotQueries[1], "offset=2") {
		t.Errorf("second query %q did not continue at next_offset", gotQueries[1])
	}
	ledger, err := DecodeTaskLedger(raw)
	if err != nil {
		t.Fatalf("DecodeTaskLedger: %v", err)
	}
	if ledger.TaskUID != "tk1" || ledger.Total != 4 || len(ledger.Photos) != 4 {
		t.Errorf("ledger = %s/%d/%d rows, want tk1/4/4", ledger.TaskUID, ledger.Total, len(ledger.Photos))
	}
	if c := ledger.Photos[0].LastComment; c == nil || c.AuthorName != "Agent" || c.UID != "cm1" {
		t.Errorf("first row's comment = %+v, want cm1 by Agent", c)
	}
	if ledger.Photos[1].LastComment != nil {
		t.Errorf("second row's comment = %+v, want nil", ledger.Photos[1].LastComment)
	}
	if ledger.Photos[0].TakenAtPrecision != "month" || !ledger.Photos[0].TakenAtEstimated {
		t.Errorf("first row's date qualifiers = %+v, want month/estimated", ledger.Photos[0])
	}
}

// TestTaskLedger_refusals verifies a blank uid costs no round trip and a server
// failure is reported with the task named.
func TestTaskLedger_refusals(t *testing.T) {
	t.Parallel()

	client := testClient(t, "tok", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	})
	if _, err := client.TaskLedger(t.Context(), ""); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("TaskLedger(\"\") error = %v, want ErrEmptyUID", err)
	}
	_, err := client.TaskLedger(t.Context(), "tk_missing")
	if err == nil || !strings.Contains(err.Error(), "tk_missing") {
		t.Errorf("TaskLedger(missing) error = %v, want one naming the task", err)
	}
}

// TestWriteTaskLedger verifies the table's columns, the precision-aware date,
// the one-line excerpt and the summary that counts the rows without a comment.
func TestWriteTaskLedger(t *testing.T) {
	t.Parallel()

	ledger, err := DecodeTaskLedger(json.RawMessage(
		`{"task_uid":"tk1","total":3,` + ledgerPageBody(3, "null")[len(`{`):]))
	if err != nil {
		t.Fatalf("DecodeTaskLedger: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteTaskLedger(&buf, ledger); err != nil {
		t.Fatalf("WriteTaskLedger: %v", err)
	}
	got := buf.String()
	for _, want := range []string{
		"UID", "TAKEN", "SOURCE", "LAST COMMENT", "BY",
		"ph1", "~1974-06", "manual", "Datum z popisku na zadní straně.", "Agent",
		"2 photos · 1 with a comment · 1 without", "showing 2 of 3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Druhý řádek") {
		t.Errorf("the excerpt ran past the first line:\n%s", got)
	}

	buf.Reset()
	if err := WriteTaskLedger(&buf, TaskLedger{TaskUID: "tk1"}); err != nil {
		t.Fatalf("WriteTaskLedger(empty): %v", err)
	}
	if strings.TrimSpace(buf.String()) != "no photos in the task" {
		t.Errorf("empty output = %q", buf.String())
	}
}

// TestRenderTaken verifies each precision renders as its own shape and an
// estimate is marked.
func TestRenderTaken(t *testing.T) {
	t.Parallel()

	stamp := time.Date(1974, 6, 14, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		takenAt   *time.Time
		precision string
		estimated bool
		want      string
	}{
		{name: "day", takenAt: &stamp, precision: "day", want: "1974-06-14"},
		{name: "unknown precision is a day", takenAt: &stamp, want: "1974-06-14"},
		{name: "month", takenAt: &stamp, precision: "month", want: "1974-06"},
		{name: "year", takenAt: &stamp, precision: "year", want: "1974"},
		{name: "decade", takenAt: &stamp, precision: "decade", want: "1970s"},
		{name: "estimate", takenAt: &stamp, precision: "year", estimated: true, want: "~1974"},
		{name: "no date", want: "-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := renderTaken(tt.takenAt, tt.precision, tt.estimated); got != tt.want {
				t.Errorf("renderTaken = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCommentExcerpt verifies the excerpt is the first non-empty line, elided.
func TestCommentExcerpt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "one line", body: "done", want: "done"},
		{name: "first line wins", body: "summary\n\ndetails", want: "summary"},
		{name: "leading blank lines are skipped", body: "\n  \nsummary", want: "summary"},
		{name: "long line is elided", body: strings.Repeat("x", 60), want: strings.Repeat("x", 47) + "…"},
		{name: "blank", body: "  \n ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := commentExcerpt(tt.body, commentWidth); got != tt.want {
				t.Errorf("commentExcerpt = %q, want %q", got, tt.want)
			}
		})
	}
}
