package photoapi

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/uploadlink"
)

// UploadLinkProvenance is the subset of the upload-link store the photo detail
// needs: where a photo came from when an upload link created it. It is an
// interface so photoapi depends on the behaviour rather than on uploadlink's
// construction, and so an instance without it wired simply passes nil.
type UploadLinkProvenance interface {
	// Provenance returns the upload link that created photoUID, or nil when no
	// link did.
	Provenance(ctx context.Context, photoUID string) (*uploadlink.Provenance, error)
}

// uploadLinkRef is the upload-link provenance embedded in a photo detail
// response: the link (its UID and title, which may be empty), the name the
// uploader typed on the public page (absent when they typed none), the account
// the upload is attributed to (absent for an anonymous upload nobody claimed)
// and when it arrived.
type uploadLinkRef struct {
	UID          string       `json:"uid"`
	Title        string       `json:"title"`
	UploaderName string       `json:"uploader_name,omitempty"`
	Account      *uploaderRef `json:"account,omitempty"`
	UploadedAt   time.Time    `json:"uploaded_at"`
}

// resolveUploadLink returns the upload link that created the photo, or nil when
// none did, no store is wired, the caller is below curator, or the lookup failed.
//
// It is curator-only on purpose. The name is whatever a guest typed on a public
// page — personal data the guest offered to the people running the event, not to
// every account of the family archive — and the link it names is managed on a
// curator page anyway. A viewer still sees the account uploader, as before.
//
// A failure is logged and swallowed: the line is an extra on a page that already
// answers half a dozen queries, and the photo is worth showing without it.
func (a *API) resolveUploadLink(r *http.Request, photoUID string) *uploadLinkRef {
	if a.uploadLinks == nil {
		return nil
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok || !user.Role.CanCurate() {
		return nil
	}
	prov, err := a.uploadLinks.Provenance(r.Context(), photoUID)
	if err != nil {
		log.Printf("photoapi: reading upload-link provenance of photo %s: %v", photoUID, err)
		return nil
	}
	if prov == nil {
		return nil
	}
	ref := &uploadLinkRef{
		UID: prov.LinkUID, Title: prov.LinkTitle, UploaderName: prov.UploaderName, UploadedAt: prov.UploadedAt,
	}
	if prov.AccountUID != nil {
		ref.Account = &uploaderRef{UID: *prov.AccountUID, Name: prov.AccountName}
	}
	return ref
}
