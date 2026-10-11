package mailer

import (
	"strings"
	"testing"
)

// TestRenderTagged pins the tagged mail's text for one photo and checks the
// Czech plural forms and the optional link.
func TestRenderTagged(t *testing.T) {
	t.Parallel()

	got := RenderTagged(TaggedData{DisplayName: "Anna", Count: 1, URL: "https://kukatko.example/n/nt1"})
	if got.Template != TemplateTagged || got.Subject != taggedSubjectOne {
		t.Errorf("template %q, subject %q", got.Template, got.Subject)
	}
	want := "Ahoj, Anna,\n" +
		"\n" +
		"v Kukátku vás někdo označil na jedné fotce.\n" +
		"\n" +
		"Podívat se na ně můžeš tady:\n" +
		"\n" +
		"https://kukatko.example/n/nt1\n" +
		"\n" +
		notificationFooter + "\n" +
		"\n" +
		"Kukátko\n"
	if got.Body != want {
		t.Errorf("Body =\n%q\nwant\n%q", got.Body, want)
	}

	for count, phrase := range map[int]string{3: "na 3 fotkách.", 7: "na 7 fotkách."} {
		r := RenderTagged(TaggedData{Count: count})
		if r.Subject != taggedSubject || !strings.Contains(r.Body, phrase) {
			t.Errorf("count %d: subject %q, body %q; want %q", count, r.Subject, r.Body, phrase)
		}
		if strings.Contains(r.Body, "tady:") || !strings.HasPrefix(r.Body, "Ahoj,\n") {
			t.Errorf("count %d without link or name: body %q", count, r.Body)
		}
	}
}

// TestRenderTagged_groups pins the per-album breakdown: one mail for the whole
// window with the total, one line per album with its count and link, and the
// photos outside the listed albums last under "Další fotky".
func TestRenderTagged_groups(t *testing.T) {
	t.Parallel()

	got := RenderTagged(TaggedData{DisplayName: "Anna", Count: 8, Groups: []TaggedGroup{
		{Album: "Pouť 2026", Count: 5, URL: "https://k.example/n/a"},
		{Album: " Hasiči ", Count: 2, URL: "https://k.example/n/b"},
		{Count: 1, URL: "https://k.example/n/c"},
	}})
	if got.Template != TemplateTagged || got.Subject != taggedSubject {
		t.Errorf("template %q, subject %q", got.Template, got.Subject)
	}
	want := "Ahoj, Anna,\n" +
		"\n" +
		"v Kukátku vás někdo označil na 8 fotkách. Podle alb:\n" +
		"\n" +
		"Pouť 2026 – 5 fotek\n" +
		"https://k.example/n/a\n" +
		"\n" +
		"Hasiči – 2 fotky\n" +
		"https://k.example/n/b\n" +
		"\n" +
		"Další fotky – 1 fotka\n" +
		"https://k.example/n/c\n" +
		"\n" +
		notificationFooter + "\n" +
		"\n" +
		"Kukátko\n"
	if got.Body != want {
		t.Errorf("Body =\n%q\nwant\n%q", got.Body, want)
	}

	// Groups win over URL, and a group without a link has no link line.
	r := RenderTagged(TaggedData{Count: 1, URL: "https://k.example/n/x", Groups: []TaggedGroup{{Album: "A", Count: 1}}})
	if strings.Contains(r.Body, "/n/x") || !strings.Contains(r.Body, "A – 1 fotka\n\n") {
		t.Errorf("groups without link: body %q", r.Body)
	}
}

// TestRenderNotification checks the generic mail: the title is the subject,
// the body and link the message, and an empty body falls back to the title.
func TestRenderNotification(t *testing.T) {
	t.Parallel()

	got := RenderNotification(NotificationData{
		DisplayName: "Eva", Title: " Nové album ", Body: "Přibylo album Pouť.", URL: "https://k.example/albums/a1",
	})
	if got.Template != TemplateNotification || got.Subject != "Nové album" {
		t.Errorf("template %q, subject %q", got.Template, got.Subject)
	}
	want := "Ahoj, Eva,\n\nPřibylo album Pouť.\n\nhttps://k.example/albums/a1\n\n" + notificationFooter + "\n\nKukátko\n"
	if got.Body != want {
		t.Errorf("Body =\n%q\nwant\n%q", got.Body, want)
	}

	bare := RenderNotification(NotificationData{Title: "Jen titulek"})
	if want := "Ahoj,\n\nJen titulek\n\n" + notificationFooter + "\n\nKukátko\n"; bare.Body != want {
		t.Errorf("bare Body =\n%q\nwant\n%q", bare.Body, want)
	}
}

// TestRenderAlbumFeatured pins the featured-album mail: the album in the
// subject, the link when there is one, the untitled fallback.
func TestRenderAlbumFeatured(t *testing.T) {
	t.Parallel()

	got := RenderAlbumFeatured(AlbumFeaturedData{
		DisplayName: "Anna", Title: " Pouť 2026 ", URL: "https://kukatko.example/albums/al1",
	})
	if got.Template != TemplateAlbumFeatured || got.Subject != "Nové doporučené album: Pouť 2026" {
		t.Errorf("template %q, subject %q", got.Template, got.Subject)
	}
	want := "Ahoj, Anna,\n" +
		"\n" +
		"v Kukátku je na úvodní stránce nově doporučené album Pouť 2026.\n" +
		"\n" +
		"Podívat se na něj můžeš tady:\n" +
		"\n" +
		"https://kukatko.example/albums/al1\n" +
		"\n" +
		notificationFooter + "\n" +
		"\n" +
		"Kukátko\n"
	if got.Body != want {
		t.Errorf("Body =\n%q\nwant\n%q", got.Body, want)
	}

	bare := RenderAlbumFeatured(AlbumFeaturedData{Title: "  "})
	if bare.Subject != "Nové doporučené album: (bez názvu)" || !strings.HasPrefix(bare.Body, "Ahoj,\n") {
		t.Errorf("untitled: subject %q, body %q", bare.Subject, bare.Body)
	}
	if strings.Contains(bare.Body, "tady:") || !strings.Contains(bare.Body, "album (bez názvu).") {
		t.Errorf("untitled without link: body %q", bare.Body)
	}
}

// TestRenderTaskAssigned pins the task_assigned mail: who asked and the task in
// the subject, the sentence and the link in the body, and the fallbacks for a
// blank asker and a blank title without a link.
func TestRenderTaskAssigned(t *testing.T) {
	t.Parallel()

	got := RenderTaskAssigned(TaskAssignedData{
		DisplayName: "Anna", Who: " Pan Botka ", Title: " Kdy to bylo? ", URL: "https://kukatko.example/tasks/pt1",
	})
	if got.Template != TemplateTaskAssigned || got.Subject != "Pan Botka tě přidal(a) k úkolu: Kdy to bylo?" {
		t.Errorf("template %q, subject %q", got.Template, got.Subject)
	}
	want := "Ahoj, Anna,\n" +
		"\n" +
		"v Kukátku tě Pan Botka přidal(a) k úkolu „Kdy to bylo?“.\n" +
		"\n" +
		"Podívat se na něj a odpovědět můžeš tady:\n" +
		"\n" +
		"https://kukatko.example/tasks/pt1\n" +
		"\n" +
		notificationFooter + "\n" +
		"\n" +
		"Kukátko\n"
	if got.Body != want {
		t.Errorf("Body =\n%q\nwant\n%q", got.Body, want)
	}

	bare := RenderTaskAssigned(TaskAssignedData{Who: " ", Title: "  "})
	if bare.Subject != "Někdo tě přidal(a) k úkolu: (bez názvu)" || !strings.HasPrefix(bare.Body, "Ahoj,\n") {
		t.Errorf("nameless and untitled: subject %q, body %q", bare.Subject, bare.Body)
	}
	if strings.Contains(bare.Body, "tady:") || !strings.Contains(bare.Body, "tě Někdo přidal(a) k úkolu „(bez názvu)“.") {
		t.Errorf("nameless and untitled without link: body %q", bare.Body)
	}
}
