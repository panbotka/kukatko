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
		title = untitled
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

// TaskAssignedData is what the "you were put on a task" message needs: whom to
// greet, who asked (by name; blank reads as „někdo"), the task's title and the
// absolute link to the task. An empty URL leaves the link out.
type TaskAssignedData struct {
	DisplayName string
	Who         string
	Title       string
	URL         string
}

// RenderTaskAssigned builds the e-mail channel of the task_assigned
// notification kind: the subject says who put the recipient on which task, the
// way the notification's title does, and the body links to the task. It is a
// pure function of d.
func RenderTaskAssigned(d TaskAssignedData) Rendered {
	who := strings.TrimSpace(d.Who)
	if who == "" {
		who = "Někdo"
	}
	title := strings.TrimSpace(d.Title)
	if title == "" {
		title = untitled
	}
	lines := []string{
		informalGreeting(d.DisplayName), "",
		"v Kukátku tě " + who + " přidal(a) k úkolu „" + title + "“.",
	}
	if url := strings.TrimSpace(d.URL); url != "" {
		lines = append(lines, "", "Podívat se na něj a odpovědět můžeš tady:", "", url)
	}
	lines = append(lines, "", notificationFooter, "", signature)
	return Rendered{
		Template: TemplateTaskAssigned,
		Subject:  who + " tě přidal(a) k úkolu: " + title,
		Body:     body(lines...),
	}
}

// untitled is how a mail names a task or an album whose title is blank.
const untitled = "(bez názvu)"

// CommentMentionData is what the "somebody mentioned you in a comment"
// message needs: whom to greet, who wrote the comment (by name; blank reads as
// „někdo"), whether the thread is a task's (else a photograph's), the title of
// that task or photograph (a photo's may be blank), the comment's excerpt and
// the absolute link to the comment. An empty URL leaves the link out.
type CommentMentionData struct {
	DisplayName string
	Who         string
	OnTask      bool
	Title       string
	Excerpt     string
	URL         string
}

// RenderCommentMention builds the e-mail channel of the comment_mention
// notification kind: the subject says who mentioned the recipient and where,
// the way the notification's title does, and the body quotes the comment and
// links to it. It is a pure function of d.
func RenderCommentMention(d CommentMentionData) Rendered {
	who := strings.TrimSpace(d.Who)
	if who == "" {
		who = "Někdo"
	}
	place := mentionPlace(d)
	lines := []string{
		informalGreeting(d.DisplayName), "",
		"v Kukátku tě " + who + " zmínil(a) v komentáři u " + place + ":",
	}
	if excerpt := strings.TrimSpace(d.Excerpt); excerpt != "" {
		lines = append(lines, "", "    "+excerpt)
	}
	if url := strings.TrimSpace(d.URL); url != "" {
		lines = append(lines, "", "Podívat se na komentář můžeš tady:", "", url)
	}
	lines = append(lines, "", notificationFooter, "", signature)
	return Rendered{
		Template: TemplateCommentMention,
		Subject:  who + " tě zmínil(a) v komentáři u " + place,
		Body:     body(lines...),
	}
}

// mentionPlace is how the mention mail names the thread's subject: a task by
// its title in quotes (a blank one reads as untitled), a photograph by its
// title in quotes when it has one and bare otherwise.
func mentionPlace(d CommentMentionData) string {
	title := strings.TrimSpace(d.Title)
	if d.OnTask {
		if title == "" {
			title = untitled
		}
		return "úkolu „" + title + "“"
	}
	if title == "" {
		return "fotky"
	}
	return "fotky „" + title + "“"
}
