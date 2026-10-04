package photoapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/panbotka/kukatko/internal/uploadlink"
)

// fakeUploadLinks is a controllable UploadLinkProvenance that counts its calls.
type fakeUploadLinks struct {
	calls int
}

// Provenance records the call and answers with a fixed link.
func (f *fakeUploadLinks) Provenance(context.Context, string) (*uploadlink.Provenance, error) {
	f.calls++
	return &uploadlink.Provenance{LinkUID: "ul1", LinkTitle: "Pouť 2026"}, nil
}

// TestResolveUploadLink_guards verifies the provenance is never read without a
// wired store or without a signed-in caller — the role gate itself and the
// payload are covered against the real store by TestDetailUploadLink.
func TestResolveUploadLink_guards(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/photos/ph1", nil)

	if got := (&API{}).resolveUploadLink(req, "ph1"); got != nil {
		t.Errorf("no store wired: resolveUploadLink = %+v, want nil", got)
	}
	links := &fakeUploadLinks{}
	if got := (&API{uploadLinks: links}).resolveUploadLink(req, "ph1"); got != nil {
		t.Errorf("anonymous caller: resolveUploadLink = %+v, want nil", got)
	}
	if links.calls != 0 {
		t.Errorf("Provenance called %d times for an anonymous caller, want 0", links.calls)
	}
}
