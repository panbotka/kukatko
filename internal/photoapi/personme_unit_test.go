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

func TestApplyPersonMe(t *testing.T) {
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

			notices := applyPersonMe(&params, userLinkedTo(tt.subjectUID))

			if len(notices) != len(tt.wantNotices) {
				t.Fatalf("applyPersonMe(%q) notices = %v, want %v", tt.q, notices, tt.wantNotices)
			}
			for i, want := range tt.wantNotices {
				if notices[i] != want {
					t.Errorf("applyPersonMe(%q) notice %d = %q, want %q", tt.q, i, notices[i], want)
				}
			}
			if params.MatchNone != tt.wantNone {
				t.Errorf("applyPersonMe(%q) MatchNone = %v, want %v", tt.q, params.MatchNone, tt.wantNone)
			}
			got := personTexts(params)
			if len(got) != len(tt.wantTexts) {
				t.Fatalf("applyPersonMe(%q) person values = %v, want %v", tt.q, got, tt.wantTexts)
			}
			for i, want := range tt.wantTexts {
				if got[i] != want {
					t.Errorf("applyPersonMe(%q) person value %d = %q, want %q", tt.q, i, got[i], want)
				}
			}
		})
	}
}
