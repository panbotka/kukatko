package mailer

import "time"

// Template names of the two messages a system administrator's "set up a
// project for somebody" sends its first admin (see internal/auth ProjectSetup).
const (
	// TemplateProjectSetup tells somebody a project was set up for them and
	// carries the link to choose the first password of their new account.
	TemplateProjectSetup = "project_setup"
	// TemplateProjectAdminAdded tells somebody who already has an account that
	// a new project was set up for them and they are its admin.
	TemplateProjectAdminAdded = "project_admin_added"
)

// ProjectSetupData is what the "a project was set up for you" message needs:
// whom to greet, the project, the username the new account signs in with, the
// link to choose its password and how long that link stays usable. A
// non-positive ValidFor renders as a general note about limited validity.
type ProjectSetupData struct {
	DisplayName    string
	ProjectName    string
	Username       string
	SetPasswordURL string
	ValidFor       time.Duration
}

// RenderProjectSetup builds the message with the link to choose the first
// password of an account created as a project's first admin. It is not the
// password-reset text on purpose: the reader never had a password, nor asked
// for this one. It is a pure function of d.
func RenderProjectSetup(d ProjectSetupData) Rendered {
	return Rendered{
		Template: TemplateProjectSetup,
		Subject:  InProject(d.ProjectName, "Byl pro vás založen projekt"),
		Body: body(
			greeting,
			"",
			"byl pro vás založen projekt „"+d.ProjectName+"“ v Kukátku a jste jeho správce.",
			"Nastavte si heslo:",
			"",
			d.SetPasswordURL,
			"",
			validitySentence(d.ValidFor)+" Použít se dá jen jednou.",
			"Přihlašovací jméno: "+orDash(d.Username),
			"",
			"Po nastavení hesla se přihlaste a otevře se vám rovnou váš projekt.",
			"",
			signature,
		),
	}
}

// ProjectAdminAddedData is what the "you are the admin of a new project"
// message needs: whom to greet, the project, the username of the account they
// already have and the link to the project.
type ProjectAdminAddedData struct {
	DisplayName string
	ProjectName string
	Username    string
	ProjectURL  string
}

// RenderProjectAdminAdded builds the message announcing a project set up for
// somebody who already has an account: no password link — they sign in as
// they always have — just the way to the project. It is a pure function of d.
func RenderProjectAdminAdded(d ProjectAdminAddedData) Rendered {
	return Rendered{
		Template: TemplateProjectAdminAdded,
		Subject:  InProject(d.ProjectName, "Byl pro vás založen projekt"),
		Body: body(
			greeting,
			"",
			"byl pro vás založen projekt „"+d.ProjectName+"“ v Kukátku a jste jeho správce.",
			"Přihlaste se svým účtem „"+d.Username+"“, projekt otevřete tady:",
			"",
			d.ProjectURL,
			"",
			signature,
		),
	}
}
