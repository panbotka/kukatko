package notification

import (
	"strings"
	"testing"
)

// TestRenderRegistrationPending checks the wording: a fixed Czech title, and a
// body naming the person by display name, by username when they gave none.
func TestRenderRegistrationPending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   RegistrationPendingData
		body string
	}{
		{
			name: "display name",
			in:   RegistrationPendingData{DisplayName: " Jan Novák ", Username: "jnovak"},
			body: "Do Kukátka se právě zaregistroval uživatel Jan Novák.",
		},
		{
			name: "no display name falls back to the username",
			in:   RegistrationPendingData{DisplayName: "  ", Username: "jnovak"},
			body: "Do Kukátka se právě zaregistroval uživatel jnovak.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RenderRegistrationPending(tt.in)
			if got.Title != "Nová registrace čeká na schválení" {
				t.Errorf("Title = %q", got.Title)
			}
			if got.Body != tt.body {
				t.Errorf("Body = %q, want %q", got.Body, tt.body)
			}
			if _, err := (New{UserUID: "us-x", Kind: KindRegistrationPending, Title: got.Title, Body: got.Body}).
				validate(); err != nil {
				t.Errorf("the rendered text does not validate: %v", err)
			}
		})
	}
}

// TestRenderTagged checks the tagged wording in both languages: one photo reads
// in the singular, Czech uses its three plural forms with the verb agreeing, and
// no text ever names who did the tagging.
func TestRenderTagged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lang  Language
		count int
		title string
		body  string
	}{
		{name: "cs one", lang: LanguageCzech, count: 1,
			title: "Označili vás na fotce", body: "Přibyla 1 fotka, na které vás označili."},
		{name: "cs two", lang: LanguageCzech, count: 2,
			title: "Označili vás na fotkách", body: "Přibyly 2 fotky, na kterých vás označili."},
		{name: "cs three", lang: LanguageCzech, count: 3,
			title: "Označili vás na fotkách", body: "Přibyly 3 fotky, na kterých vás označili."},
		{name: "cs four", lang: LanguageCzech, count: 4,
			title: "Označili vás na fotkách", body: "Přibyly 4 fotky, na kterých vás označili."},
		{name: "cs five", lang: LanguageCzech, count: 5,
			title: "Označili vás na fotkách", body: "Přibylo 5 fotek, na kterých vás označili."},
		{name: "cs twelve", lang: LanguageCzech, count: 12,
			title: "Označili vás na fotkách", body: "Přibylo 12 fotek, na kterých vás označili."},
		{name: "cs twenty-two", lang: LanguageCzech, count: 22,
			title: "Označili vás na fotkách", body: "Přibylo 22 fotek, na kterých vás označili."},
		{name: "unknown language is Czech", lang: "de", count: 3,
			title: "Označili vás na fotkách", body: "Přibyly 3 fotky, na kterých vás označili."},
		{name: "en one", lang: LanguageEnglish, count: 1,
			title: "You were tagged in a photo", body: "You were tagged in 1 photo."},
		{name: "en twelve", lang: LanguageEnglish, count: 12,
			title: "You were tagged in photos", body: "You were tagged in 12 photos."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RenderTagged(tt.lang, tt.count)
			if got.Title != tt.title || got.Body != tt.body {
				t.Fatalf("RenderTagged(%q, %d) = %+v, want title %q body %q", tt.lang, tt.count, got, tt.title, tt.body)
			}
			if _, err := (New{UserUID: "us-x", Kind: KindTagged, Title: got.Title, Body: got.Body}).
				validate(); err != nil {
				t.Errorf("the rendered text does not validate: %v", err)
			}
		})
	}
}

// TestCzechPlural checks the three categories at their boundaries.
func TestCzechPlural(t *testing.T) {
	t.Parallel()

	for n, want := range map[int]string{0: "other", 1: "one", 2: "few", 4: "few", 5: "other", 11: "other", 21: "other"} {
		if got := czechPlural(n, "one", "few", "other"); got != want {
			t.Errorf("czechPlural(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestVisiblePhotoSQL pins the predicate to the three filters Photos applies
// under the zero Visibility, so the two readings cannot drift apart.
func TestVisiblePhotoSQL(t *testing.T) {
	t.Parallel()

	for _, clause := range []string{"p.archived_at IS NULL", "NOT p.hidden_from_library", "NOT p.private"} {
		if !strings.Contains(VisiblePhotoSQL, clause) || !strings.Contains(photosSQL, clause) {
			t.Errorf("clause %q missing from VisiblePhotoSQL or photosSQL", clause)
		}
	}
}
