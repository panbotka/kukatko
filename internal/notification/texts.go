package notification

import (
	"fmt"
	"strings"
)

// Text is what a notification says: the title shown in bold and the line under
// it. A render function produces it the way internal/mailer renders its mails —
// a pure function of a data struct, written in Czech, the instance's language —
// so a notification and the mail sent for the same event read alike.
type Text struct {
	// Title is the headline, never empty.
	Title string
	// Body is the line under the title.
	Body string
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
	return Text{
		Title: "Nová registrace čeká na schválení",
		Body:  "Do Kukátka se právě zaregistroval uživatel " + registrantName(d) + ".",
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
	if lang == LanguageEnglish {
		return Text{
			Title: "New featured album: " + name,
			Body:  "The album " + name + " is now featured on the home page.",
		}
	}
	return Text{
		Title: "Nové doporučené album: " + name,
		Body:  "Na úvodní stránce Kukátka je nově doporučené album " + name + ".",
	}
}

// MaxTaskTitleLen is the most characters of a task's title a notification
// repeats. A task title may run to 200 characters, the whole of MaxTitleLen,
// so a longer one is cut and ended with an ellipsis after the name of whoever
// asked.
const MaxTaskTitleLen = 100

// RenderTaskAssigned builds the "somebody put you on a task" notification: who
// asked (by name) and the task's title, in lang (anything but English reads
// as Czech). The headline is the whole message — "Anna tě přidal(a) k úkolu:
// Kdy to bylo?" — so a lock screen says who and what; the line under it is the
// call to open the task. The asking person's name is trimmed and, when blank
// (an account since deleted), replaced by "Někdo"/"Somebody"; the title is cut
// at MaxTaskTitleLen and a blank one reads as untitled. It is a pure function
// of its arguments.
func RenderTaskAssigned(lang Language, who, title string) Text {
	name := taskTitle(lang, title)
	asker := strings.TrimSpace(who)
	if lang == LanguageEnglish {
		if asker == "" {
			asker = "Somebody"
		}
		return Text{
			Title: asker + " added you to the task: " + name,
			Body:  "Open the task and answer.",
		}
	}
	if asker == "" {
		asker = "Někdo"
	}
	return Text{
		Title: asker + " tě přidal(a) k úkolu: " + name,
		Body:  "Otevři úkol a odpověz.",
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
