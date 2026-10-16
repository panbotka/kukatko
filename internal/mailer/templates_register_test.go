package mailer

import (
	"strings"
	"testing"
	"time"
	"unicode"
)

// informalMarkers are the words that give the informal register ("ty") away in
// Czech: the greeting, the pronouns and the second-person singular forms the
// templates would reach for. They are matched as whole words, so "tě" does not
// fire inside "tedy" or at the end of a sentence it closes.
var informalMarkers = []string{"ahoj", "tě", "ti", "tebe", "tobě", "tebou", "tvůj", "tvoje", "můžeš", "sis", "jsi"}

// formalMarkers are the words of the formal register ("vy") the mails speak in.
var formalMarkers = []string{"dobrý", "vás", "vám", "vaši", "váš", "vaše", "vašeho", "můžete", "jste"}

// everyTemplate renders every message Kukátko can send, each with every field
// filled — a display name included, so a greeting that started naming the
// recipient again would show — and, where a template branches, once per
// branch. A new template belongs here, or the register test cannot see it.
func everyTemplate() []Rendered {
	const name = "Pavla Pětková"
	return []Rendered{
		RenderRegistrationReceived(RegistrationReceivedData{DisplayName: name, Username: "pavla", ProjectName: "Rodina"}),
		RenderAccountApproved(AccountApprovedData{DisplayName: name, SignInURL: "https://kukatko.example.com/login"}),
		RenderAccountApproved(AccountApprovedData{
			DisplayName: name, ProjectName: "Rodina", ProjectURL: "https://kukatko.example.com/p/rodina/",
		}),
		RenderUsernameChanged(UsernameChangedData{
			DisplayName: name, Username: "pavla.p", SignInURL: "https://kukatko.example.com/login",
		}),
		RenderNewRegistrationPending(NewRegistrationPendingData{
			Username: "pavla", DisplayName: name, Email: "pavla@example.com", ProjectName: "Rodina",
			ApprovalURL: "https://kukatko.example.com/p/rodina/members",
		}),
		RenderPasswordReset(PasswordResetData{
			DisplayName: name, ResetURL: "https://kukatko.example.com/reset?token=abc", ValidFor: time.Hour,
		}),
		RenderProjectSetup(ProjectSetupData{
			DisplayName: name, ProjectName: "Rodina", Username: "pavla",
			SetPasswordURL: "https://kukatko.example.com/reset?token=abc", ValidFor: 7 * 24 * time.Hour,
		}),
		RenderProjectAdminAdded(ProjectAdminAddedData{
			DisplayName: name, ProjectName: "Rodina", Username: "pavla",
			ProjectURL: "https://kukatko.example.com/p/rodina/",
		}),
		RenderTasksWaitingDigest(TasksWaitingDigestData{
			DisplayName: name, ProjectName: "Rodina", Total: 7,
			Tasks:    []DigestTask{{Title: "Kdy to bylo?", State: "question", URL: "https://kukatko.example.com/p/rodina/tasks/t1"}},
			QueueURL: "https://kukatko.example.com/p/rodina/tasks?waiting=1",
		}),
		RenderTagged(TaggedData{DisplayName: name, ProjectName: "Rodina", Count: 3, URL: "https://kukatko.example.com/n/n1"}),
		RenderTagged(TaggedData{
			DisplayName: name, ProjectName: "Rodina", Count: 3,
			Groups: []TaggedGroup{{Album: "Pouť", Count: 2, URL: "https://kukatko.example.com/n/n1"}, {Count: 1}},
		}),
		RenderAlbumFeatured(AlbumFeaturedData{
			DisplayName: name, ProjectName: "Rodina", Title: "Pouť", URL: "https://kukatko.example.com/p/rodina/albums/a1",
		}),
		RenderTaskAssigned(TaskAssignedData{
			DisplayName: name, ProjectName: "Rodina", Who: "Eva", Title: "Kdy to bylo?",
			URL: "https://kukatko.example.com/p/rodina/tasks/t1",
		}),
		RenderCommentMention(CommentMentionData{
			DisplayName: name, ProjectName: "Rodina", Who: "Eva", Title: "Pouť", Excerpt: "Kdo to je?",
			URL: "https://kukatko.example.com/p/rodina/photos/p1",
		}),
		RenderCommentMention(CommentMentionData{
			DisplayName: name, ProjectName: "Rodina", Who: "Eva", OnTask: true, Title: "Kdy?",
			URL: "https://kukatko.example.com/p/rodina/tasks/t1",
		}),
		RenderCommentMention(CommentMentionData{
			DisplayName: name, ProjectName: "Rodina", Who: "Eva", OnAlbum: true, Title: "Pouť",
			URL: "https://kukatko.example.com/p/rodina/albums/a1",
		}),
		RenderNotification(NotificationData{
			DisplayName: name, ProjectName: "Rodina", Title: "Nová registrace čeká na schválení",
			Body: "Do Kukátka se právě zaregistroval uživatel Petr.", URL: "https://kukatko.example.com/p/rodina/members",
		}),
	}
}

// markersIn returns which of markers occur in text as whole words, compared
// case-insensitively.
func markersIn(text string, markers []string) []string {
	words := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) }) {
		words[word] = true
	}
	var found []string
	for _, marker := range markers {
		if words[marker] {
			found = append(found, marker)
		}
	}
	return found
}

// TestTemplates_speakOneRegister guards the one voice of every e-mail: the
// formal "vy". A template — subject or body — that uses an informal word fails,
// whether it slipped one in beside the formal ones (the mixed text this test
// was written after) or went informal altogether, and so does one that has no
// formal word to show for itself. markersIn is checked on both registers so a
// failure names the words on each side.
func TestTemplates_speakOneRegister(t *testing.T) {
	t.Parallel()

	for _, mail := range everyTemplate() {
		text := mail.Subject + "\n" + mail.Body
		informal := markersIn(text, informalMarkers)
		formal := markersIn(text, formalMarkers)
		if len(informal) > 0 {
			t.Errorf("%s speaks informally (%v) beside the formal %v:\n%s", mail.Template, informal, formal, text)
		}
		if len(formal) == 0 {
			t.Errorf("%s has no formal word at all:\n%s", mail.Template, text)
		}
	}
}

// TestTemplates_greetWithoutTheName verifies every message opens with the bare
// "Dobrý den," even though the recipient's display name is known: a name in
// the nominative ("Dobrý den, Pavla Pětková,") reads wrong in Czech and the
// vocative cannot be derived from a free-form name.
func TestTemplates_greetWithoutTheName(t *testing.T) {
	t.Parallel()

	for _, mail := range everyTemplate() {
		if first, _, _ := strings.Cut(mail.Body, "\n"); first != "Dobrý den," {
			t.Errorf("%s opens with %q, want %q", mail.Template, first, "Dobrý den,")
		}
	}
}

// TestMarkersIn verifies the marker match is by whole word and ignores case,
// so the register test neither misses "Ahoj," nor fires on "tedy".
func TestMarkersIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text string
		want []string
	}{
		{text: "Ahoj, Pavlo,", want: []string{"ahoj"}},
		{text: "označil tě.", want: []string{"tě"}},
		{text: "a tedy titulek", want: nil},
		{text: "Tam ho můžeš i vypnout", want: []string{"můžeš"}},
	}
	for _, tt := range tests {
		got := markersIn(tt.text, informalMarkers)
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("markersIn(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}
