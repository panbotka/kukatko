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

// TestKinds checks the registry: every kind exists, in display order, and an
// unknown kind is neither known nor offers any channel.
func TestKinds(t *testing.T) {
	t.Parallel()

	want := []Kind{KindTagged, KindRegistrationPending, KindAlbumFeatured, KindTaskAssigned, KindCommentMention}
	if got := Kinds(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Kinds() = %v, want %v", got, want)
	}
	for _, kind := range Kinds() {
		if !kind.Known() {
			t.Errorf("kind %q: Known = false, want true", kind)
		}
	}
	if Kind("nope").Known() || Kind("nope").Channels() != nil || Kind("nope").Supports(ChannelPush) {
		t.Error("an unknown kind reads as known or deliverable")
	}
}

// TestKindChannels pins every kind to all three channels, in display order
// (in-app, push, e-mail), and every default off — "nothing unless asked for"
// is the product decision, so a kind that defaulted on would be a regression.
func TestKindChannels(t *testing.T) {
	t.Parallel()

	want := []Channel{ChannelInApp, ChannelPush, ChannelEmail}
	if got := AllChannels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllChannels() = %v, want %v", got, want)
	}
	for _, kind := range Kinds() {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			if got := kind.Channels(); !reflect.DeepEqual(got, want) {
				t.Fatalf("Channels() = %v, want %v", got, want)
			}
			for _, channel := range want {
				if !kind.Supports(channel) || kind.Default(channel) {
					t.Errorf("%s: Supports=%v Default=%v, want true, false",
						channel, kind.Supports(channel), kind.Default(channel))
				}
			}
		})
	}
	if KindRegistrationPending.Supports("sms") || KindRegistrationPending.Default("sms") {
		t.Error("an unknown channel reads as supported")
	}
}

// TestKindChannelsReturnsFreshSlice checks a caller cannot rewrite a kind's
// channels in the registry, nor the channel list.
func TestKindChannelsReturnsFreshSlice(t *testing.T) {
	t.Parallel()

	channels := KindRegistrationPending.Channels()
	channels[0] = "mutated"
	if KindRegistrationPending.Channels()[0] != ChannelInApp {
		t.Fatal("Channels() shares its backing array with the registry")
	}
	all := AllChannels()
	all[0] = "mutated"
	if AllChannels()[0] != ChannelInApp {
		t.Fatal("AllChannels() shares its backing array")
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

	valid := New{
		ProjectUID: "pj1", ProjectSlug: "v", UserUID: "us1", Kind: KindTagged, Title: "Tagged",
		Link: "/p/v/photos?notification=nt1",
	}
	tests := []struct {
		name    string
		mutate  func(n *New)
		wantErr error
	}{
		{name: "valid", mutate: func(*New) {}},
		{name: "empty link is allowed", mutate: func(n *New) { n.Link = "" }},
		{name: "no project", mutate: func(n *New) { n.ProjectUID = "" }, wantErr: ErrNoProject},
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
		{
			name:    "self link without the project's slug",
			mutate:  func(n *New) { n.Link = ""; n.SelfLink = true; n.ProjectSlug = "" },
			wantErr: ErrInvalid,
		},
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

	n := New{
		ProjectUID: "pj1", UserUID: "us1", Kind: KindTagged, Title: "t",
		PhotoUIDs: []string{"b", "a", "", "b", "c", "a"},
	}
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

// TestValidatePrefs checks a replace refuses unknown kinds, channels a kind is
// not delivered through and repeated (kind, channel) pairs.
func TestValidatePrefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prefs   []Preference
		wantErr error
	}{
		{name: "empty", prefs: nil},
		{
			name: "every pair",
			prefs: []Preference{
				{Kind: KindTagged, Channel: ChannelPush},
				{Kind: KindRegistrationPending, Channel: ChannelPush, Enabled: true},
				{Kind: KindRegistrationPending, Channel: ChannelEmail, Enabled: true},
			},
		},
		{name: "unknown", prefs: []Preference{{Kind: "later", Channel: ChannelPush}}, wantErr: ErrUnknownKind},
		{
			name: "every channel of a kind",
			prefs: []Preference{
				{Kind: KindTagged, Channel: ChannelInApp}, {Kind: KindTagged, Channel: ChannelPush},
				{Kind: KindTagged, Channel: ChannelEmail},
			},
		},
		{
			name:    "unknown channel",
			prefs:   []Preference{{Kind: KindRegistrationPending, Channel: "sms"}},
			wantErr: ErrUnsupportedChannel,
		},
		{name: "no channel", prefs: []Preference{{Kind: KindTagged}}, wantErr: ErrUnsupportedChannel},
		{
			name: "same pair twice",
			prefs: []Preference{
				{Kind: KindTagged, Channel: ChannelPush, Enabled: true}, {Kind: KindTagged, Channel: ChannelPush},
			},
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

// TestEffective checks stored choices override defaults, missing pairs fall
// back, and a retired kind's or channel's row is ignored.
func TestEffective(t *testing.T) {
	t.Parallel()

	got := effective(map[prefKey]bool{
		{kind: KindTagged, channel: ChannelPush}:               false,
		{kind: KindRegistrationPending, channel: ChannelEmail}: true,
		{kind: "retired", channel: ChannelPush}:                true,
		{kind: KindTagged, channel: "sms"}:                     true,
		{kind: KindTagged, channel: ChannelInApp}:              true,
	})
	want := []Preference{
		{Kind: KindTagged, Channel: ChannelInApp, Enabled: true},
		{Kind: KindTagged, Channel: ChannelPush, Enabled: false},
		{Kind: KindTagged, Channel: ChannelEmail, Enabled: false, IsDefault: true},
		{Kind: KindRegistrationPending, Channel: ChannelInApp, Enabled: false, IsDefault: true},
		{Kind: KindRegistrationPending, Channel: ChannelPush, Enabled: false, IsDefault: true},
		{Kind: KindRegistrationPending, Channel: ChannelEmail, Enabled: true},
		{Kind: KindAlbumFeatured, Channel: ChannelInApp, Enabled: false, IsDefault: true},
		{Kind: KindAlbumFeatured, Channel: ChannelPush, Enabled: false, IsDefault: true},
		{Kind: KindAlbumFeatured, Channel: ChannelEmail, Enabled: false, IsDefault: true},
		{Kind: KindTaskAssigned, Channel: ChannelInApp, Enabled: false, IsDefault: true},
		{Kind: KindTaskAssigned, Channel: ChannelPush, Enabled: false, IsDefault: true},
		{Kind: KindTaskAssigned, Channel: ChannelEmail, Enabled: false, IsDefault: true},
		{Kind: KindCommentMention, Channel: ChannelInApp, Enabled: false, IsDefault: true},
		{Kind: KindCommentMention, Channel: ChannelPush, Enabled: false, IsDefault: true},
		{Kind: KindCommentMention, Channel: ChannelEmail, Enabled: false, IsDefault: true},
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
		[]Preference{
			{Kind: KindTagged, Channel: ChannelPush, Enabled: false},
			{Kind: KindRegistrationPending, Channel: ChannelEmail, Enabled: true},
		})
	if got.Action != audit.ActionNotificationPrefsUpdate || got.TargetType != "users" || got.TargetUID != "us1" {
		t.Fatalf("entry = %+v, want the prefs action targeting users/us1", got)
	}
	if got.ActorUID != "us-admin" || got.Details["via"] != "settings" {
		t.Fatalf("entry lost the caller's fields: %+v", got)
	}
	if !reflect.DeepEqual(got.Details["preferences"], map[string]bool{
		"tagged/push": false, "registration_pending/email": true,
	}) {
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

// TestPath checks a notification's own page lives under its project and that
// New stores it only when asked to.
func TestPath(t *testing.T) {
	t.Parallel()

	if got := Path("rodina", "ntabc"); got != "/p/rodina/n/ntabc" {
		t.Fatalf("Path = %q, want /p/rodina/n/ntabc", got)
	}
	if err := validateLink(Path("v", "ntabc")); err != nil {
		t.Fatalf("Path is not a valid in-app link: %v", err)
	}
	if got := (New{ProjectSlug: "v", SelfLink: true}).link("ntabc"); got != "/p/v/n/ntabc" {
		t.Errorf("self link = %q, want /p/v/n/ntabc", got)
	}
	if got := (New{ProjectSlug: "v", Link: "/p/v/users"}).link("ntabc"); got != "/p/v/users" {
		t.Errorf("plain link = %q, want /p/v/users", got)
	}
}
