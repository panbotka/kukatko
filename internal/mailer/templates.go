package mailer

import (
	"strconv"
	"strings"
	"time"
)

// Template names. Each names one of the four messages Kukátko can send and is
// carried on the Rendered value, so a log line or an audit entry can say which
// message went out without repeating its subject.
const (
	// TemplateRegistrationReceived confirms a registration and says it waits for
	// an administrator.
	TemplateRegistrationReceived = "registration_received"
	// TemplateAccountApproved tells somebody their account is now usable.
	TemplateAccountApproved = "account_approved"
	// TemplateNewRegistrationPending asks an administrator to approve somebody.
	TemplateNewRegistrationPending = "new_registration_pending"
	// TemplatePasswordReset carries the link to choose a new password.
	TemplatePasswordReset = "password_reset"
)

// signature closes every message. Kukátko sends as itself, never as a person.
const signature = "Kukátko"

// Rendered is what a template produced: the name of the template, the subject
// line and the plain-text body. Rendering never fails, so there is no error to
// carry — a template and its data struct are checked by the compiler.
type Rendered struct {
	Template string
	Subject  string
	Body     string
}

// Message turns a rendered template into a Message addressed to `to`.
func (r Rendered) Message(to string) Message {
	return Message{To: to, Subject: r.Subject, Body: r.Body}
}

// RegistrationReceivedData is what the "registration received" message needs:
// who registered (DisplayName may be empty — then the greeting is impersonal)
// and under which username.
type RegistrationReceivedData struct {
	DisplayName string
	Username    string
}

// RenderRegistrationReceived builds the message confirming a registration was
// received and is waiting for an administrator to approve it. It is a pure
// function of d.
func RenderRegistrationReceived(d RegistrationReceivedData) Rendered {
	return Rendered{
		Template: TemplateRegistrationReceived,
		Subject:  "Registrace do Kukátka čeká na schválení",
		Body: body(
			greeting(d.DisplayName),
			"",
			"vaši registraci do Kukátka jsme přijali. Účet „"+d.Username+"“ je založený,",
			"zatím ale není aktivní — musí ho nejdřív schválit správce.",
			"",
			"Až se tak stane, přijde vám další e-mail a budete se moct přihlásit.",
			"",
			signature,
		),
	}
}

// AccountApprovedData is what the "account approved" message needs: whom to
// greet and where to sign in.
type AccountApprovedData struct {
	DisplayName string
	SignInURL   string
}

// RenderAccountApproved builds the message telling somebody their account is
// active, with the link to sign in. It is a pure function of d.
func RenderAccountApproved(d AccountApprovedData) Rendered {
	return Rendered{
		Template: TemplateAccountApproved,
		Subject:  "Váš účet v Kukátku byl schválen",
		Body: body(
			greeting(d.DisplayName),
			"",
			"váš účet v Kukátku je od teď aktivní. Přihlásit se můžete tady:",
			"",
			d.SignInURL,
			"",
			signature,
		),
	}
}

// NewRegistrationPendingData is what the administrator's message needs: who
// registered, under which username and with which e-mail address.
type NewRegistrationPendingData struct {
	Username    string
	DisplayName string
	Email       string
}

// RenderNewRegistrationPending builds the message telling an administrator that
// somebody registered and is waiting for approval. It is a pure function of d.
func RenderNewRegistrationPending(d NewRegistrationPendingData) Rendered {
	return Rendered{
		Template: TemplateNewRegistrationPending,
		Subject:  "Nová registrace čeká na schválení: " + d.Username,
		Body: body(
			"Dobrý den,",
			"",
			"do Kukátka se zaregistroval nový uživatel a čeká na schválení:",
			"",
			"Uživatelské jméno: "+d.Username,
			"Jméno: "+orDash(d.DisplayName),
			"E-mail: "+orDash(d.Email),
			"",
			"Účet zůstane neaktivní, dokud ho někdo ze správců neschválí.",
			"",
			signature,
		),
	}
}

// PasswordResetData is what the password-reset message needs: whom to greet,
// where to choose a new password and how long that link stays usable. A
// non-positive ValidFor renders as a general note about limited validity rather
// than as a wrong number.
type PasswordResetData struct {
	DisplayName string
	ResetURL    string
	ValidFor    time.Duration
}

// RenderPasswordReset builds the message with the link to choose a new password.
// It is a pure function of d.
func RenderPasswordReset(d PasswordResetData) Rendered {
	return Rendered{
		Template: TemplatePasswordReset,
		Subject:  "Obnovení hesla do Kukátka",
		Body: body(
			greeting(d.DisplayName),
			"",
			"na této adrese si můžete nastavit nové heslo do Kukátka:",
			"",
			d.ResetURL,
			"",
			validitySentence(d.ValidFor),
			"Pokud jste o obnovení hesla nežádali, nemusíte dělat nic — heslo",
			"zůstane beze změny.",
			"",
			signature,
		),
	}
}

// body joins the lines of a message body with newlines and terminates the last
// one, so every body ends in exactly one line break. The SMTP sender turns the
// newlines into CRLF.
func body(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

// greeting opens a message addressed to name, falling back to an impersonal
// greeting when the display name is empty — better than greeting a blank.
func greeting(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "Dobrý den,"
	}
	return "Dobrý den, " + trimmed + ","
}

// orDash renders an empty value as an em dash, so a listing line never ends in a
// colon with nothing behind it.
func orDash(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "—"
	}
	return trimmed
}

// validitySentence says how long a link stays usable. A non-positive duration
// means nobody told us, so it says only that the validity is limited instead of
// printing a nonsensical zero.
func validitySentence(validFor time.Duration) string {
	if validFor <= 0 {
		return "Odkaz má omezenou platnost."
	}
	return "Odkaz platí " + formatValidity(validFor) + "."
}

// formatValidity renders a duration in Czech using the largest unit it divides
// evenly into — days, then hours, then minutes — so an hour reads "jednu hodinu"
// rather than "60 minut". Anything under a minute rounds up to one minute.
func formatValidity(validFor time.Duration) string {
	switch {
	case validFor >= 24*time.Hour && validFor%(24*time.Hour) == 0:
		return czechCount(int(validFor/(24*time.Hour)), "jeden den", "dny", "dnů")
	case validFor >= time.Hour && validFor%time.Hour == 0:
		return czechCount(int(validFor/time.Hour), "jednu hodinu", "hodiny", "hodin")
	default:
		minutes := int((validFor + 30*time.Second) / time.Minute)
		return czechCount(max(minutes, 1), "jednu minutu", "minuty", "minut")
	}
}

// czechCount applies the Czech plural rule to a count of units: `one` is the
// whole phrase for a single unit (it carries its own numeral, "jednu hodinu"),
// `few` is the form for two to four and `many` the form for everything else.
func czechCount(count int, one, few, many string) string {
	switch {
	case count == 1:
		return one
	case count >= 2 && count <= 4:
		return strconv.Itoa(count) + " " + few
	default:
		return strconv.Itoa(count) + " " + many
	}
}
