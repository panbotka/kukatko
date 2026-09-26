package notification

import "testing"

// TestRenderRegistrationPending checks the wording: a fixed Czech title, and a
// body naming the person by display name, by username when they gave none.
func TestRenderRegistrationPending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   RegistrationPendingData
		body string
	}{
		{
			name: "display name",
			in:   RegistrationPendingData{DisplayName: " Jan Novák ", Username: "jnovak"},
			body: "Do Kukátka se právě zaregistroval uživatel Jan Novák.",
		},
		{
			name: "no display name falls back to the username",
			in:   RegistrationPendingData{DisplayName: "  ", Username: "jnovak"},
			body: "Do Kukátka se právě zaregistroval uživatel jnovak.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RenderRegistrationPending(tt.in)
			if got.Title != "Nová registrace čeká na schválení" {
				t.Errorf("Title = %q", got.Title)
			}
			if got.Body != tt.body {
				t.Errorf("Body = %q, want %q", got.Body, tt.body)
			}
			if _, err := (New{UserUID: "us-x", Kind: KindRegistrationPending, Title: got.Title, Body: got.Body}).
				validate(); err != nil {
				t.Errorf("the rendered text does not validate: %v", err)
			}
		})
	}
}
