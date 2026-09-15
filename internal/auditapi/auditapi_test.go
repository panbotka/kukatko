package auditapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// testAccounts is the account set the filter tests resolve against. Both an
// account's username and its UID map onto the UID, exactly as the real lookup
// in cmd/kukatko does.
var testAccounts = map[string]string{
	"panbotka": "us-1", "us-1": "us-1",
	"other": "us-2", "us-2": "us-2",
}

// fakeResolver resolves the user filter from an accounts map, standing in for
// the account store so the filter parsing can be tested without a database.
func fakeResolver(accounts map[string]string) ResolveUser {
	return func(_ context.Context, value string) (string, error) {
		if uid, ok := accounts[value]; ok {
			return uid, nil
		}
		return "", ErrUnknownUser
	}
}

// testResolver resolves against testAccounts.
var testResolver = fakeResolver(testAccounts)

// parse runs parseFilter over q with the test account set.
func parse(t *testing.T, q url.Values) (audit.Filter, error) {
	t.Helper()
	return parseFilter(t.Context(), q, testResolver)
}

// TestListMineWithoutPrincipal verifies the own-activity handler fails closed:
// a request that reaches it without an authenticated user — a guard wired wrong —
// is refused rather than served an unnarrowed listing of everybody's actions.
func TestListMineWithoutPrincipal(t *testing.T) {
	t.Parallel()
	passthrough := func(next http.Handler) http.Handler { return next }
	// A nil store is safe here precisely because the handler must not reach it.
	api := NewAPI(Config{RequireAdmin: passthrough, RequireAuth: passthrough})
	router := chi.NewRouter()
	api.RegisterRoutes(router)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/audit/mine", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

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
		filter, err := parse(t, q)
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
		yes, err := parse(t, url.Values{"via": {"review"}, "decision": {"yes"}})
		if err != nil {
			t.Fatalf("parseFilter(via=review,decision=yes) error = %v, want nil", err)
		}
		if !yes.ReviewOnly {
			t.Errorf("ReviewOnly = false, want true")
		}
		// The bucket is audit's own list, so a new review question type becomes
		// filterable here the moment it is answerable — asserting a hardcoded copy
		// would only pin this test to yesterday's set.
		wantYes := audit.ReviewYesActions()
		if !slices.Equal(yes.Actions, wantYes) {
			t.Errorf("Actions = %v, want %v", yes.Actions, wantYes)
		}
		if !slices.Contains(yes.Actions, audit.ActionFaceAssign) ||
			!slices.Contains(yes.Actions, audit.ActionLabelAttach) {
			t.Errorf("Actions = %v, want it to include the face assign and label attach", yes.Actions)
		}

		no, err := parse(t, url.Values{"decision": {"no"}})
		if err != nil {
			t.Fatalf("parseFilter(decision=no) error = %v, want nil", err)
		}
		wantNo := audit.ReviewNoActions()
		if no.ReviewOnly {
			t.Errorf("ReviewOnly = true, want false without via")
		}
		if !slices.Equal(no.Actions, wantNo) {
			t.Errorf("Actions = %v, want %v", no.Actions, wantNo)
		}
		if slices.ContainsFunc(no.Actions, func(a string) bool {
			return slices.Contains(audit.ReviewYesActions(), a)
		}) {
			t.Errorf("Actions = %v, want the two buckets disjoint", no.Actions)
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
			if _, err := parse(t, q); err == nil {
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

// TestParseFilterRejectsNonsense verifies the three values that used to be taken
// verbatim are now refused, and that each refusal names what was wrong — the
// whole point of the change, since an accepted typo answers with an empty list
// that reads as "this person did nothing".
func TestParseFilterRejectsNonsense(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query url.Values
		want  string
	}{
		{name: "misspelled action", query: url.Values{"action": {"photo.updat"}}, want: "photo.updat"},
		{name: "invented action", query: url.Values{"action": {"nonsense"}}, want: "nonsense"},
		{name: "unknown user", query: url.Values{"user": {"nobody"}}, want: "nobody"},
		{name: "unknown parameter", query: url.Values{"actor": {"us-1"}}, want: "actor"},
		{name: "near-miss parameter", query: url.Values{"from": {"2026-01-01T00:00:00Z"}}, want: "from"},
		{name: "empty unknown parameter", query: url.Values{"actor": {""}}, want: "actor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parse(t, tt.query)
			if err == nil {
				t.Fatalf("parseFilter(%v) error = nil, want an error", tt.query)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to name %q", err, tt.want)
			}
			if errors.Is(err, errUserLookup) {
				t.Errorf("error = %q, want a caller error, not a lookup failure", err)
			}
		})
	}
}

// TestParseFilterAcceptsUsername verifies a username and the account's UID
// produce the same filter, so filtering by the name a human knows works.
func TestParseFilterAcceptsUsername(t *testing.T) {
	t.Parallel()

	byName, err := parse(t, url.Values{"user": {"panbotka"}})
	if err != nil {
		t.Fatalf("parseFilter(user=panbotka) error = %v, want nil", err)
	}
	byUID, err := parse(t, url.Values{"user": {"us-1"}})
	if err != nil {
		t.Fatalf("parseFilter(user=us-1) error = %v, want nil", err)
	}
	if byName.ActorUID != "us-1" || byUID.ActorUID != "us-1" {
		t.Errorf("actor UIDs = %q / %q, want us-1 for both", byName.ActorUID, byUID.ActorUID)
	}
}

// TestParseFilterLeavesEntityAlone verifies the two deliberate exceptions: the
// trail outlives what it describes, so an entity UID that no longer resolves and
// an entity type nobody has heard of must both pass through untouched.
func TestParseFilterLeavesEntityAlone(t *testing.T) {
	t.Parallel()

	filter, err := parse(t, url.Values{
		"entity_uid":  {"ph-purged-long-ago"},
		"entity_type": {"a_kind_invented_tomorrow"},
	})
	if err != nil {
		t.Fatalf("parseFilter() error = %v, want nil", err)
	}
	if filter.TargetUID != "ph-purged-long-ago" || filter.TargetType != "a_kind_invented_tomorrow" {
		t.Errorf("entity filters = %q/%q, want them passed through", filter.TargetUID, filter.TargetType)
	}
}

// TestParseFilterAcceptsTheFrontendQuery verifies every parameter the two audit
// pages send survives the allow-list. The keys are the ones buildAuditQuery in
// web/src/services/audit.ts writes; rejecting unknown keys is the change most
// likely to break a caller, so the caller we have is pinned here.
func TestParseFilterAcceptsTheFrontendQuery(t *testing.T) {
	t.Parallel()

	// GET /audit — everything the admin listing can send at once.
	admin := url.Values{
		"user":        {"panbotka"},
		"action":      {audit.ActionPhotoUpdate},
		"entity_type": {"photos"},
		"entity_uid":  {"ph-1"},
		"via":         {"review"},
		"decision":    {"yes"},
		"since":       {"2026-01-01T00:00:00Z"},
		"until":       {"2026-12-31T23:59:59Z"},
		"limit":       {"100"},
		"offset":      {"100"},
	}
	if _, err := parse(t, admin); err != nil {
		t.Errorf("parseFilter(admin page query) error = %v, want nil", err)
	}

	// GET /audit/mine — the same minus user, which that endpoint never sends.
	mine := url.Values{}
	for key, values := range admin {
		if key != "user" {
			mine[key] = values
		}
	}
	if _, err := parse(t, mine); err != nil {
		t.Errorf("parseFilter(my-activity query) error = %v, want nil", err)
	}

	// And the empty query both pages open with.
	if _, err := parse(t, url.Values{}); err != nil {
		t.Errorf("parseFilter(no filters) error = %v, want nil", err)
	}
}

// TestResolveActorFailures verifies the two ways the lookup can go wrong are
// told apart: a value naming no account is the caller's mistake, an unreachable
// or unconfigured store is the server's and must not be reported as a bad filter.
func TestResolveActorFailures(t *testing.T) {
	t.Parallel()

	broken := func(_ context.Context, _ string) (string, error) {
		return "", errors.New("connection refused")
	}
	if _, err := parseFilter(t.Context(), url.Values{"user": {"panbotka"}}, broken); !errors.Is(err, errUserLookup) {
		t.Errorf("error for a failing lookup = %v, want it to wrap errUserLookup", err)
	}
	if _, err := parseFilter(t.Context(), url.Values{"user": {"panbotka"}}, nil); !errors.Is(err, errUserLookup) {
		t.Errorf("error for an unconfigured lookup = %v, want it to wrap errUserLookup", err)
	}
	// No user filter, no lookup needed — an unconfigured resolver is not an error.
	if _, err := parseFilter(t.Context(), url.Values{"limit": {"10"}}, nil); err != nil {
		t.Errorf("error without a user filter = %v, want nil", err)
	}
}

// TestWriteFilterError verifies a lookup failure is a 500 and everything else a
// 400 carrying the parser's own message.
func TestWriteFilterError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeFilterError(rec, errors.New(`unknown action "photo.updat"`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status for a bad value = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "photo.updat") {
		t.Errorf("body = %q, want it to name the offending value", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	writeFilterError(rec, fmt.Errorf("%w: down", errUserLookup))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status for a lookup failure = %d, want 500", rec.Code)
	}
}
