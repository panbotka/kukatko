package mailer

import "testing"

// projectFooter is the closing every notification mail shares, as the goldens
// below spell it out.
const projectFooter = "\n" + notificationFooter + "\n\nKukátko\n"

// TestTemplates_nameTheProject pins, byte for byte, every mail that names a
// project: the subject carries "Kukátko · <name>: " and the body says which
// project the event happened in, its links under /p/<slug>/.
func TestTemplates_nameTheProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		got         Rendered
		wantSubject string
		wantBody    string
	}{
		{
			name: "tagged",
			got: RenderTagged(TaggedData{
				DisplayName: "Anna", ProjectName: "Veselice", Count: 3, URL: "https://k.example/p/v/n/nt1",
			}),
			wantSubject: "Kukátko · Veselice: Označili vás na fotkách",
			wantBody: "Dobrý den,\n\nv projektu „Veselice“ v Kukátku vás někdo označil na 3 fotkách.\n\n" +
				"Podívat se na ně můžete tady:\n\nhttps://k.example/p/v/n/nt1\n" + projectFooter,
		},
		{
			name: "tagged by album",
			got: RenderTagged(TaggedData{
				ProjectName: "Rodina", Count: 2,
				Groups: []TaggedGroup{{Album: "Vánoce", Count: 2, URL: "https://k.example/p/rodina/n/a"}},
			}),
			wantSubject: "Kukátko · Rodina: Označili vás na fotkách",
			wantBody: "Dobrý den,\n\nv projektu „Rodina“ v Kukátku vás někdo označil na 2 fotkách. Podle alb:\n\n" +
				"Vánoce – 2 fotky\nhttps://k.example/p/rodina/n/a\n" + projectFooter,
		},
		{
			name: "generic notification",
			got: RenderNotification(NotificationData{
				DisplayName: "Jan", ProjectName: "Veselice", Title: "Nová registrace",
				Body: "Petr čeká na schválení.", URL: "https://k.example/p/v/members",
			}),
			wantSubject: "Kukátko · Veselice: Nová registrace",
			wantBody: "Dobrý den,\n\nZ projektu „Veselice“ v Kukátku:\n\nPetr čeká na schválení.\n\n" +
				"https://k.example/p/v/members\n" + projectFooter,
		},
		{
			name: "album featured",
			got: RenderAlbumFeatured(AlbumFeaturedData{
				ProjectName: "Veselice", Title: "Pouť", URL: "https://k.example/p/v/albums/al1",
			}),
			wantSubject: "Kukátko · Veselice: Nové doporučené album: Pouť",
			wantBody: "Dobrý den,\n\nv projektu „Veselice“ v Kukátku je na úvodní stránce nově doporučené album Pouť.\n\n" +
				"Podívat se na něj můžete tady:\n\nhttps://k.example/p/v/albums/al1\n" + projectFooter,
		},
		{
			name: "task assigned",
			got: RenderTaskAssigned(TaskAssignedData{
				ProjectName: "Veselice", Who: "Eva", Title: "Kdo je to?", URL: "https://k.example/p/v/tasks/tk1",
			}),
			wantSubject: "Kukátko · Veselice: Eva vás přidal(a) k úkolu: Kdo je to?",
			wantBody: "Dobrý den,\n\nv projektu „Veselice“ v Kukátku vás Eva přidal(a) k úkolu „Kdo je to?“.\n\n" +
				"Podívat se na něj a odpovědět můžete tady:\n\nhttps://k.example/p/v/tasks/tk1\n" + projectFooter,
		},
		{
			name: "comment mention",
			got: RenderCommentMention(CommentMentionData{
				ProjectName: "Veselice", Who: "Eva", OnAlbum: true, Title: "Pouť", Excerpt: "Koukni!",
				URL: "https://k.example/p/v/albums/al1",
			}),
			wantSubject: "Kukátko · Veselice: Eva vás zmínil(a) v komentáři u alba „Pouť“",
			wantBody: "Dobrý den,\n\nv projektu „Veselice“ v Kukátku vás Eva zmínil(a) v komentáři u alba „Pouť“:\n\n" +
				"    Koukni!\n\nPodívat se na komentář můžete tady:\n\nhttps://k.example/p/v/albums/al1\n" +
				projectFooter,
		},
		{
			name:        "registration received",
			got:         RenderRegistrationReceived(RegistrationReceivedData{Username: "petr", ProjectName: "Veselice"}),
			wantSubject: "Kukátko · Veselice: Registrace do Kukátka čeká na schválení",
			wantBody: "Dobrý den,\n\nvaši registraci do projektu „Veselice“ v Kukátku jsme přijali. " +
				"Účet „petr“ je založený,\nzatím ale není aktivní — musí ho nejdřív schválit správce.\n\n" +
				"Až se tak stane, přijde vám další e-mail a budete se moct přihlásit.\n\nKukátko\n",
		},
		{
			name: "registration pending",
			got: RenderNewRegistrationPending(NewRegistrationPendingData{
				Username: "petr", ProjectName: "Veselice", ApprovalURL: "https://k.example/p/v/members",
			}),
			wantSubject: "Kukátko · Veselice: Nová registrace čeká na schválení: petr",
			wantBody: "Dobrý den,\n\ndo projektu „Veselice“ v Kukátku se zaregistroval nový uživatel " +
				"a čeká na schválení:\n\nUživatelské jméno: petr\nJméno: —\nE-mail: —\n\n" +
				"Účet zůstane neaktivní, dokud ho někdo ze správců neschválí.\n\n" +
				"Schválit nebo zamítnout: https://k.example/p/v/members\n\nKukátko\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got.Subject != tt.wantSubject {
				t.Errorf("Subject = %q, want %q", tt.got.Subject, tt.wantSubject)
			}
			if tt.got.Body != tt.wantBody {
				t.Errorf("Body =\n%q\nwant\n%q", tt.got.Body, tt.wantBody)
			}
		})
	}
}

// TestInProject verifies the subject prefix: a name is trimmed and prefixed, a
// blank one leaves the subject alone.
func TestInProject(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"":           "Předmět",
		"   ":        "Předmět",
		" Veselice ": "Kukátko · Veselice: Předmět",
	} {
		if got := InProject(name, "Předmět"); got != want {
			t.Errorf("InProject(%q) = %q, want %q", name, got, want)
		}
	}
}
