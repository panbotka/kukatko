package auditapi

import (
	"net/url"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
)

// TestParseFilter verifies query parameters map onto an audit.Filter and that
// malformed timestamps and pagination values are rejected.
func TestParseFilter(t *testing.T) {
	t.Parallel()

	t.Run("maps recognised parameters", func(t *testing.T) {
		t.Parallel()
		q := url.Values{
			"user":        {"us-1"},
			"entity_type": {"photos"},
			"entity_uid":  {"ph-1"},
			"action":      {audit.ActionPhotoUpdate},
			"since":       {"2026-01-01T00:00:00Z"},
			"until":       {"2026-12-31T23:59:59Z"},
			"limit":       {"25"},
			"offset":      {"50"},
		}
		filter, err := parseFilter(q)
		if err != nil {
			t.Fatalf("parseFilter() error = %v, want nil", err)
		}
		if filter.ActorUID != "us-1" || filter.TargetType != "photos" || filter.TargetUID != "ph-1" {
			t.Errorf("identity filters = %+v, want us-1/photos/ph-1", filter)
		}
		if filter.Action != audit.ActionPhotoUpdate || filter.Limit != 25 || filter.Offset != 50 {
			t.Errorf("action/paging = %q/%d/%d, want %s/25/50", filter.Action, filter.Limit, filter.Offset, audit.ActionPhotoUpdate)
		}
		wantSince := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		if filter.Since == nil || !filter.Since.Equal(wantSince) {
			t.Errorf("since = %v, want %v", filter.Since, wantSince)
		}
		if filter.Until == nil {
			t.Errorf("until = nil, want a time")
		}
	})

	t.Run("maps the review and decision filters", func(t *testing.T) {
		t.Parallel()
		yes, err := parseFilter(url.Values{"via": {"review"}, "decision": {"yes"}})
		if err != nil {
			t.Fatalf("parseFilter(via=review,decision=yes) error = %v, want nil", err)
		}
		if !yes.ReviewOnly {
			t.Errorf("ReviewOnly = false, want true")
		}
		wantYes := []string{audit.ActionFaceAssign, audit.ActionLabelAttach}
		if len(yes.Actions) != 2 || yes.Actions[0] != wantYes[0] || yes.Actions[1] != wantYes[1] {
			t.Errorf("Actions = %v, want %v", yes.Actions, wantYes)
		}

		no, err := parseFilter(url.Values{"decision": {"no"}})
		if err != nil {
			t.Fatalf("parseFilter(decision=no) error = %v, want nil", err)
		}
		wantNo := []string{audit.ActionFaceReject, audit.ActionLabelReject}
		if no.ReviewOnly {
			t.Errorf("ReviewOnly = true, want false without via")
		}
		if len(no.Actions) != 2 || no.Actions[0] != wantNo[0] || no.Actions[1] != wantNo[1] {
			t.Errorf("Actions = %v, want %v", no.Actions, wantNo)
		}
	})

	t.Run("rejects bad values", func(t *testing.T) {
		t.Parallel()
		bad := []url.Values{
			{"since": {"yesterday"}},
			{"until": {"soon"}},
			{"limit": {"-1"}},
			{"limit": {"abc"}},
			{"offset": {"-5"}},
			{"via": {"import"}},
			{"decision": {"maybe"}},
		}
		for _, q := range bad {
			if _, err := parseFilter(q); err == nil {
				t.Errorf("parseFilter(%v) error = nil, want error", q)
			}
		}
	})
}

// TestBuildResponse verifies the effective limit clamps and next_offset is set
// only when more rows follow the current page.
func TestBuildResponse(t *testing.T) {
	t.Parallel()

	records := func(n int) []audit.Record {
		out := make([]audit.Record, n)
		return out
	}

	tests := []struct {
		name        string
		filter      audit.Filter
		entries     []audit.Record
		total       int
		wantLim     int
		wantHasNext bool
		wantNext    int
	}{
		{name: "more pages sets next", filter: audit.Filter{Limit: 2, Offset: 0}, entries: records(2), total: 5, wantLim: 2, wantHasNext: true, wantNext: 2},
		{name: "last page no next", filter: audit.Filter{Limit: 2, Offset: 4}, entries: records(1), total: 5, wantLim: 2},
		{name: "zero limit clamps to default", filter: audit.Filter{}, entries: records(0), total: 0, wantLim: defaultLimit},
		{name: "oversize limit clamps", filter: audit.Filter{Limit: maxLimit + 1}, entries: records(0), total: 0, wantLim: defaultLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := buildResponse(tt.filter, tt.entries, tt.total)
			if resp.Limit != tt.wantLim {
				t.Errorf("Limit = %d, want %d", resp.Limit, tt.wantLim)
			}
			if (resp.NextOffset != nil) != tt.wantHasNext {
				t.Fatalf("NextOffset present = %v, want %v", resp.NextOffset != nil, tt.wantHasNext)
			}
			if tt.wantHasNext && *resp.NextOffset != tt.wantNext {
				t.Errorf("NextOffset = %d, want %d", *resp.NextOffset, tt.wantNext)
			}
		})
	}
}
