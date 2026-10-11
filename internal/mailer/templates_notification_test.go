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
	if got.Template != TemplateTagged || got.Subject != "Označili vás na fotce" {
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
		if r.Subject != "Označili vás na fotkách" || !strings.Contains(r.Body, phrase) {
			t.Errorf("count %d: subject %q, body %q; want %q", count, r.Subject, r.Body, phrase)
		}
		if strings.Contains(r.Body, "tady:") || !strings.HasPrefix(r.Body, "Ahoj,\n") {
			t.Errorf("count %d without link or name: body %q", count, r.Body)
		}
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
