package mailer

import (
	"strings"
	"testing"
)

// TestRenderTasksWaitingDigest pins the exact Czech text of the daily digest:
// the greeting, one line per task with its state in words and its link, the
// "and N more" tail when the list was cut, and the link to the full queue.
func TestRenderTasksWaitingDigest(t *testing.T) {
	t.Parallel()

	got := RenderTasksWaitingDigest(TasksWaitingDigestData{
		DisplayName: "Jan Novák",
		Total:       7,
		Tasks: []DigestTask{
			{Title: "Kdy byl dům přestavěn?", State: "question", URL: "https://kukatko.example.com/tasks/tk-1"},
			{Title: "Rok svatby", State: "review", URL: "https://kukatko.example.com/tasks/tk-2"},
		},
		QueueURL: "https://kukatko.example.com/tasks?waiting=1",
	})

	if got.Template != TemplateTasksWaitingDigest {
		t.Errorf("Template = %q, want %q", got.Template, TemplateTasksWaitingDigest)
	}
	if want := "Čeká na tebe 7 úkolů"; got.Subject != want {
		t.Errorf("Subject = %q, want %q", got.Subject, want)
	}
	want := "Ahoj, Jan Novák,\n" +
		"\n" +
		"v Kukátku na tebe čeká 7 úkolů:\n" +
		"\n" +
		"- Kdy byl dům přestavěn? (čeká na odpověď)\n" +
		"  https://kukatko.example.com/tasks/tk-1\n" +
		"- Rok svatby (ke schválení)\n" +
		"  https://kukatko.example.com/tasks/tk-2\n" +
		"\n" +
		"…a dalších 5 úkolů.\n" +
		"\n" +
		"Všechno, co na tebe čeká: https://kukatko.example.com/tasks?waiting=1\n" +
		"\n" +
		"Kukátko\n"
	if got.Body != want {
		t.Errorf("Body =\n%q\nwant\n%q", got.Body, want)
	}
}

// TestRenderTasksWaitingDigest_counts verifies the subject and the opening line
// follow the Czech plural rule, the verb agrees with the count, and the "and N
// more" tail appears only when the list was cut.
func TestRenderTasksWaitingDigest_counts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		total       int
		listed      int
		wantSubject string
		wantOpening string
		wantTail    string
	}{
		{
			name: "one task", total: 1, listed: 1,
			wantSubject: "Čeká na tebe 1 úkol", wantOpening: "v Kukátku na tebe čeká 1 úkol:",
		},
		{
			name: "three tasks take the plural verb", total: 3, listed: 3,
			wantSubject: "Čekají na tebe 3 úkoly", wantOpening: "v Kukátku na tebe čekají 3 úkoly:",
		},
		{
			name: "five tasks", total: 5, listed: 5,
			wantSubject: "Čeká na tebe 5 úkolů", wantOpening: "v Kukátku na tebe čeká 5 úkolů:",
		},
		{
			name: "one more than listed", total: 4, listed: 3,
			wantSubject: "Čekají na tebe 4 úkoly", wantOpening: "v Kukátku na tebe čekají 4 úkoly:",
			wantTail: "…a další 1 úkol.",
		},
		{
			name: "three more than listed", total: 6, listed: 3,
			wantSubject: "Čeká na tebe 6 úkolů", wantOpening: "v Kukátku na tebe čeká 6 úkolů:",
			wantTail: "…a další 3 úkoly.",
		},
		{
			name: "many more than listed", total: 30, listed: 20,
			wantSubject: "Čeká na tebe 30 úkolů", wantOpening: "v Kukátku na tebe čeká 30 úkolů:",
			wantTail: "…a dalších 10 úkolů.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tasks := make([]DigestTask, tt.listed)
			for i := range tasks {
				tasks[i] = DigestTask{Title: "T", State: "working", URL: "https://k.example/tasks/x"}
			}
			got := RenderTasksWaitingDigest(TasksWaitingDigestData{Total: tt.total, Tasks: tasks})
			if got.Subject != tt.wantSubject {
				t.Errorf("Subject = %q, want %q", got.Subject, tt.wantSubject)
			}
			if !strings.Contains(got.Body, tt.wantOpening+"\n") {
				t.Errorf("Body = %q, want it to contain %q", got.Body, tt.wantOpening)
			}
			if tt.wantTail == "" && strings.Contains(got.Body, "…a dal") {
				t.Errorf("Body = %q, want no \"and more\" tail", got.Body)
			}
			if tt.wantTail != "" && !strings.Contains(got.Body, "\n"+tt.wantTail+"\n") {
				t.Errorf("Body = %q, want it to contain %q", got.Body, tt.wantTail)
			}
			if strings.Count(got.Body, "(pracuje se)") != tt.listed {
				t.Errorf("Body lists %d tasks, want %d", strings.Count(got.Body, "(pracuje se)"), tt.listed)
			}
		})
	}
}

// TestRenderTasksWaitingDigest_emptyNameAndUnknownState verifies the greeting
// degrades to a bare "Ahoj," and an unknown state is printed as it is rather
// than dropped.
func TestRenderTasksWaitingDigest_emptyNameAndUnknownState(t *testing.T) {
	t.Parallel()

	got := RenderTasksWaitingDigest(TasksWaitingDigestData{
		Total: 1,
		Tasks: []DigestTask{{Title: "  Otázka  ", State: "odd", URL: "/tasks/tk-1"}},
	})
	if !strings.HasPrefix(got.Body, "Ahoj,\n\n") {
		t.Errorf("Body = %q, want it to open with a bare greeting", got.Body)
	}
	if !strings.Contains(got.Body, "- Otázka (odd)\n  /tasks/tk-1\n") {
		t.Errorf("Body = %q, want the trimmed title, the raw state and the link", got.Body)
	}
}

// TestCapitalize verifies the first letter is upper-cased even when it is a
// multi-byte one, and an empty word survives.
func TestCapitalize(t *testing.T) {
	t.Parallel()

	tests := []struct{ in, want string }{
		{"čeká", "Čeká"},
		{"čekají", "Čekají"},
		{"a", "A"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := capitalize(tt.in); got != tt.want {
			t.Errorf("capitalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
