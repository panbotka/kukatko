package notification

import (
	"strings"
	"testing"
	"unicode/utf8"
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

// TestRenderTaggedInAlbum checks the per-album wording in both languages: the
// album is named in the title and the body, the Czech plural forms agree, a
// blank title reads as untitled and a long one is cut so the text still
// validates.
func TestRenderTaggedInAlbum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lang  Language
		count int
		album string
		title string
		body  string
	}{
		{name: "cs one", lang: LanguageCzech, count: 1, album: "Pouť 2026",
			title: "Označili vás na fotce v albu Pouť 2026", body: "Přibyla 1 fotka v albu Pouť 2026, na které vás označili."},
		{name: "cs few", lang: LanguageCzech, count: 3, album: " Pouť 2026 ",
			title: "Označili vás na fotkách v albu Pouť 2026", body: "Přibyly 3 fotky v albu Pouť 2026, na kterých vás označili."},
		{name: "cs many", lang: LanguageCzech, count: 5, album: "Pouť 2026",
			title: "Označili vás na fotkách v albu Pouť 2026", body: "Přibylo 5 fotek v albu Pouť 2026, na kterých vás označili."},
		{name: "cs untitled", lang: LanguageCzech, count: 2, album: "  ",
			title: "Označili vás na fotkách v albu (bez názvu)", body: "Přibyly 2 fotky v albu (bez názvu), na kterých vás označili."},
		{name: "en one", lang: LanguageEnglish, count: 1, album: "Fair",
			title: "You were tagged in a photo in the album Fair", body: "You were tagged in 1 photo in the album Fair."},
		{name: "en many", lang: LanguageEnglish, count: 7, album: "",
			title: "You were tagged in photos in the album (untitled)", body: "You were tagged in 7 photos in the album (untitled)."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RenderTaggedInAlbum(tt.lang, tt.count, tt.album)
			if got.Title != tt.title || got.Body != tt.body {
				t.Fatalf("RenderTaggedInAlbum(%q, %d, %q) = %+v, want title %q body %q",
					tt.lang, tt.count, tt.album, got, tt.title, tt.body)
			}
			if _, err := (New{UserUID: "us-x", Kind: KindTagged, Title: got.Title, Body: got.Body}).
				validate(); err != nil {
				t.Errorf("the rendered text does not validate: %v", err)
			}
		})
	}

	long := strings.Repeat("Velmi dlouhý název alba ", 20)
	got := RenderTaggedInAlbum(LanguageCzech, 5, long)
	if n := utf8.RuneCountInString(got.Title); n > MaxTitleLen || !strings.HasSuffix(got.Title, "…") {
		t.Errorf("long album: title %d runes %q, want cut with an ellipsis under %d", n, got.Title, MaxTitleLen)
	}
	if _, err := (New{UserUID: "us-x", Kind: KindTagged, Title: got.Title, Body: got.Body}).validate(); err != nil {
		t.Errorf("long album: the rendered text does not validate: %v", err)
	}
	if name := albumName(LanguageCzech, long); utf8.RuneCountInString(name) != MaxAlbumNameLen {
		t.Errorf("albumName = %q (%d runes), want exactly %d", name, utf8.RuneCountInString(name), MaxAlbumNameLen)
	}
}

// TestRenderTaggedOther checks the "other photos" wording in both languages
// with the Czech plural forms agreeing.
func TestRenderTaggedOther(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lang  Language
		count int
		title string
		body  string
	}{
		{name: "cs one", lang: LanguageCzech, count: 1,
			title: "Označili vás na další fotce", body: "Přibyla 1 další fotka, na které vás označili."},
		{name: "cs few", lang: LanguageCzech, count: 4,
			title: "Označili vás na dalších fotkách", body: "Přibyly 4 další fotky, na kterých vás označili."},
		{name: "cs many", lang: LanguageCzech, count: 9,
			title: "Označili vás na dalších fotkách", body: "Přibylo 9 dalších fotek, na kterých vás označili."},
		{name: "en one", lang: LanguageEnglish, count: 1,
			title: "You were tagged in another photo", body: "You were tagged in 1 other photo."},
		{name: "en many", lang: LanguageEnglish, count: 6,
			title: "You were tagged in other photos", body: "You were tagged in 6 other photos."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RenderTaggedOther(tt.lang, tt.count)
			if got.Title != tt.title || got.Body != tt.body {
				t.Fatalf("RenderTaggedOther(%q, %d) = %+v, want title %q body %q", tt.lang, tt.count, got, tt.title, tt.body)
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

// TestRenderAlbumFeatured checks the wording in both languages, the untitled
// fallback and the cut of an overlong title.
func TestRenderAlbumFeatured(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", MaxAlbumNameLen+10)
	tests := []struct {
		name  string
		lang  Language
		album string
		want  Text
	}{
		{
			name: "czech", lang: LanguageCzech, album: " Pouť 2026 ",
			want: Text{
				Title: "Nové doporučené album: Pouť 2026",
				Body:  "Na úvodní stránce Kukátka je nově doporučené album Pouť 2026.",
			},
		},
		{
			name: "english", lang: LanguageEnglish, album: "Pouť 2026",
			want: Text{
				Title: "New featured album: Pouť 2026",
				Body:  "The album Pouť 2026 is now featured on the home page.",
			},
		},
		{
			name: "unknown language reads as czech", lang: "de", album: "Pouť",
			want: Text{
				Title: "Nové doporučené album: Pouť",
				Body:  "Na úvodní stránce Kukátka je nově doporučené album Pouť.",
			},
		},
		{
			name: "untitled", lang: LanguageCzech, album: "  ",
			want: Text{
				Title: "Nové doporučené album: (bez názvu)",
				Body:  "Na úvodní stránce Kukátka je nově doporučené album (bez názvu).",
			},
		},
		{
			name: "overlong title is cut", lang: LanguageEnglish, album: long,
			want: Text{
				Title: "New featured album: " + strings.Repeat("x", MaxAlbumNameLen-1) + "…",
				Body:  "The album " + strings.Repeat("x", MaxAlbumNameLen-1) + "… is now featured on the home page.",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RenderAlbumFeatured(tt.lang, tt.album)
			if got != tt.want {
				t.Errorf("RenderAlbumFeatured(%q, %q) = %+v, want %+v", tt.lang, tt.album, got, tt.want)
			}
			if utf8.RuneCountInString(got.Title) > MaxTitleLen {
				t.Errorf("title over MaxTitleLen: %d", utf8.RuneCountInString(got.Title))
			}
		})
	}
}
