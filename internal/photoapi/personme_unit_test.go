package photoapi

import (
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
)

// userLinkedTo builds a caller whose account names subjectUID as the person it
// is; an empty subjectUID builds an unlinked one.
func userLinkedTo(subjectUID string) auth.User {
	u := auth.User{UID: "usr1"}
	if subjectUID != "" {
		u.SubjectUID = &subjectUID
	}
	return u
}

// personTexts returns the value texts of the params' person filters, which is
// what the store compiles into SQL.
func personTexts(params photos.ListParams) []string {
	var out []string
	for _, f := range params.QueryFilters {
		if f.Key != query.KeyPerson {
			continue
		}
		for _, v := range f.Values {
			out = append(out, v.Text)
		}
	}
	return out
}

func TestApplyMeTokens_person(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		q           string
		subjectUID  string
		wantNotices []string
		wantNone    bool
		wantTexts   []string
	}{
		{
			name:       "a linked caller is filtered to their own person",
			q:          "person:me",
			subjectUID: "sub123",
			wantTexts:  []string{"sub123"},
		},
		{
			name:        "an unlinked caller gets nothing, and is told why",
			q:           "person:me",
			wantNotices: []string{noticePersonMeUnlinked},
			wantNone:    true,
			wantTexts:   []string{"me"},
		},
		{
			name:       "a query without the token is untouched",
			q:          "person:babicka year:1998",
			subjectUID: "sub123",
			wantTexts:  []string{"babicka"},
		},
		{
			name:      "an unlinked caller who never asked still sees the library",
			q:         "dovolená",
			wantTexts: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params := photos.ListParams{QueryFilters: query.Parse(tt.q).Filters}

			notices := applyMeTokens(&params, userLinkedTo(tt.subjectUID))

			if len(notices) != len(tt.wantNotices) {
				t.Fatalf("applyMeTokens(%q) notices = %v, want %v", tt.q, notices, tt.wantNotices)
			}
			for i, want := range tt.wantNotices {
				if notices[i] != want {
					t.Errorf("applyMeTokens(%q) notice %d = %q, want %q", tt.q, i, notices[i], want)
				}
			}
			if params.MatchNone != tt.wantNone {
				t.Errorf("applyMeTokens(%q) MatchNone = %v, want %v", tt.q, params.MatchNone, tt.wantNone)
			}
			got := personTexts(params)
			if len(got) != len(tt.wantTexts) {
				t.Fatalf("applyMeTokens(%q) person values = %v, want %v", tt.q, got, tt.wantTexts)
			}
			for i, want := range tt.wantTexts {
				if got[i] != want {
					t.Errorf("applyMeTokens(%q) person value %d = %q, want %q", tt.q, i, got[i], want)
				}
			}
		})
	}
}

// uploaderTexts returns the value texts of the params' uploader filters, which
// is what the store compiles into SQL.
func uploaderTexts(params photos.ListParams) []string {
	var out []string
	for _, f := range params.QueryFilters {
		if f.Key != query.KeyUploader {
			continue
		}
		for _, v := range f.Values {
			out = append(out, v.Text)
		}
	}
	return out
}

// TestApplyMeTokens_uploader covers the second token every handler resolves:
// `uploader:me` becomes the caller's own account UID, cannot leave the request
// unsatisfiable (no notice, no MatchNone), and leaves `none` — the store's word,
// not the caller's — untouched.
func TestApplyMeTokens_uploader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		q         string
		wantTexts []string
	}{
		{
			name:      "the caller is filtered to their own uploads",
			q:         "uploader:me",
			wantTexts: []string{"usr1"},
		},
		{
			name:      "the imported group is left to the store",
			q:         "uploader:none",
			wantTexts: []string{"none"},
		},
		{
			name:      "a named uploader is untouched",
			q:         "uploader:tomas album:x",
			wantTexts: []string{"tomas"},
		},
		{
			name:      "both tokens resolve in one query",
			q:         "uploader:me person:me",
			wantTexts: []string{"usr1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params := photos.ListParams{QueryFilters: query.Parse(tt.q).Filters}

			notices := applyMeTokens(&params, userLinkedTo("sub123"))

			if len(notices) != 0 {
				t.Errorf("applyMeTokens(%q) notices = %v, want none", tt.q, notices)
			}
			if params.MatchNone {
				t.Errorf("applyMeTokens(%q) MatchNone = true, want false", tt.q)
			}
			got := uploaderTexts(params)
			if len(got) != len(tt.wantTexts) {
				t.Fatalf("applyMeTokens(%q) uploader values = %v, want %v", tt.q, got, tt.wantTexts)
			}
			for i, want := range tt.wantTexts {
				if got[i] != want {
					t.Errorf("applyMeTokens(%q) uploader value %d = %q, want %q", tt.q, i, got[i], want)
				}
			}
		})
	}
}
