package reset

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// catalogueTables is every table the wipe empties: the photo catalogue itself,
// everything hanging off it (files, vectors, faces, people, albums, labels,
// places, edits), the per-user curation of those photos, the import bookkeeping
// and the job queue that would otherwise keep working on photos that no longer
// exist.
//
// It is an explicit list rather than "every table except the preserved ones",
// because the failure modes of the two are not symmetric. A table this list
// forgets survives the wipe — visible, and fixed by adding it. A table an
// exclusion list forgets is destroyed — and users, sessions or the audit trail
// are exactly the tables that must not be. The list is checked against the live
// schema before anything is deleted (see checkSchema), so forgetting one aborts
// the reset instead of quietly half-wiping the library.
//
// The names are also the TRUNCATE argument list. Every foreign key between them
// stays inside the list, which is what lets the truncation run without CASCADE:
// a CASCADE would silently pull in whatever else happens to reference a
// catalogue table, and "silently pull in" is the one thing this command must
// never do.
var catalogueTables = []string{
	"album_photos",
	"albums",
	"duplicate_dismissals",
	"duplicate_marker_dismissals",
	"embeddings",
	"face_clusters",
	"face_confirmations",
	"face_detections",
	"face_rejections",
	"faces",
	"import_failures",
	"import_runs",
	"jobs",
	"label_rejections",
	"labels",
	"markers",
	"photo_edits",
	"photo_files",
	"photo_labels",
	"photo_phashes",
	"photo_places",
	"photoprism_aliases",
	"photos",
	"saved_searches",
	"subjects",
	"user_favorites",
	"user_ratings",
}

// preservedTables is every table the wipe must leave exactly as it found it.
//
// The accounts and their credentials (users, sessions, api_tokens) stay, because
// a wipe that locks the operator out of the instance they just wiped is not a
// reset but an outage. schema_migrations stays, because the schema is not being
// rebuilt — dropping it would make the next start re-apply every migration.
// announcements stays, because the banner describes the instance, not the
// library. And audit_log stays, because the record of the deletion is the only
// thing left to read afterwards; the reset writes its own entry into it, in the
// same transaction as the truncation.
var preservedTables = []string{
	"announcements",
	"api_tokens",
	"audit_log",
	"schema_migrations",
	"sessions",
	"users",
}

// CatalogueTables returns, in a fresh slice, the tables a reset empties.
func CatalogueTables() []string {
	return slices.Clone(catalogueTables)
}

// PreservedTables returns, in a fresh slice, the tables a reset must never touch.
func PreservedTables() []string {
	return slices.Clone(preservedTables)
}

// TableCount is one table's row count at a point in time.
type TableCount struct {
	// Table is the unqualified table name.
	Table string `json:"table"`
	// Rows is the number of rows the table held when it was counted.
	Rows int64 `json:"rows"`
}

// Counts is a snapshot of the whole schema, split the way the reset treats it:
// what it deletes and what it protects. Both halves are counted before and after
// a run, so the summary can prove the first went to zero and the second did not
// move.
type Counts struct {
	// Catalogue holds the row counts of the tables the reset empties.
	Catalogue []TableCount `json:"catalogue"`
	// Preserved holds the row counts of the tables the reset must not touch.
	Preserved []TableCount `json:"preserved"`
}

// Rows returns the total number of rows across the catalogue tables.
func (c Counts) Rows() int64 {
	var total int64
	for _, table := range c.Catalogue {
		total += table.Rows
	}
	return total
}

// NonEmpty returns the catalogue tables that still hold rows. After a completed
// reset it must be empty; anything in it is a table that was counted but not
// truncated, which is a bug in the table list rather than an acceptable leftover.
func (c Counts) NonEmpty() []TableCount {
	var left []TableCount
	for _, table := range c.Catalogue {
		if table.Rows > 0 {
			left = append(left, table)
		}
	}
	return left
}

// countTables returns the row count of every table in names, in the order given.
// The names come from the package's own hardcoded lists, never from input, so
// they are safe to interpolate — they are still quoted through pgx.Identifier so
// the statement cannot be misread.
func countTables(ctx context.Context, pool *pgxpool.Pool, names []string) ([]TableCount, error) {
	counts := make([]TableCount, 0, len(names))
	for _, name := range names {
		query := "SELECT count(*) FROM " + pgx.Identifier{name}.Sanitize()
		var rows int64
		if err := pool.QueryRow(ctx, query).Scan(&rows); err != nil {
			return nil, fmt.Errorf("reset: counting rows in %s: %w", name, err)
		}
		counts = append(counts, TableCount{Table: name, Rows: rows})
	}
	return counts, nil
}

// listPublicTables returns the sorted names of every base table in the public
// schema of the connected database.
func listPublicTables(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	const query = `SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename`
	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("reset: listing public tables: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("reset: scanning table name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reset: iterating table names: %w", err)
	}
	return names, nil
}

// classifySchema compares the tables the database actually holds with the two
// lists this package classifies, and returns ErrSchemaDrift describing the
// difference when they disagree.
//
// This is the guard against the wipe going stale. A migration that adds a table
// nobody classified would otherwise leave it behind on every reset — a table full
// of the library that was supposed to be gone, discovered much later. Refusing to
// run is the safe answer: the fix is one line in catalogueTables or
// preservedTables, and until it is made, no half-wipe happens.
func classifySchema(actual []string) error {
	known := make(map[string]bool, len(catalogueTables)+len(preservedTables))
	for _, name := range catalogueTables {
		known[name] = true
	}
	for _, name := range preservedTables {
		known[name] = true
	}

	present := make(map[string]bool, len(actual))
	var unclassified []string
	for _, name := range actual {
		present[name] = true
		if !known[name] {
			unclassified = append(unclassified, name)
		}
	}
	var missing []string
	for name := range known {
		if !present[name] {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)

	return schemaDriftError(unclassified, missing)
}

// schemaDriftError renders the drift found by classifySchema, or nil when there
// is none.
func schemaDriftError(unclassified, missing []string) error {
	var parts []string
	if len(unclassified) > 0 {
		parts = append(parts, "unclassified table(s) "+strings.Join(unclassified, ", ")+
			" — add them to catalogueTables (wiped) or preservedTables (kept) in internal/reset")
	}
	if len(missing) > 0 {
		parts = append(parts, "classified table(s) "+strings.Join(missing, ", ")+
			" do not exist in the database — is this a Kukátko database, and are its migrations applied?")
	}
	if len(parts) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrSchemaDrift, strings.Join(parts, "; "))
}
