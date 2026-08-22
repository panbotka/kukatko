package mailer

import (
	"strings"
	"testing"
	"time"
)

// TestRenderRegistrationReceived pins the exact Czech text of the message that
// confirms a registration and says it waits for an administrator.
func TestRenderRegistrationReceived(t *testing.T) {
	t.Parallel()

	got := RenderRegistrationReceived(RegistrationReceivedData{DisplayName: "Jan Novák", Username: "jan"})

	if got.Template != TemplateRegistrationReceived {
		t.Errorf("Template = %q, want %q", got.Template, TemplateRegistrationReceived)
	}
	if want := "Registrace do Kukátka čeká na schválení"; got.Subject != want {
		t.Errorf("Subject = %q, want %q", got.Subject, want)
	}
	want := "Dobrý den, Jan Novák,\n" +
		"\n" +
		"vaši registraci do Kukátka jsme přijali. Účet „jan“ je založený,\n" +
		"zatím ale není aktivní — musí ho nejdřív schválit správce.\n" +
		"\n" +
		"Až se tak stane, přijde vám další e-mail a budete se moct přihlásit.\n" +
		"\n" +
		"Kukátko\n"
	if got.Body != want {
		t.Errorf("Body =\n%q\nwant\n%q", got.Body, want)
	}
}

// TestRenderAccountApproved pins the text of the approval message and checks the
// sign-in link is carried through verbatim.
func TestRenderAccountApproved(t *testing.T) {
	t.Parallel()

	got := RenderAccountApproved(AccountApprovedData{
		DisplayName: "Eva",
		SignInURL:   "https://kukatko.example.com/login",
	})

	if got.Template != TemplateAccountApproved {
		t.Errorf("Template = %q, want %q", got.Template, TemplateAccountApproved)
	}
	if want := "Váš účet v Kukátku byl schválen"; got.Subject != want {
		t.Errorf("Subject = %q, want %q", got.Subject, want)
	}
	want := "Dobrý den, Eva,\n" +
		"\n" +
		"váš účet v Kukátku je od teď aktivní. Přihlásit se můžete tady:\n" +
		"\n" +
		"https://kukatko.example.com/login\n" +
		"\n" +
		"Kukátko\n"
	if got.Body != want {
		t.Errorf("Body =\n%q\nwant\n%q", got.Body, want)
	}
}

// TestRenderNewRegistrationPending pins the administrator's message, which must
// name the username, the display name and the e-mail address.
func TestRenderNewRegistrationPending(t *testing.T) {
	t.Parallel()

	got := RenderNewRegistrationPending(NewRegistrationPendingData{
		Username:    "jan",
		DisplayName: "Jan Novák",
		Email:       "jan@example.com",
	})

	if got.Template != TemplateNewRegistrationPending {
		t.Errorf("Template = %q, want %q", got.Template, TemplateNewRegistrationPending)
	}
	if want := "Nová registrace čeká na schválení: jan"; got.Subject != want {
		t.Errorf("Subject = %q, want %q", got.Subject, want)
	}
	want := "Dobrý den,\n" +
		"\n" +
		"do Kukátka se zaregistroval nový uživatel a čeká na schválení:\n" +
		"\n" +
		"Uživatelské jméno: jan\n" +
		"Jméno: Jan Novák\n" +
		"E-mail: jan@example.com\n" +
		"\n" +
		"Účet zůstane neaktivní, dokud ho někdo ze správců neschválí.\n" +
		"\n" +
		"Kukátko\n"
	if got.Body != want {
		t.Errorf("Body =\n%q\nwant\n%q", got.Body, want)
	}
}

// TestRenderNewRegistrationPending_missingValues verifies an account without a
// display name or e-mail renders a dash rather than a line ending in a colon.
func TestRenderNewRegistrationPending_missingValues(t *testing.T) {
	t.Parallel()

	got := RenderNewRegistrationPending(NewRegistrationPendingData{Username: "jan"})

	for _, want := range []string{"Jméno: —\n", "E-mail: —\n"} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("Body =\n%s\nwant it to contain %q", got.Body, want)
		}
	}
}

// TestRenderPasswordReset pins the reset message, including how long the link
// stays valid.
func TestRenderPasswordReset(t *testing.T) {
	t.Parallel()

	got := RenderPasswordReset(PasswordResetData{
		DisplayName: "Jan",
		ResetURL:    "https://kukatko.example.com/reset?token=abc",
		ValidFor:    2 * time.Hour,
	})

	if got.Template != TemplatePasswordReset {
		t.Errorf("Template = %q, want %q", got.Template, TemplatePasswordReset)
	}
	if want := "Obnovení hesla do Kukátka"; got.Subject != want {
		t.Errorf("Subject = %q, want %q", got.Subject, want)
	}
	want := "Dobrý den, Jan,\n" +
		"\n" +
		"na této adrese si můžete nastavit nové heslo do Kukátka:\n" +
		"\n" +
		"https://kukatko.example.com/reset?token=abc\n" +
		"\n" +
		"Odkaz platí 2 hodiny.\n" +
		"Pokud jste o obnovení hesla nežádali, nemusíte dělat nic — heslo\n" +
		"zůstane beze změny.\n" +
		"\n" +
		"Kukátko\n"
	if got.Body != want {
		t.Errorf("Body =\n%q\nwant\n%q", got.Body, want)
	}
}

// TestRenderPasswordReset_unknownValidity verifies a caller that does not say
// how long the link lives gets a general sentence, never "0 minut".
func TestRenderPasswordReset_unknownValidity(t *testing.T) {
	t.Parallel()

	got := RenderPasswordReset(PasswordResetData{ResetURL: "https://example.com/r"})

	if !strings.Contains(got.Body, "Odkaz má omezenou platnost.\n") {
		t.Errorf("Body =\n%s\nwant the general validity sentence", got.Body)
	}
	if !strings.HasPrefix(got.Body, "Dobrý den,\n") {
		t.Errorf("Body =\n%s\nwant the impersonal greeting", got.Body)
	}
}

// TestGreeting covers the personal and impersonal openings.
func TestGreeting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "with a name", in: "Jan", want: "Dobrý den, Jan,"},
		{name: "trims", in: "  Jan  ", want: "Dobrý den, Jan,"},
		{name: "empty", in: "", want: "Dobrý den,"},
		{name: "blank", in: "   ", want: "Dobrý den,"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := greeting(tt.in); got != tt.want {
				t.Errorf("greeting(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestFormatValidity covers the Czech plural rule and the choice of unit: the
// largest one the duration divides evenly into.
func TestFormatValidity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   time.Duration
		want string
	}{
		{in: time.Minute, want: "jednu minutu"},
		{in: 3 * time.Minute, want: "3 minuty"},
		{in: 30 * time.Minute, want: "30 minut"},
		{in: 90 * time.Minute, want: "90 minut"},
		{in: 10 * time.Second, want: "jednu minutu"},
		{in: time.Hour, want: "jednu hodinu"},
		{in: 2 * time.Hour, want: "2 hodiny"},
		{in: 12 * time.Hour, want: "12 hodin"},
		{in: 24 * time.Hour, want: "jeden den"},
		{in: 72 * time.Hour, want: "3 dny"},
		{in: 168 * time.Hour, want: "7 dnů"},
	}
	for _, tt := range tests {
		t.Run(tt.in.String(), func(t *testing.T) {
			t.Parallel()
			if got := formatValidity(tt.in); got != tt.want {
				t.Errorf("formatValidity(%s) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestRendered_Message verifies a rendered template becomes a Message addressed
// to the recipient, with nothing else changed.
func TestRendered_Message(t *testing.T) {
	t.Parallel()

	rendered := RenderAccountApproved(AccountApprovedData{SignInURL: "https://example.com"})
	msg := rendered.Message("jan@example.com")

	want := Message{To: "jan@example.com", Subject: rendered.Subject, Body: rendered.Body}
	if msg != want {
		t.Errorf("Message = %+v, want %+v", msg, want)
	}
}

// TestTemplates_haveDistinctNames guards against a copy-paste that would make
// two templates report the same name in a log line.
func TestTemplates_haveDistinctNames(t *testing.T) {
	t.Parallel()

	names := map[string]bool{}
	for _, name := range []string{
		TemplateRegistrationReceived,
		TemplateAccountApproved,
		TemplateNewRegistrationPending,
		TemplatePasswordReset,
	} {
		if names[name] {
			t.Errorf("template name %q is used twice", name)
		}
		names[name] = true
	}
}
