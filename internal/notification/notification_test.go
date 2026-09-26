package notification

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
)

// TestKinds checks the registry: both kinds exist, in display order, default on,
// and an unknown kind is neither known nor wanted.
func TestKinds(t *testing.T) {
	t.Parallel()

	if got, want := Kinds(), []Kind{KindTagged, KindRegistrationPending}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Kinds() = %v, want %v", got, want)
	}
	for _, kind := range Kinds() {
		if !kind.Known() || !kind.Default() {
			t.Errorf("kind %q: Known=%v Default=%v, want both true", kind, kind.Known(), kind.Default())
		}
	}
	if Kind("nope").Known() || Kind("nope").Default() {
		t.Error("an unknown kind reads as known or wanted")
	}
}

// TestKindsReturnsFreshSlice checks a caller cannot rewrite the registry.
func TestKindsReturnsFreshSlice(t *testing.T) {
	t.Parallel()

	kinds := Kinds()
	kinds[0] = "mutated"
	if Kinds()[0] != KindTagged {
		t.Fatal("Kinds() shares its backing array with the registry")
	}
}

// TestNewValidate walks the field rules of a notification to record.
func TestNewValidate(t *testing.T) {
	t.Parallel()

	valid := New{UserUID: "us1", Kind: KindTagged, Title: "Tagged", Link: "/photos?notification=nt1"}
	tests := []struct {
		name    string
		mutate  func(n *New)
		wantErr error
	}{
		{name: "valid", mutate: func(*New) {}},
		{name: "empty link is allowed", mutate: func(n *New) { n.Link = "" }},
		{name: "no account", mutate: func(n *New) { n.UserUID = " " }, wantErr: ErrInvalid},
		{name: "unknown kind", mutate: func(n *New) { n.Kind = "later" }, wantErr: ErrUnknownKind},
		{name: "blank title", mutate: func(n *New) { n.Title = "  " }, wantErr: ErrInvalid},
		{
			name: "title at the limit", mutate: func(n *New) { n.Title = strings.Repeat("č", MaxTitleLen) },
		},
		{
			name:    "title over the limit",
			mutate:  func(n *New) { n.Title = strings.Repeat("č", MaxTitleLen+1) },
			wantErr: ErrInvalid,
		},
		{
			name:    "body over the limit",
			mutate:  func(n *New) { n.Body = strings.Repeat("a", MaxBodyLen+1) },
			wantErr: ErrInvalid,
		},
		{name: "self link", mutate: func(n *New) { n.Link = ""; n.SelfLink = true }},
		{name: "self link and a link", mutate: func(n *New) { n.SelfLink = true }, wantErr: ErrInvalid},
		{name: "absolute URL", mutate: func(n *New) { n.Link = "https://evil.test/" }, wantErr: ErrInvalid},
		{name: "scheme-relative", mutate: func(n *New) { n.Link = "//evil.test/" }, wantErr: ErrInvalid},
		{name: "backslash host", mutate: func(n *New) { n.Link = `/\evil.test` }, wantErr: ErrInvalid},
		{
			name:    "link over the limit",
			mutate:  func(n *New) { n.Link = "/" + strings.Repeat("a", MaxLinkLen) },
			wantErr: ErrInvalid,
		},
		{
			name: "too many photos",
			mutate: func(n *New) {
				n.PhotoUIDs = make([]string, MaxPhotos+1)
				for i := range n.PhotoUIDs {
					n.PhotoUIDs[i] = "ph" + strconv.Itoa(i)
				}
			},
			wantErr: ErrTooManyPhotos,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n := valid
			tt.mutate(&n)
			_, err := n.validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestNewValidateDedupesPhotos checks a repeated photograph keeps its first
// position and an empty uid is dropped.
func TestNewValidateDedupesPhotos(t *testing.T) {
	t.Parallel()

	n := New{UserUID: "us1", Kind: KindTagged, Title: "t", PhotoUIDs: []string{"b", "a", "", "b", "c", "a"}}
	got, err := n.validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if want := []string{"b", "a", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("photos = %v, want %v", got, want)
	}
}

// TestRetentionValidate checks the purge thresholds.
func TestRetentionValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		r    Retention
		ok   bool
	}{
		{name: "default", r: DefaultRetention(), ok: true},
		{name: "equal thresholds", r: Retention{Read: time.Hour, Unread: time.Hour}, ok: true},
		{name: "unread shorter than read", r: Retention{Read: 2 * time.Hour, Unread: time.Hour}},
		{name: "zero read", r: Retention{Unread: time.Hour}},
		{name: "negative unread", r: Retention{Read: time.Hour, Unread: -time.Hour}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.r.validate()
			if tt.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidRetention)) {
				t.Fatalf("validate(%+v) = %v, want ok=%v", tt.r, err, tt.ok)
			}
		})
	}
}

// TestValidatePrefs checks a replace refuses unknown and repeated kinds.
func TestValidatePrefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prefs   []Preference
		wantErr error
	}{
		{name: "empty", prefs: nil},
		{name: "both kinds", prefs: []Preference{{Kind: KindTagged}, {Kind: KindRegistrationPending, Enabled: true}}},
		{name: "unknown", prefs: []Preference{{Kind: "later"}}, wantErr: ErrUnknownKind},
		{
			name:    "same kind twice",
			prefs:   []Preference{{Kind: KindTagged, Enabled: true}, {Kind: KindTagged}},
			wantErr: ErrDuplicateKind,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := validatePrefs(tt.prefs); !errors.Is(err, tt.wantErr) {
				t.Fatalf("validatePrefs() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestEffective checks stored choices override defaults, missing kinds fall
// back, and a retired kind's row is ignored.
func TestEffective(t *testing.T) {
	t.Parallel()

	got := effective(map[Kind]bool{KindTagged: false, "retired": true})
	want := []Preference{
		{Kind: KindTagged, Enabled: false},
		{Kind: KindRegistrationPending, Enabled: true, IsDefault: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("effective() = %+v, want %+v", got, want)
	}
}

// TestPrefsEntry checks the audit entry defaults and that the caller's details
// map is not written into.
func TestPrefsEntry(t *testing.T) {
	t.Parallel()

	callerDetails := map[string]any{"via": "settings"}
	got := prefsEntry(audit.Entry{ActorUID: "us-admin", Details: callerDetails}, "us1",
		[]Preference{{Kind: KindTagged, Enabled: false}})
	if got.Action != audit.ActionNotificationPrefsUpdate || got.TargetType != "users" || got.TargetUID != "us1" {
		t.Fatalf("entry = %+v, want the prefs action targeting users/us1", got)
	}
	if got.ActorUID != "us-admin" || got.Details["via"] != "settings" {
		t.Fatalf("entry lost the caller's fields: %+v", got)
	}
	if !reflect.DeepEqual(got.Details["preferences"], map[string]bool{"tagged": false}) {
		t.Fatalf("details.preferences = %v", got.Details["preferences"])
	}
	if _, leaked := callerDetails["preferences"]; leaked {
		t.Fatal("prefsEntry wrote into the caller's details map")
	}

	kept := prefsEntry(audit.Entry{Action: "x", TargetType: "t", TargetUID: "u"}, "us1", nil)
	if kept.Action != "x" || kept.TargetType != "t" || kept.TargetUID != "u" {
		t.Fatalf("prefsEntry overwrote set fields: %+v", kept)
	}
}

// TestPath checks a notification's own page and that New stores it only when
// asked to.
func TestPath(t *testing.T) {
	t.Parallel()

	if got := Path("ntabc"); got != "/n/ntabc" {
		t.Fatalf("Path = %q, want /n/ntabc", got)
	}
	if err := validateLink(Path("ntabc")); err != nil {
		t.Fatalf("Path is not a valid in-app link: %v", err)
	}
	if got := (New{SelfLink: true}).link("ntabc"); got != "/n/ntabc" {
		t.Errorf("self link = %q, want /n/ntabc", got)
	}
	if got := (New{Link: "/users"}).link("ntabc"); got != "/users" {
		t.Errorf("plain link = %q, want /users", got)
	}
}
