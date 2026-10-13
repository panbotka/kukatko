//go:build integration

// Package projecttest is the tenant-isolation harness: it seeds two projects
// side by side — the bootstrap project as A and a freshly created one as B —
// with the same small catalogue in each, signs their members in, and offers the
// assertions every scoped package uses to prove that one project's rows cannot
// be seen from the other, through the store and through the HTTP handler.
//
// It is test-support code behind the integration build tag, like the database
// it seeds: a test calls dbtest.New and dbtest.TruncateAll first (which leaves
// the bootstrap project in place), then Seed.
//
// The rows are written with plain SQL that names project_id explicitly rather
// than through the stores. That is deliberate: the stores are exactly what the
// scoping steps change, and a harness written against them would put rows into
// whatever project the unscoped code defaults to — the very bug it exists to
// catch.
package projecttest

import (
	"net/http"
	"sync"

	"github.com/panbotka/kukatko/internal/project"
)

// Password is the password of every member Seed creates; ClientFor signs in
// with it.
const Password = "correct horse battery staple"

// Side names one of the two seeded projects.
type Side int

const (
	// SideA is the bootstrap project (project.BootstrapUID, slug "v").
	SideA Side = iota
	// SideB is the project Seed creates (slug "b-<random>", storage prefix
	// "projects/<uid>/").
	SideB
)

// String returns "A" or "B", for test messages.
func (s Side) String() string {
	if s == SideB {
		return "B"
	}
	return "A"
}

// Other returns the opposite side: B for A and A for B.
func (s Side) Other() Side {
	if s == SideB {
		return SideA
	}
	return SideB
}

// Tenant is everything Seed created in one project: the project itself, its
// one approved member and one row of every catalogue kind, all tied together
// (the marker, album, label and task are on the photo; the saved search and the
// upload link belong to the member).
type Tenant struct {
	// ProjectUID is the project's uid.
	ProjectUID string
	// Slug is the project's URL slug — the {project} segment of PathFor.
	Slug string
	// StoragePrefix is the project's object-store prefix ("" for the bootstrap
	// project, "projects/<uid>/" for B).
	StoragePrefix string
	// Token is a random lowercase word unique to this tenant that every seeded
	// title and name contains, so a search for it should match only this side.
	Token string

	// UserUID is the member's account uid.
	UserUID string
	// Username is the member's login name; the password is Password.
	Username string
	// Role is the member's role in the project (admin unless WithRole said
	// otherwise).
	Role project.Role

	// PhotoUID is the photo; it has one primary photo_files row and no object in
	// storage.
	PhotoUID string
	// PhotoFileHash is the photo's SHA-256 file hash.
	PhotoFileHash string
	// SubjectUID is the person marked on the photo.
	SubjectUID string
	// MarkerUID is the face marker assigning SubjectUID on PhotoUID.
	MarkerUID string
	// AlbumUID is the album holding the photo.
	AlbumUID string
	// LabelUID is the label on the photo.
	LabelUID string
	// TaskUID is the task over the photo, created by the member.
	TaskUID string
	// SavedSearchUID is the member's saved search.
	SavedSearchUID string
	// UploadLinkUID is the upload link the member manages.
	UploadLinkUID string
	// UploadLinkCode is that link's readable short code.
	UploadLinkCode string
}

// UIDs returns every identifier of the tenant that must never appear in an
// answer given inside the other project: the project, the member and each
// seeded row, plus the photo's file hash and the upload link's code. The slug
// is left out on purpose — "v" would match almost any body.
func (tb *Tenant) UIDs() []string {
	return []string{
		tb.ProjectUID, tb.UserUID, tb.PhotoUID, tb.PhotoFileHash, tb.SubjectUID, tb.MarkerUID,
		tb.AlbumUID, tb.LabelUID, tb.TaskUID, tb.SavedSearchUID, tb.UploadLinkUID, tb.UploadLinkCode,
	}
}

// Fixture is the result of Seed: the two tenants and the clients signed in as
// their members.
type Fixture struct {
	// A is the bootstrap project's tenant.
	A Tenant
	// B is the created project's tenant.
	B Tenant

	// mu guards clients, so parallel subtests may each sign a client in.
	mu sync.Mutex
	// clients remembers, for every client ClientFor handed out, which side it is
	// signed in as and which server it talks to.
	clients map[*http.Client]signedIn
}

// signedIn is what the Fixture knows about one of its clients.
type signedIn struct {
	// side is the tenant whose member the client is signed in as.
	side Side
	// baseURL is the httptest server the client signed in on.
	baseURL string
}

// Tenant returns the tenant of which.
func (f *Fixture) Tenant(which Side) *Tenant {
	if which == SideB {
		return &f.B
	}
	return &f.A
}

// options are the knobs of Seed.
type options struct {
	// roles is each side's member role.
	roles [2]project.Role
	// maintainers says which side's member is an instance maintainer.
	maintainers [2]bool
}

// Option configures Seed.
type Option func(*options)

// WithRole makes the member of which a member with role instead of admin.
func WithRole(which Side, role project.Role) Option {
	return func(o *options) {
		o.roles[which] = role
	}
}

// AsMaintainer makes the member of which an instance maintainer as well — the
// account the maintainer-only project routes (import history, processing
// backfills) admit. Its membership is still of its own project only.
func AsMaintainer(which Side) Option {
	return func(o *options) {
		o.maintainers[which] = true
	}
}

// defaultOptions returns the options Seed starts from: an admin on each side.
func defaultOptions() options {
	return options{roles: [2]project.Role{project.RoleAdmin, project.RoleAdmin}}
}
