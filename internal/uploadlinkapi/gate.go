package uploadlinkapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/uploadlink"
)

// LinkResolver resolves a short code to its link. It is satisfied by
// *uploadlink.Store.
type LinkResolver interface {
	// ByCode returns the link code names, or uploadlink.ErrNotFound.
	ByCode(ctx context.Context, code string) (uploadlink.Link, error)
}

// RegistrationGate implements auth.UploadLinkGate: a live upload link stands in
// for the shared registration secret, and the photos the registering browser
// uploaded anonymously become the new account's.
type RegistrationGate struct {
	links   LinkResolver
	sidecar SidecarEnqueuer
	now     func() time.Time
	log     *slog.Logger
}

// NewRegistrationGate returns a gate resolving links through links. sidecar,
// when not nil, reschedules the sidecars of the photos a registration claims
// (their uploader changed); now nil uses time.Now.
func NewRegistrationGate(links LinkResolver, sidecar SidecarEnqueuer, now func() time.Time) *RegistrationGate {
	if now == nil {
		now = time.Now
	}
	return &RegistrationGate{links: links, sidecar: sidecar, now: now, log: slog.Default()}
}

// AdmitRegistration admits a registration naming code when code names a live
// link, returning auth.ErrRegistrationLink otherwise — an unknown, expired and
// revoked link are one answer, so the registration endpoint confirms nothing
// about which codes exist. The grant claims, on the account's transaction, the
// photos r's anonymous upload session created.
func (g *RegistrationGate) AdmitRegistration(
	ctx context.Context, r *http.Request, code string,
) (auth.LinkGrant, error) {
	link, err := g.links.ByCode(ctx, code)
	if errors.Is(err, uploadlink.ErrNotFound) {
		return auth.LinkGrant{}, auth.ErrRegistrationLink
	}
	if err != nil {
		return auth.LinkGrant{}, fmt.Errorf("uploadlinkapi: resolving upload link: %w", err)
	}
	if link.StateAt(g.now()) != uploadlink.StateActive {
		return auth.LinkGrant{}, auth.ErrRegistrationLink
	}
	sessionHash := SessionHash(r)
	var claimed []string
	return auth.LinkGrant{
		LinkUID: link.UID,
		Claim: func(ctx context.Context, tx pgx.Tx, userUID string) error {
			uids, err := uploadlink.AttributeSessionTx(ctx, tx, sessionHash, userUID)
			claimed = uids
			return err //nolint:wrapcheck // already wrapped by uploadlink.
		},
		Committed: func(ctx context.Context) { g.rescheduleSidecars(ctx, claimed) },
	}, nil
}

// rescheduleSidecars schedules the sidecar rewrite of every claimed photo,
// best-effort.
func (g *RegistrationGate) rescheduleSidecars(ctx context.Context, photoUIDs []string) {
	if g.sidecar == nil {
		return
	}
	for _, uid := range photoUIDs {
		if err := g.sidecar.EnqueueSidecar(ctx, uid); err != nil {
			g.log.WarnContext(ctx, "uploadlinkapi: scheduling sidecar after a claim",
				slog.String("photo_uid", uid), slog.String("error", err.Error()))
		}
	}
}
