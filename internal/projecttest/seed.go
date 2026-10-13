//go:build integration

package projecttest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/uploadlink"
)

const (
	// uidAlphabet is the 32-symbol lowercase alphabet every Kukátko uid is
	// drawn from (see internal/photos).
	uidAlphabet = "0123456789abcdefghijklmnopqrstuv"
	// uidSuffixLen is the number of random characters after a uid's
	// two-letter prefix.
	uidSuffixLen = 24
	// uploadLinkLifetime is how long the seeded upload link stays valid — long
	// enough that no test ever sees it expire.
	uploadLinkLifetime = 30 * 24 * time.Hour
)

// Seed creates the two tenants of the fixture over pool: A in the bootstrap
// project (which must exist — dbtest.TruncateAll leaves it in place) and B in
// a project it creates with the slug "b-<random>". In each it creates one
// approved member (admin by default, see WithRole) who belongs to that project
// only, and one photo with its primary photo_files row, a person with a face
// marker on the photo, an album holding it, a label on it, a task over it, a
// saved search of the member and an upload link the member manages. Every row
// names its project explicitly. Any failure fails tb at once.
func Seed(tb testing.TB, pool *pgxpool.Pool, opts ...Option) *Fixture {
	tb.Helper()
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}
	ctx := tb.Context()
	projects := project.NewStore(pool)

	bootstrap, err := projects.Get(ctx, project.BootstrapUID)
	if err != nil {
		tb.Fatalf("projecttest: the bootstrap project is missing (run dbtest.TruncateAll first): %v", err)
	}
	slug := "b-" + randomHex(tb, 4)
	created, err := projects.Create(ctx, audit.Meta{}, project.CreateParams{Slug: slug, Name: "Projekt " + slug})
	if err != nil {
		tb.Fatalf("projecttest: creating project B: %v", err)
	}

	f := &Fixture{clients: map[*http.Client]signedIn{}}
	for _, side := range []Side{SideA, SideB} {
		p := bootstrap
		if side == SideB {
			p = created
		}
		tenant := f.Tenant(side)
		*tenant = newTenant(tb, p, o.roles[side])
		seedMember(tb, pool, projects, tenant, o.maintainers[side])
		seedCatalogue(ctx, tb, pool, tenant)
	}
	return f
}

// newTenant mints every uid and name of a tenant in project p, with a member
// of role. Nothing is written yet.
func newTenant(tb testing.TB, p *project.Project, role project.Role) Tenant {
	tb.Helper()
	token := "tenant" + randomHex(tb, 4)
	code, err := uploadlink.NewCode()
	if err != nil {
		tb.Fatalf("projecttest: minting an upload link code: %v", err)
	}
	return Tenant{
		ProjectUID: p.UID, Slug: p.Slug, StoragePrefix: p.StoragePrefix, Token: token,
		UserUID: newUID(tb, "us"), Username: "member-" + token, Role: role,
		PhotoUID: newUID(tb, "ph"), PhotoFileHash: randomHex(tb, 32),
		SubjectUID: newUID(tb, "su"), MarkerUID: newUID(tb, "mk"),
		AlbumUID: newUID(tb, "al"), LabelUID: newUID(tb, "lb"), TaskUID: newUID(tb, "tk"),
		SavedSearchUID: newUID(tb, "ss"), UploadLinkUID: newUID(tb, "ul"), UploadLinkCode: code,
	}
}

// seedMember creates the tenant's account with no membership at all and then
// makes it an approved member of the tenant's project only, so the member of B
// is not also a member of the bootstrap project.
func seedMember(tb testing.TB, pool *pgxpool.Pool, projects *project.Store, tenant *Tenant, maintainer bool) {
	tb.Helper()
	hash, err := auth.HashPassword(Password)
	if err != nil {
		tb.Fatalf("projecttest: hashing the password: %v", err)
	}
	approved := time.Now()
	role := auth.RoleUser
	if maintainer {
		role = auth.RoleMaintainer
	}
	if err := auth.NewStore(pool).CreateUser(tb.Context(), auth.User{
		UID: tenant.UserUID, Username: tenant.Username, DisplayName: "Člen " + tenant.Token,
		Email: tenant.Username + "@example.test", PasswordHash: hash, Role: role,
		ApprovedAt: &approved,
	}); err != nil {
		tb.Fatalf("projecttest: creating member %s: %v", tenant.Username, err)
	}
	if _, err := projects.AddMember(tb.Context(), audit.Meta{}, tenant.ProjectUID, tenant.UserUID,
		tenant.Role, true); err != nil {
		tb.Fatalf("projecttest: adding %s to project %s: %v", tenant.Username, tenant.Slug, err)
	}
}

// statement is one INSERT of the catalogue seed.
type statement struct {
	// what names the row for the failure message.
	what string
	// sql is the statement.
	sql string
	// args are its arguments.
	args []any
}

// seedCatalogue writes the tenant's catalogue rows in one transaction.
func seedCatalogue(ctx context.Context, tb testing.TB, pool *pgxpool.Pool, tenant *Tenant) {
	tb.Helper()
	statements := catalogueStatements(tb, tenant)
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		for _, s := range statements {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				return fmt.Errorf("inserting the %s: %w", s.what, err)
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatalf("projecttest: seeding project %s: %v", tenant.Slug, err)
	}
}

// catalogueStatements returns the INSERTs of the tenant's catalogue in
// dependency order. Every root row names project_id; the child rows (file,
// marker, memberships of album, label and task) are scoped through their
// parent, as the schema has it.
func catalogueStatements(tb testing.TB, tn *Tenant) []statement {
	tb.Helper()
	params, err := json.Marshal(map[string]string{"q": tn.Token})
	if err != nil {
		tb.Fatalf("projecttest: encoding the saved search: %v", err)
	}
	filePath := "2026/10/" + tn.PhotoUID + ".jpg"
	return []statement{
		{"photo", `INSERT INTO photos (uid, project_id, file_hash, file_path, file_name, file_size, file_mime,
			file_width, file_height, taken_at, title, uploaded_by)
			VALUES ($1, $2, $3, $4, $5, 1024, 'image/jpeg', 640, 480, '2026-06-01T12:00:00Z', $6, $7)`,
			[]any{tn.PhotoUID, tn.ProjectUID, tn.PhotoFileHash, filePath, tn.PhotoUID + ".jpg",
				"Fotka " + tn.Token, tn.UserUID}},
		{"photo file", `INSERT INTO photo_files (photo_uid, file_path, file_hash, file_size, file_mime, is_primary, role)
			VALUES ($1, $2, $3, 1024, 'image/jpeg', true, 'original')`,
			[]any{tn.PhotoUID, filePath, tn.PhotoFileHash}},
		{"subject", `INSERT INTO subjects (uid, project_id, slug, name, type) VALUES ($1, $2, $3, $4, 'person')`,
			[]any{tn.SubjectUID, tn.ProjectUID, "osoba-" + tn.Token, "Osoba " + tn.Token}},
		{"marker", `INSERT INTO markers (uid, photo_uid, subject_uid, type, x, y, w, h)
			VALUES ($1, $2, $3, 'face', 0.25, 0.25, 0.2, 0.2)`,
			[]any{tn.MarkerUID, tn.PhotoUID, tn.SubjectUID}},
		{"album", `INSERT INTO albums (uid, project_id, slug, title, type, created_by)
			VALUES ($1, $2, $3, $4, 'album', $5)`,
			[]any{tn.AlbumUID, tn.ProjectUID, "album-" + tn.Token, "Album " + tn.Token, tn.UserUID}},
		{"album photo", `INSERT INTO album_photos (album_uid, photo_uid) VALUES ($1, $2)`,
			[]any{tn.AlbumUID, tn.PhotoUID}},
		{"label", `INSERT INTO labels (uid, project_id, slug, name) VALUES ($1, $2, $3, $4)`,
			[]any{tn.LabelUID, tn.ProjectUID, "stitek-" + tn.Token, "Štítek " + tn.Token}},
		{"photo label", `INSERT INTO photo_labels (photo_uid, label_uid) VALUES ($1, $2)`,
			[]any{tn.PhotoUID, tn.LabelUID}},
		{"task", `INSERT INTO photo_tasks (uid, project_id, title, created_by) VALUES ($1, $2, $3, $4)`,
			[]any{tn.TaskUID, tn.ProjectUID, "Úkol " + tn.Token, tn.UserUID}},
		{"task photo", `INSERT INTO photo_task_photos (task_uid, photo_uid) VALUES ($1, $2)`,
			[]any{tn.TaskUID, tn.PhotoUID}},
		{"saved search", `INSERT INTO saved_searches (uid, project_id, owner_uid, name, params)
			VALUES ($1, $2, $3, $4, $5::jsonb)`,
			[]any{tn.SavedSearchUID, tn.ProjectUID, tn.UserUID, "Hledání " + tn.Token, string(params)}},
		{"upload link", `INSERT INTO upload_links (uid, project_id, code_hash, code, title, created_by, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			[]any{tn.UploadLinkUID, tn.ProjectUID, uploadlink.HashSecret(tn.UploadLinkCode), tn.UploadLinkCode,
				"Odkaz " + tn.Token, tn.UserUID, time.Now().Add(uploadLinkLifetime)}},
	}
}

// MissingUID returns a well-formed uid with the two-letter prefix (for example
// "ph" for a photo) that names no row: the "missing" half of AssertSameShape.
// It panics only when the system random source fails.
func MissingUID(prefix string) string {
	uid, err := mintUID(prefix)
	if err != nil {
		panic(err)
	}
	return uid
}

// newUID returns a fresh uid with prefix, failing tb when the random source
// does.
func newUID(tb testing.TB, prefix string) string {
	tb.Helper()
	uid, err := mintUID(prefix)
	if err != nil {
		tb.Fatalf("projecttest: %v", err)
	}
	return uid
}

// mintUID returns prefix followed by uidSuffixLen random symbols of
// uidAlphabet — the shape of every uid Kukátko mints.
func mintUID(prefix string) (string, error) {
	buf := make([]byte, uidSuffixLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	for i, b := range buf {
		buf[i] = uidAlphabet[int(b)%len(uidAlphabet)]
	}
	return prefix + string(buf), nil
}

// randomHex returns n random bytes as lowercase hex, failing tb when the random
// source does.
func randomHex(tb testing.TB, n int) string {
	tb.Helper()
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		tb.Fatalf("projecttest: reading random bytes: %v", err)
	}
	return hex.EncodeToString(buf)
}
