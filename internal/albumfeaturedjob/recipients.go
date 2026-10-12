package albumfeaturedjob

import "github.com/panbotka/kukatko/internal/project"

// account is what the recipient rules know about one user.
type account struct {
	// UID is the account.
	UID string
	// Role is the account's role in the album's project (admin for a
	// maintainer, "" for no membership); it decides whether a private album may
	// be announced to it.
	Role project.Role
	// Disabled is whether the account was switched off; a disabled account
	// hears nothing.
	Disabled bool
}

// recipients returns, in the given order, the accounts a featured album may be
// announced to: never the account that featured it (actorUID, empty for a
// system caller), never a disabled one, and — for a private album — only an
// account that may curate, the rule the homepage strip applies to a private
// album. Whether each of them wants the kind at all is notifydeliver's
// question, asked per account when the notification is delivered. It is a pure
// function of its arguments.
func recipients(accounts []account, actorUID string, private bool) []string {
	out := make([]string, 0, len(accounts))
	for _, a := range accounts {
		switch {
		case a.Disabled:
		case a.UID == actorUID:
		case private && !a.Role.CanCurate():
		default:
			out = append(out, a.UID)
		}
	}
	return out
}
