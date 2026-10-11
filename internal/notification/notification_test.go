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

// TestKinds checks the registry: both kinds exist, in display order, and an
// unknown kind is neither known nor offers any channel.
func TestKinds(t *testing.T) {
	t.Parallel()

	if got, want := Kinds(), []Kind{KindTagged, KindRegistrationPending}; !reflect.DeepEqual(got, want) {
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

// TestKindChannels pins each kind's channels, their order (push before e-mail)
// and their defaults — above all that the administrators' registration e-mail is
// opt-in.
func TestKindChannels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind     Kind
		channels []Channel
		defaults map[Channel]bool
	}{
		{kind: KindTagged, channels: []Channel{ChannelPush}, defaults: map[Channel]bool{ChannelPush: true}},
		{
			kind:     KindRegistrationPending,
			channels: []Channel{ChannelPush, ChannelEmail},
			defaults: map[Channel]bool{ChannelPush: true, ChannelEmail: false},
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Parallel()
			if got := tt.kind.Channels(); !reflect.DeepEqual(got, tt.channels) {
				t.Fatalf("Channels() = %v, want %v", got, tt.channels)
			}
			for channel, def := range tt.defaults {
				if !tt.kind.Supports(channel) || tt.kind.Default(channel) != def {
					t.Errorf("%s: Supports=%v Default=%v, want true, %v",
						channel, tt.kind.Supports(channel), tt.kind.Default(channel), def)
				}
			}
		})
	}
	if KindTagged.Supports(ChannelEmail) || KindTagged.Default(ChannelEmail) {
		t.Error("tagged reads as deliverable by e-mail")
	}
	if KindRegistrationPending.Supports("sms") {
		t.Error("an unknown channel reads as supported")
	}
}

// TestKindChannelsReturnsFreshSlice checks a caller cannot rewrite a kind's
// channels in the registry.
func TestKindChannelsReturnsFreshSlice(t *testing.T) {
	t.Parallel()

	channels := KindRegistrationPending.Channels()
	channels[0] = "mutated"
	if KindRegistrationPending.Channels()[0] != ChannelPush {
		t.Fatal("Channels() shares its backing array with the registry")
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
			name:    "channel the kind lacks",
			prefs:   []Preference{{Kind: KindTagged, Channel: ChannelEmail}},
			wantErr: ErrUnsupportedChannel,
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
		{kind: KindTagged, channel: ChannelEmail}:              true,
	})
	want := []Preference{
		{Kind: KindTagged, Channel: ChannelPush, Enabled: false},
		{Kind: KindRegistrationPending, Channel: ChannelPush, Enabled: true, IsDefault: true},
		{Kind: KindRegistrationPending, Channel: ChannelEmail, Enabled: true},
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
