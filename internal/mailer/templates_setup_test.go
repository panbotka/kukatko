package mailer

import (
	"testing"
	"time"
)

// TestRenderProjectSetup pins, byte for byte, the mail a new project's first
// admin gets with the link to choose their first password.
func TestRenderProjectSetup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		data        ProjectSetupData
		wantSubject string
		wantBody    string
	}{
		{
			name: "named, seven days",
			data: ProjectSetupData{
				DisplayName: "Jana Nováková", ProjectName: "Rodina", Username: "jana",
				SetPasswordURL: "https://k.example/password-reset/tok?project=rodina", ValidFor: 7 * 24 * time.Hour,
			},
			wantSubject: "Byl pro vás založen projekt „Rodina“ v Kukátku",
			wantBody: "Dobrý den,\n\nbyl pro vás založen projekt „Rodina“ v Kukátku a jste jeho správce.\n" +
				"Nastavte si heslo:\n\nhttps://k.example/password-reset/tok?project=rodina\n\n" +
				"Odkaz platí 7 dnů. Použít se dá jen jednou.\nPřihlašovací jméno: jana\n\n" +
				"Po nastavení hesla se přihlaste a otevře se vám rovnou váš projekt.\n\nKukátko\n",
		},
		{
			name:        "no name, unknown validity",
			data:        ProjectSetupData{ProjectName: "Chata", SetPasswordURL: "/password-reset/t"},
			wantSubject: "Byl pro vás založen projekt „Chata“ v Kukátku",
			wantBody: "Dobrý den,\n\nbyl pro vás založen projekt „Chata“ v Kukátku a jste jeho správce.\n" +
				"Nastavte si heslo:\n\n/password-reset/t\n\n" +
				"Odkaz má omezenou platnost. Použít se dá jen jednou.\nPřihlašovací jméno: —\n\n" +
				"Po nastavení hesla se přihlaste a otevře se vám rovnou váš projekt.\n\nKukátko\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RenderProjectSetup(tt.data)
			if got.Template != TemplateProjectSetup {
				t.Errorf("Template = %q, want %q", got.Template, TemplateProjectSetup)
			}
			if got.Subject != tt.wantSubject {
				t.Errorf("Subject = %q, want %q", got.Subject, tt.wantSubject)
			}
			if got.Body != tt.wantBody {
				t.Errorf("Body =\n%q\nwant\n%q", got.Body, tt.wantBody)
			}
		})
	}
}

// TestRenderProjectAdminAdded pins the mail announcing a new project to an
// admin who already has an account: the project link, no password link.
func TestRenderProjectAdminAdded(t *testing.T) {
	t.Parallel()

	got := RenderProjectAdminAdded(ProjectAdminAddedData{
		DisplayName: "Petr", ProjectName: "Rodina", Username: "petr", ProjectURL: "https://k.example/p/rodina/",
	})
	if got.Template != TemplateProjectAdminAdded {
		t.Errorf("Template = %q, want %q", got.Template, TemplateProjectAdminAdded)
	}
	if want := "Byl pro vás založen projekt „Rodina“ v Kukátku"; got.Subject != want {
		t.Errorf("Subject = %q, want %q", got.Subject, want)
	}
	want := "Dobrý den,\n\nbyl pro vás založen projekt „Rodina“ v Kukátku a jste jeho správce.\n" +
		"Přihlaste se svým účtem „petr“, projekt otevřete tady:\n\nhttps://k.example/p/rodina/\n\nKukátko\n"
	if got.Body != want {
		t.Errorf("Body =\n%q\nwant\n%q", got.Body, want)
	}
}
