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
