package mailer

import "strings"

// TaggedData is what the "you were tagged" message needs: whom to greet, how
// many photos the notification holds and the absolute link that opens exactly
// those photos. An empty URL leaves the link out.
type TaggedData struct {
	DisplayName string
	Count       int
	URL         string
}

// RenderTagged builds the e-mail channel of the tagged notification kind. Like
// the push it names nobody — several people may tag inside one window — and it
// agrees with the count in all three Czech plural forms ("1 fotka", "3 fotky",
// "7 fotek"). It is a pure function of d.
func RenderTagged(d TaggedData) Rendered {
	photos := czechCount(d.Count, "jedné fotce", "fotkách", "fotkách")
	subject := "Označili vás na fotkách"
	if d.Count == 1 {
		subject = "Označili vás na fotce"
	}
	lines := []string{
		informalGreeting(d.DisplayName),
		"",
		"v Kukátku vás někdo označil na " + photos + ".",
	}
	if url := strings.TrimSpace(d.URL); url != "" {
		lines = append(lines, "", "Podívat se na ně můžeš tady:", "", url)
	}
	lines = append(lines, "", notificationFooter, "", signature)
	return Rendered{Template: TemplateTagged, Subject: subject, Body: body(lines...)}
}

// NotificationData is what the generic notification message needs: whom to
// greet, the notification's own title and body, and the absolute link it opens.
// An empty Body or URL leaves that part out.
type NotificationData struct {
	DisplayName string
	Title       string
	Body        string
	URL         string
}

// RenderNotification builds the e-mail channel of a notification kind that has
// no template of its own: the title is the subject, the body and the link are
// the message. It is what lets a new kind be mailed from the day it exists. It
// is a pure function of d.
func RenderNotification(d NotificationData) Rendered {
	lines := []string{informalGreeting(d.DisplayName), ""}
	text := strings.TrimSpace(d.Body)
	if text == "" {
		text = strings.TrimSpace(d.Title)
	}
	lines = append(lines, text)
	if url := strings.TrimSpace(d.URL); url != "" {
		lines = append(lines, "", url)
	}
	lines = append(lines, "", notificationFooter, "", signature)
	return Rendered{Template: TemplateNotification, Subject: strings.TrimSpace(d.Title), Body: body(lines...)}
}

// notificationFooter closes every notification mail: it says why the message
// came and where to stop it, because the recipient chose it and can unchoose it.
const notificationFooter = "Tenhle e-mail ti chodí, protože sis ho zapnul(a) v nastavení oznámení\n" +
	"v Kukátku (Účet → Oznámení). Tam ho můžeš i vypnout."
