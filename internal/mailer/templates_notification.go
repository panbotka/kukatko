package mailer

import "strings"

// TaggedData is what the "you were tagged" message needs: whom to greet, how
// many photos the window holds in all, and either the absolute link that opens
// exactly those photos (URL — a window whose photos are in no album, one
// notification) or the per-album breakdown (Groups — one entry per album
// notification plus one for the photos outside them, each with its own link).
// An empty URL leaves the link out; a non-empty Groups wins over URL.
type TaggedData struct {
	DisplayName string
	Count       int
	URL         string
	Groups      []TaggedGroup
}

// TaggedGroup is one line of the per-album breakdown: the album's title (empty
// for the photos no listed album holds), how many of the window's photos it
// counts and the absolute link to the notification that opens them (empty
// leaves it out).
type TaggedGroup struct {
	Album string
	Count int
	URL   string
}

// RenderTagged builds the e-mail channel of the tagged notification kind. Like
// the push it names nobody — several people may tag inside one window — and it
// agrees with the count in all three Czech plural forms ("1 fotka", "3 fotky",
// "7 fotek"). A window split by album is one mail all the same: the total,
// then one line per album with its count and link, the photos outside the
// albums last. It is a pure function of d.
func RenderTagged(d TaggedData) Rendered {
	photos := czechCount(d.Count, "jedné fotce", "fotkách", "fotkách")
	subject := taggedSubject
	if d.Count == 1 {
		subject = taggedSubjectOne
	}
	lines := []string{informalGreeting(d.DisplayName), ""}
	switch {
	case len(d.Groups) > 0:
		lines = append(lines, "v Kukátku vás někdo označil na "+photos+". Podle alb:")
		for _, g := range d.Groups {
			lines = append(lines, "", taggedGroupLine(g))
			if url := strings.TrimSpace(g.URL); url != "" {
				lines = append(lines, url)
			}
		}
	default:
		lines = append(lines, "v Kukátku vás někdo označil na "+photos+".")
		if url := strings.TrimSpace(d.URL); url != "" {
			lines = append(lines, "", "Podívat se na ně můžeš tady:", "", url)
		}
	}
	lines = append(lines, "", notificationFooter, "", signature)
	return Rendered{Template: TemplateTagged, Subject: subject, Body: body(lines...)}
}

// taggedSubject and taggedSubjectOne are the tagged mail's subject lines for
// several photos and for one.
const (
	taggedSubject    = "Označili vás na fotkách"
	taggedSubjectOne = "Označili vás na fotce"
)

// taggedGroupLine is one album's line of the breakdown: the title (or "další
// fotky" for the photos outside the listed albums) and the count in the right
// plural form.
func taggedGroupLine(g TaggedGroup) string {
	name := strings.TrimSpace(g.Album)
	if name == "" {
		name = "Další fotky"
	}
	return name + " – " + czechCount(g.Count, "1 fotka", "fotky", "fotek")
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

// AlbumFeaturedData is what the "a new album is featured" message needs: whom
// to greet, the album's title and the absolute link to the album. An empty URL
// leaves the link out.
type AlbumFeaturedData struct {
	DisplayName string
	Title       string
	URL         string
}

// RenderAlbumFeatured builds the e-mail channel of the album_featured
// notification kind: the subject names the album the way the notification's
// title does, the body says it is on the homepage and links to it. It names no
// curator — the strip is the library's recommendation, not one person's. It is
// a pure function of d.
func RenderAlbumFeatured(d AlbumFeaturedData) Rendered {
	title := strings.TrimSpace(d.Title)
	if title == "" {
		title = "(bez názvu)"
	}
	lines := []string{
		informalGreeting(d.DisplayName), "",
		"v Kukátku je na úvodní stránce nově doporučené album " + title + ".",
	}
	if url := strings.TrimSpace(d.URL); url != "" {
		lines = append(lines, "", "Podívat se na něj můžeš tady:", "", url)
	}
	lines = append(lines, "", notificationFooter, "", signature)
	return Rendered{Template: TemplateAlbumFeatured, Subject: "Nové doporučené album: " + title, Body: body(lines...)}
}
