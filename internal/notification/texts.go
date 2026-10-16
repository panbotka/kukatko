package notification

import (
	"fmt"
	"strings"
)

// Text is what a notification says: the title shown in bold and the line under
// it. A render function produces it the way internal/mailer renders its mails —
// a pure function of a data struct, written in Czech, the instance's language —
// so a notification and the mail sent for the same event read alike.
//
// Params are the facts the text was rendered from. They are stored beside the
// text (notifications.params), so the in-app inbox can say the same thing in
// the reader's own language instead of the instance's; the stored Title and
// Body stay the fallback, and what a push or an e-mail carries.
type Text struct {
	// Title is the headline, never empty.
	Title string
	// Body is the line under the title.
	Body string
	// Params are the facts Title and Body were rendered from.
	Params Params
}

// Variants of the tagged kind's text, Params.Variant.
const (
	// TaggedVariantAlbum is RenderTaggedInAlbum: the photos of one album.
	TaggedVariantAlbum = "album"
	// TaggedVariantOther is RenderTaggedOther: the photos no album names.
	TaggedVariantOther = "other"
)

// Params are the facts a notification's text was rendered from, per kind:
//
//   - tagged: Count, and Variant "" (RenderTagged), "album" (with Album) or
//     "other";
//   - registration_pending: Name, the registering person;
//   - album_featured: Album;
//   - task_assigned: Name (who asked) and Title (the task);
//   - comment_mention: Name (the writer), Place and Title (the thread's
//     subject); the body is the comment's own words, in no language.
//
// Names and titles are stored trimmed and already cut the way the text cuts
// them; a blank one stays "" and the reader writes its own "untitled" or
// "somebody". The zero Params (an unknown kind, or a caller with no facts) is
// stored as NULL, which a reader takes as "show the stored text".
type Params struct {
	// Variant picks one of a kind's texts; "" is the kind's plain one.
	Variant string `json:"variant,omitempty"`
	// Count is how many photos the text counts.
	Count int `json:"count,omitempty"`
	// Name is the person the text names.
	Name string `json:"name,omitempty"`
	// Album is the album the text names.
	Album string `json:"album,omitempty"`
	// Title is the task's, album's or photo's title the text names.
	Title string `json:"title,omitempty"`
	// Place is where a mentioning comment sits.
	Place MentionPlace `json:"place,omitempty"`
}

// IsZero reports whether p holds no fact at all.
func (p Params) IsZero() bool {
	return p == Params{}
}

// RegistrationPendingData is what the "a registration is waiting" notification
// needs: who registered. DisplayName may be empty; the username stands in then.
type RegistrationPendingData struct {
	DisplayName string
	Username    string
}

// RenderRegistrationPending builds the notification telling an administrator
// that somebody registered and waits for approval, naming them. The wording
// follows the mail sent for the same event (mailer.RenderNewRegistrationPending).
// It is a pure function of d.
func RenderRegistrationPending(d RegistrationPendingData) Text {
	name := registrantName(d)
	return Text{
		Title:  "Nová registrace čeká na schválení",
		Body:   "Do Kukátka se právě zaregistroval uživatel " + name + ".",
		Params: Params{Name: name},
	}
}

// registrantName is the name a notification calls a registering person by: the
// display name, or the username when they gave none.
func registrantName(d RegistrationPendingData) string {
	if name := strings.TrimSpace(d.DisplayName); name != "" {
		return name
	}
	return strings.TrimSpace(d.Username)
}

// Language is a language a notification can be written in. Czech is the
// instance's language and the default; English is the other one the app
// speaks.
type Language string

const (
	// LanguageCzech is Czech, the default.
	LanguageCzech Language = "cs"
	// LanguageEnglish is English.
	LanguageEnglish Language = "en"
)

// RenderTagged builds the "you were tagged in N photos" notification for count
// photos, in lang (anything but English reads as Czech, the default).
//
// It deliberately names nobody: several people may tag inside one window, so a
// single name would be wrong and a list would not fit a lock screen. It reads
// correctly for one photo, and in Czech it uses all three plural forms —
// "1 fotka", "2–4 fotky", "5 a více fotek" — with the verb agreeing ("přibyla",
// "přibyly", "přibylo"). The caller never asks for zero: an empty window sends
// nothing. It is a pure function of its arguments.
func RenderTagged(lang Language, count int) Text {
	text := renderTagged(lang, count)
	text.Params = Params{Count: count}
	return text
}

// renderTagged is RenderTagged's wording, without the params.
func renderTagged(lang Language, count int) Text {
	if lang == LanguageEnglish {
		if count == 1 {
			return Text{Title: "You were tagged in a photo", Body: "You were tagged in 1 photo."}
		}
		return Text{Title: "You were tagged in photos", Body: fmt.Sprintf("You were tagged in %d photos.", count)}
	}
	title := "Označili vás na fotkách"
	if count == 1 {
		title = "Označili vás na fotce"
	}
	body := czechPlural(count,
		"Přibyla %d fotka, na které vás označili.",
		"Přibyly %d fotky, na kterých vás označili.",
		"Přibylo %d fotek, na kterých vás označili.")
	return Text{Title: title, Body: fmt.Sprintf(body, count)}
}

// czechPlural picks the Czech form for n: one for exactly 1, few for 2 to 4,
// other for everything else (0, 5 and up, and so also 11–14 and 22 — written
// Czech says "22 fotek"). These are the CLDR integer categories for Czech.
func czechPlural(n int, one, few, other string) string {
	switch {
	case n == 1:
		return one
	case n >= 2 && n <= 4:
		return few
	default:
		return other
	}
}

// MaxAlbumNameLen is the most characters of an album's title a notification
// repeats. An album title is unbounded; a title has MaxTitleLen and a lock
// screen far less, so a longer one is cut and ended with an ellipsis.
const MaxAlbumNameLen = 60

// RenderTaggedInAlbum builds the "you were tagged in N photos in the album X"
// notification: the tagging window's message for the newly tagged photos of
// one album, in lang (anything but English reads as Czech). The wording is
// RenderTagged's with the album named, and the count is the album's — a photo
// in two albums is counted in each. The album's title is cut at
// MaxAlbumNameLen; an empty title reads as untitled. Like RenderTagged it
// names nobody and is never asked for zero. It is a pure function of its
// arguments.
func RenderTaggedInAlbum(lang Language, count int, album string) Text {
	text := renderTaggedInAlbum(lang, count, album)
	text.Params = Params{Variant: TaggedVariantAlbum, Count: count, Album: paramName(album, MaxAlbumNameLen)}
	return text
}

// renderTaggedInAlbum is RenderTaggedInAlbum's wording, without the params.
func renderTaggedInAlbum(lang Language, count int, album string) Text {
	name := albumName(lang, album)
	if lang == LanguageEnglish {
		if count == 1 {
			return Text{
				Title: "You were tagged in a photo in the album " + name,
				Body:  "You were tagged in 1 photo in the album " + name + ".",
			}
		}
		return Text{
			Title: "You were tagged in photos in the album " + name,
			Body:  fmt.Sprintf("You were tagged in %d photos in the album %s.", count, name),
		}
	}
	title := "Označili vás na fotkách v albu " + name
	if count == 1 {
		title = "Označili vás na fotce v albu " + name
	}
	body := czechPlural(count,
		"Přibyla %d fotka v albu %s, na které vás označili.",
		"Přibyly %d fotky v albu %s, na kterých vás označili.",
		"Přibylo %d fotek v albu %s, na kterých vás označili.")
	return Text{Title: title, Body: fmt.Sprintf(body, count, name)}
}

// RenderTaggedOther builds the "you were tagged in N other photos"
// notification: the tagging window's message for the newly tagged photos that
// no album notification of the same window names — photos in no album, in an
// album the person may not see, or in one of the albums beyond the cap. It is
// only used beside at least one album notification ("other" needs something to
// be other than); a window without albums uses RenderTagged. It is a pure
// function of its arguments.
func RenderTaggedOther(lang Language, count int) Text {
	text := renderTaggedOther(lang, count)
	text.Params = Params{Variant: TaggedVariantOther, Count: count}
	return text
}

// renderTaggedOther is RenderTaggedOther's wording, without the params.
func renderTaggedOther(lang Language, count int) Text {
	if lang == LanguageEnglish {
		if count == 1 {
			return Text{Title: "You were tagged in another photo", Body: "You were tagged in 1 other photo."}
		}
		return Text{Title: "You were tagged in other photos", Body: fmt.Sprintf("You were tagged in %d other photos.", count)}
	}
	title := "Označili vás na dalších fotkách"
	if count == 1 {
		title = "Označili vás na další fotce"
	}
	body := czechPlural(count,
		"Přibyla %d další fotka, na které vás označili.",
		"Přibyly %d další fotky, na kterých vás označili.",
		"Přibylo %d dalších fotek, na kterých vás označili.")
	return Text{Title: title, Body: fmt.Sprintf(body, count)}
}

// albumName is how a notification calls an album: its trimmed title, cut to
// MaxAlbumNameLen characters with an ellipsis, or "untitled" in lang when the
// title is blank.
func albumName(lang Language, title string) string {
	name := strings.TrimSpace(title)
	if name == "" {
		if lang == LanguageEnglish {
			return "(untitled)"
		}
		return "(bez názvu)"
	}
	return shorten(name, MaxAlbumNameLen)
}

// paramName is how Params keeps a name or a title: trimmed and cut at limit
// like the text cuts it, "" when blank (the reader says "untitled" itself).
func paramName(name string, limit int) string {
	return shorten(strings.TrimSpace(name), limit)
}

// shorten cuts name to at most limit characters, ending a cut name with an
// ellipsis (which counts towards the limit); a name within the limit is
// returned as it is.
func shorten(name string, limit int) string {
	runes := []rune(name)
	if len(runes) <= limit {
		return name
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

// RenderAlbumFeatured builds the "a new album is featured" notification for
// the album titled title, in lang (anything but English reads as Czech). The
// title is the headline — "Nové doporučené album: Pouť 2026" — so the album is
// named where a lock screen shows it, cut at MaxAlbumNameLen like every album
// name here; an empty title reads as untitled. It names no curator: the strip
// is the library's recommendation, not one person's. It is a pure function of
// its arguments.
func RenderAlbumFeatured(lang Language, title string) Text {
	name := albumName(lang, title)
	params := Params{Album: paramName(title, MaxAlbumNameLen)}
	if lang == LanguageEnglish {
		return Text{
			Title:  "New featured album: " + name,
			Body:   "The album " + name + " is now featured on the home page.",
			Params: params,
		}
	}
	return Text{
		Title:  "Nové doporučené album: " + name,
		Body:   "Na úvodní stránce Kukátka je nově doporučené album " + name + ".",
		Params: params,
	}
}

// MaxTaskTitleLen is the most characters of a task's title a notification
// repeats. A task title may run to 200 characters, the whole of MaxTitleLen,
// so a longer one is cut and ended with an ellipsis after the name of whoever
// asked.
const MaxTaskTitleLen = 100

// RenderTaskAssigned builds the "somebody put you on a task" notification: who
// asked (by name) and the task's title, in lang (anything but English reads
// as Czech). The headline is the whole message — "Anna vás přidal(a) k úkolu:
// Kdy to bylo?" — so a lock screen says who and what; the line under it is the
// call to open the task. The asking person's name is trimmed and, when blank
// (an account since deleted), replaced by "Někdo"/"Somebody"; the title is cut
// at MaxTaskTitleLen and a blank one reads as untitled. It is a pure function
// of its arguments.
func RenderTaskAssigned(lang Language, who, title string) Text {
	name := taskTitle(lang, title)
	asker := strings.TrimSpace(who)
	params := Params{Name: asker, Title: paramName(title, MaxTaskTitleLen)}
	if lang == LanguageEnglish {
		if asker == "" {
			asker = "Somebody"
		}
		return Text{
			Title:  asker + " added you to the task: " + name,
			Body:   "Open the task and answer.",
			Params: params,
		}
	}
	if asker == "" {
		asker = "Někdo"
	}
	return Text{
		Title:  asker + " vás přidal(a) k úkolu: " + name,
		Body:   "Otevřete úkol a odpovězte.",
		Params: params,
	}
}

// taskTitle is how a notification calls a task: its trimmed title, cut to
// MaxTaskTitleLen characters with an ellipsis, or "untitled" in lang when the
// title is blank (the store never stores one, but a render function refuses
// nothing).
func taskTitle(lang Language, title string) string {
	name := strings.TrimSpace(title)
	if name == "" {
		if lang == LanguageEnglish {
			return "(untitled)"
		}
		return "(bez názvu)"
	}
	return shorten(name, MaxTaskTitleLen)
}

// MentionPlace is where a comment that mentions somebody sits: on a
// photograph, on a task or on an album. The text names the place, because
// "u fotky", "u úkolu" and "u alba" are different sentences.
type MentionPlace string

const (
	// MentionPlacePhoto is a comment in a photograph's thread.
	MentionPlacePhoto MentionPlace = "photo"
	// MentionPlaceTask is a comment in a task's thread.
	MentionPlaceTask MentionPlace = "task"
	// MentionPlaceAlbum is a comment in an album's thread.
	MentionPlaceAlbum MentionPlace = "album"
)

// RenderCommentMention builds the "somebody mentioned you in a comment"
// notification: who wrote the comment (by name), where it sits — a photo, a
// task or an album, named by title — and the comment's excerpt as the line
// under the headline, in lang (anything but English reads as Czech). The
// writer's name is trimmed and, when blank, replaced by "Někdo"/"Somebody". A
// task or an album is named like RenderTaskAssigned names a task (cut at
// MaxTaskTitleLen, blank reads as untitled); a photograph is named only when it
// has a title, because most do not and "u fotky (bez názvu)" would be noise. An
// empty excerpt leaves the body empty. It is a pure function of its arguments.
func RenderCommentMention(lang Language, who string, place MentionPlace, title, excerpt string) Text {
	writer := strings.TrimSpace(who)
	params := Params{Name: writer, Place: place, Title: paramName(title, MaxTaskTitleLen)}
	if lang == LanguageEnglish {
		if writer == "" {
			writer = "Somebody"
		}
		return Text{Title: writer + " mentioned you in a comment on " + mentionPlace(lang, place, title),
			Body: strings.TrimSpace(excerpt), Params: params}
	}
	if writer == "" {
		writer = "Někdo"
	}
	return Text{Title: writer + " vás zmínil(a) v komentáři u " + mentionPlace(lang, place, title),
		Body: strings.TrimSpace(excerpt), Params: params}
}

// mentionPlace is how the mention notification names the thread's subject: the
// task or the album by its title, the photograph by its title when it has one
// and bare otherwise, in lang.
func mentionPlace(lang Language, place MentionPlace, title string) string {
	if place == MentionPlaceTask {
		if lang == LanguageEnglish {
			return "the task " + taskTitle(lang, title)
		}
		return "úkolu " + taskTitle(lang, title)
	}
	if place == MentionPlaceAlbum {
		if lang == LanguageEnglish {
			return "the album " + taskTitle(lang, title)
		}
		return "alba " + taskTitle(lang, title)
	}
	name := strings.TrimSpace(title)
	if lang == LanguageEnglish {
		if name == "" {
			return "a photo"
		}
		return "the photo " + shorten(name, MaxTaskTitleLen)
	}
	if name == "" {
		return "fotky"
	}
	return "fotky " + shorten(name, MaxTaskTitleLen)
}
