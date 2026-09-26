package notification

import "strings"

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
