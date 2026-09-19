package phototaskapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/phototask"
)

// TestParseFilter verifies a listing's parameters reach the store filter, and
// that an unknown state is refused rather than quietly matching nothing.
func TestParseFilter(t *testing.T) {
	t.Parallel()

	got, err := parseFilter(url.Values{
		"state":    {"question,working", "review"},
		"open":     {"true"},
		"answered": {"1"},
		"q":        {"  dům  "},
		"photo":    {"ph1"},
		"limit":    {"10"},
		"offset":   {"20"},
	}, "us-caller")
	if err != nil {
		t.Fatalf("parseFilter: %v", err)
	}
	if len(got.States) != 3 || got.States[0] != phototask.StateQuestion {
		t.Errorf("states = %v, want the three parsed from both parameters", got.States)
	}
	if !got.Open || !got.Answered {
		t.Errorf("flags = open %v, answered %v, want both true", got.Open, got.Answered)
	}
	if got.Search != "dům" || got.PhotoUID != "ph1" {
		t.Errorf("search = %q, photo = %q", got.Search, got.PhotoUID)
	}
	if got.Limit != 10 || got.Offset != 20 {
		t.Errorf("paging = %d/%d, want 10/20", got.Limit, got.Offset)
	}

	if _, err := parseFilter(url.Values{"state": {"parked"}}, "us-caller"); err == nil {
		t.Error("parseFilter(unknown state) = nil error, want a refusal")
	}
	if _, err := parseFilter(url.Values{"limit": {"-1"}}, "us-caller"); err == nil {
		t.Error("parseFilter(negative limit) = nil error, want a refusal")
	}
	if _, err := parseFilter(url.Values{"offset": {"x"}}, "us-caller"); err == nil {
		t.Error("parseFilter(non-numeric offset) = nil error, want a refusal")
	}
	if empty, err := parseFilter(url.Values{}, "us-caller"); err != nil || empty.Limit != 0 {
		t.Errorf("parseFilter(empty) = %+v, %v, want the zero filter", empty, err)
	}
}

// TestParseFilterParticipant verifies the "what am I on?" filter, including the
// "me" alias — the form it is almost always asked in, so that a client need not
// know its own uid to ask the question.
func TestParseFilterParticipant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "absent", value: "", want: ""},
		{name: "me resolves to the caller", value: "me", want: "us-caller"},
		{name: "padded me still resolves", value: "  me  ", want: "us-caller"},
		{name: "an explicit uid passes through", value: "us-other", want: "us-other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			values := url.Values{}
			if tt.value != "" {
				values.Set("participant", tt.value)
			}
			got, err := parseFilter(values, "us-caller")
			if err != nil {
				t.Fatalf("parseFilter: %v", err)
			}
			if got.ParticipantUID != tt.want {
				t.Errorf("ParticipantUID = %q, want %q", got.ParticipantUID, tt.want)
			}
		})
	}
}

// TestParseBool verifies the affirmatives a flag parameter accepts.
func TestParseBool(t *testing.T) {
	t.Parallel()

	for _, yes := range []string{"1", "true", "TRUE", " yes "} {
		if !parseBool(yes) {
			t.Errorf("parseBool(%q) = false, want true", yes)
		}
	}
	for _, no := range []string{"", "0", "false", "maybe"} {
		if parseBool(no) {
			t.Errorf("parseBool(%q) = true, want false", no)
		}
	}
}

// TestDecodeMembership verifies a membership request must name photographs, and
// not more than the store would accept.
func TestDecodeMembership(t *testing.T) {
	t.Parallel()

	got, err := decodeMembership(postJSON(`{"photo_uids":["ph1","ph2"]}`))
	if err != nil || len(got) != 2 {
		t.Fatalf("decodeMembership = %v, %v, want two uids", got, err)
	}
	if _, err := decodeMembership(postJSON(`{"photo_uids":[]}`)); err == nil {
		t.Error("an empty list was accepted, want a refusal")
	}
	if _, err := decodeMembership(postJSON(`{"nope":1}`)); err == nil {
		t.Error("an unknown field was accepted, want a refusal")
	}
	many := `{"photo_uids":[` + strings.TrimSuffix(strings.Repeat(`"ph",`, maxPhotoUIDs+1), ",") + `]}`
	if _, err := decodeMembership(postJSON(many)); err == nil {
		t.Error("an over-long list was accepted, want a refusal")
	}
}

// postJSON returns a request carrying body, for the decoders under test.
func postJSON(body string) *http.Request {
	return httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/", strings.NewReader(body))
}

// TestUpdateRequestToUpdate verifies an absent field stays absent, an explicit
// empty string is carried through as a clear, and the state is passed to the
// store rather than validated twice.
func TestUpdateRequestToUpdate(t *testing.T) {
	t.Parallel()

	empty := updateRequest{}.toUpdate()
	if empty.Title != nil || empty.State != nil || empty.Resolution != nil {
		t.Errorf("an empty request produced %+v, want every field absent", empty)
	}

	blank := ""
	parked := "parked"
	got := updateRequest{Resolution: &blank, State: &parked}.toUpdate()
	if got.Resolution == nil || *got.Resolution != "" {
		t.Errorf("resolution = %v, want an explicit empty string", got.Resolution)
	}
	if got.State == nil || *got.State != phototask.State("parked") {
		t.Errorf("state = %v, want the value carried through unvalidated", got.State)
	}
	if got.Options != nil {
		t.Errorf("options = %v, want absent when the request did not name them", got.Options)
	}

	cleared := updateRequest{Options: &[]string{}}.toUpdate()
	if cleared.Options == nil || len(*cleared.Options) != 0 {
		t.Errorf("options = %v, want an explicit empty set", cleared.Options)
	}
}

// TestCommentPolicy verifies who may rewrite and who may remove a comment.
func TestCommentPolicy(t *testing.T) {
	t.Parallel()

	author := auth.User{UID: "us1", Role: auth.RoleViewer}
	stranger := auth.User{UID: "us2", Role: auth.RoleEditor}
	admin := auth.User{UID: "us3", Role: auth.RoleAdmin}
	mine := comments.Comment{AuthorUID: "us1"}
	orphan := comments.Comment{}

	tests := []struct {
		name      string
		comment   comments.Comment
		user      auth.User
		canEdit   bool
		canDelete bool
	}{
		{name: "author", comment: mine, user: author, canEdit: true, canDelete: true},
		{name: "a stranger with write access", comment: mine, user: stranger},
		{name: "an admin may delete but not edit", comment: mine, user: admin, canDelete: true},
		{name: "an authorless comment", comment: orphan, user: author},
		{name: "an authorless comment and an admin", comment: orphan, user: admin, canDelete: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := canEditComment(tt.comment, tt.user); got != tt.canEdit {
				t.Errorf("canEditComment = %v, want %v", got, tt.canEdit)
			}
			if got := canDeleteComment(tt.comment, tt.user); got != tt.canDelete {
				t.Errorf("canDeleteComment = %v, want %v", got, tt.canDelete)
			}
		})
	}
}

// TestWriteTaskError verifies each store error becomes the status a client can
// act on: a missing thing is 404, anything the caller could fix is 400.
func TestWriteTaskError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "missing task", err: phototask.ErrNotFound, want: http.StatusNotFound},
		{name: "missing photo", err: phototask.ErrPhotoNotFound, want: http.StatusNotFound},
		{name: "no title", err: phototask.ErrEmptyTitle, want: http.StatusBadRequest},
		{name: "too long", err: phototask.ErrTooLong, want: http.StatusBadRequest},
		{name: "unknown state", err: phototask.ErrInvalidState, want: http.StatusBadRequest},
		{
			name: "closing without a resolution",
			err:  phototask.ErrClosedNeedsResolution,
			want: http.StatusBadRequest,
		},
		{name: "too many photos", err: phototask.ErrTooManyPhotos, want: http.StatusBadRequest},
		{name: "too many options", err: phototask.ErrTooManyOptions, want: http.StatusBadRequest},
		{name: "blank option", err: phototask.ErrEmptyOption, want: http.StatusBadRequest},
		{name: "duplicate option", err: phototask.ErrDuplicateOption, want: http.StatusBadRequest},
		{name: "anything else", err: errors.New("boom"), want: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			writeTaskError(rec, tt.err, "failed")
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

// TestWriteCommentError verifies the thread's own error mapping, including that a
// missing subject reads as a missing task rather than a missing photograph.
func TestWriteCommentError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "missing task", err: comments.ErrSubjectNotFound, want: http.StatusNotFound},
		{name: "missing comment", err: comments.ErrNotFound, want: http.StatusNotFound},
		{name: "empty body", err: comments.ErrEmptyBody, want: http.StatusBadRequest},
		{name: "over-long body", err: comments.ErrBodyTooLong, want: http.StatusBadRequest},
		{name: "anything else", err: errors.New("boom"), want: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			writeCommentError(rec, tt.err, "failed")
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

// TestCommentsUnavailable verifies the thread endpoints answer 503 without a
// comment store, while the task endpoints are unaffected.
func TestCommentsUnavailable(t *testing.T) {
	t.Parallel()

	api := NewAPI(Config{})
	rec := httptest.NewRecorder()
	api.handleListComments(rec,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
