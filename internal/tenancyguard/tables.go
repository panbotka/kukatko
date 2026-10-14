// Package tenancyguard is the static half of tenant isolation: it finds the SQL
// statements in the Go sources that touch a catalogue root table without naming
// the project they belong to. Its test runs inside `make check`, so a future
// query that forgets its `project_id` predicate fails the gate instead of
// leaking one project's rows into another.
//
// The scan is deliberately textual. It folds every string expression it can
// see — literals, `+` concatenations, package-level constants, `fmt.Sprintf`
// and `strings.Join` — into one statement text, finds the root tables named
// after FROM/JOIN/UPDATE/INTO, and asks whether the same statement names the
// table's project column in a position that constrains or writes the row: after
// WHERE, in a JOIN … ON condition, an INSERT column list, an UPDATE … SET list
// or an ON CONFLICT target, at any subquery depth. A project column only in a
// select list (or ORDER BY, RETURNING…) scopes nothing. Legitimate
// instance-wide statements (worker loaders, maintenance, metrics, reset,
// backup) are listed in allow.txt with a reason.
package tenancyguard

// RootTables is the one list of the catalogue's root tables: every table whose
// rows belong to exactly one project, mapped to the column that names it. A
// statement over one of them must name that column in a predicate position
// (see predicateColumns). The repository test checks this map (together with
// ExemptTables) against the migrations, so a new root table cannot be
// forgotten here.
var RootTables = map[string]string{
	"photos":             "project_id",
	"subjects":           "project_id",
	"albums":             "project_id",
	"labels":             "project_id",
	"face_clusters":      "project_id",
	"subject_families":   "project_id",
	"photo_tasks":        "project_id",
	"upload_links":       "project_id",
	"import_runs":        "project_id",
	"saved_searches":     "project_id",
	"notifications":      "project_id",
	"embeddings":         "project_id",
	"faces":              "project_id",
	"search_history":     "project_id",
	"instance_settings":  "project_id",
	"announcements":      "project_id",
	"project_invites":    "project_uid",
	"photoprism_aliases": "project_id",
}

// ExemptTables are the tables that carry a project column but are not catalogue
// roots, each with the reason the guard leaves its statements alone. They are
// listed (rather than silently ignored) so the migration cross-check still
// accounts for every project column in the schema.
var ExemptTables = map[string]string{
	"audit_log": "the trail is instance-wide; project_id is nullable and absent on account " +
		"and maintenance actions",
	"project_members": "account table: a user's memberships are read across projects by design " +
		"(sign-in, the project switcher)",
	"storage_rekey_runs": "maintainer-only run state of the storage move, addressed by the run's own uid",
	"storage_rekey_derived_failures": "maintainer-only bookkeeping of the storage move, keyed by the " +
		"photo's uid; internal/rekey reads it per project",
}

// projectColumns are the column names that tie a row to a project.
var projectColumns = []string{"project_id", "project_uid"}
