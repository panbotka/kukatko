// Package tenancyguard is the static half of tenant isolation: it finds the SQL
// statements in the Go sources that touch a catalogue root table — or read a
// read-scoped or keyed child table such as audit_log or photo_phashes —
// without tying it to the project it belongs to. Its test runs inside `make
// check`, so a future query that forgets its `project_id` predicate fails the
// gate instead of leaking one project's rows into another.
//
// The scan is deliberately textual. It folds every string expression it can
// see — literals, `+` concatenations, package-level constants, `fmt.Sprintf`,
// `strings.Join` and calls of the package's own one-line SQL helpers — into
// one statement text, blanks its SQL comments, and walks it with a small
// parser (unscopedRefs): every root table named after FROM/JOIN/USING/UPDATE/
// INTO, with its alias, must be tied to one project by its own predicate — a
// top-level AND term `<alias>.project_id = <value or another scoped alias's
// project_id>` of a WHERE or JOIN … ON (OR binding looser than AND; never
// under OR, NOT or CASE, never a NULL test, the value naming no column), or
// its project column in an INSERT's own column list. A project column in a
// select list, SET list, ORDER BY or comment scopes nothing, a predicate on
// one alias scopes no other, and a piece the scan cannot read scopes nothing
// whatever it is called. A finding is anchored at the statement's use site:
// the line naming a shared constant, so one allow-list entry never covers a
// second use of it. Legitimate instance-wide statements (worker loaders,
// maintenance, metrics, reset, backup) are listed in allow.txt with a reason.
package tenancyguard

import "slices"

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

// ReadScopedTables are the tables whose reads must name the project but whose
// writes need not, mapped to the column that names it. Their project column is
// nullable on purpose — an instance-wide row (an account or maintenance action
// in the audit trail) belongs to no project, so an INSERT, UPDATE or DELETE
// without one is legitimate — but every row that does carry a project is that
// project's data, so a statement that reads the table (FROM or JOIN, other than
// a DELETE's own FROM) must name the column in a predicate exactly like a root
// table, or be allow-listed with a reason.
var ReadScopedTables = map[string]string{
	"audit_log": "project_id",
}

// KeyedChildTables are the child tables of a root table that carry no project
// column of their own, mapped to the columns that key a row to a parent the
// caller resolves inside a project (a photo, a subject, an album, a task, a
// link — or, for markers and comments, the row's own uid, handed out by a
// project-scoped listing). A read of one (FROM or JOIN, other than a DELETE's
// own FROM) must be keyed by a top-level AND term `<key> = <value>` (an
// `= ANY(…)` set included), `<key> IN (<values or a subquery>)`, or `<key> =
// <a column of another item>` that item being scoped itself (a root table by
// its project, a child by its own key) or none of the guard's tables (a CTE, a
// derived table, unnest) — or be allow-listed. Any other comparison (`<>`,
// LIKE, NOT IN, a NULL test) keys nothing. What the guard catches is the
// whole-table scan: a neighbour search such as "the nearest pHash of all"
// reads every project's rows and names none of them.
//
// The key is a heuristic, not a proof: `WHERE photo_uid = ANY($1)` is accepted
// however the caller built $1. Writes are not judged — a child row is written
// for a parent the caller already holds.
//
// The list is the schema's, not a choice: TestKeyedChildTables_matchMigrations
// derives from the migrations every table without a project column that has a
// foreign key into a root table (or into another such child), and fails
// unless it is listed here with exactly those foreign-key columns as its keys
// (plus `uid` where its own uid is handed out).
var KeyedChildTables = map[string][]string{
	"album_photos":                {"album_uid", "photo_uid"},
	"comment_mentions":            {"comment_uid"},
	"comments":                    {"album_uid", "photo_uid", "task_uid", "uid"},
	"duplicate_confirmations":     {"other_uid", "photo_uid"},
	"duplicate_dismissals":        {"other_uid", "photo_uid"},
	"duplicate_marker_dismissals": {"photo_uid", "subject_uid"},
	"face_confirmations":          {"photo_uid", "subject_uid"},
	"face_detections":             {"photo_uid"},
	"face_rejections":             {"photo_uid", "subject_uid"},
	"import_failures":             {"run_id"},
	"label_rejections":            {"label_uid", "photo_uid"},
	"markers":                     {"photo_uid", "subject_uid", "uid"},
	"notification_photos":         {"notification_uid", "photo_uid"},
	"photo_edits":                 {"photo_uid"},
	"photo_files":                 {"photo_uid"},
	"photo_hls_renditions":        {"photo_uid"},
	"photo_labels":                {"label_uid", "photo_uid"},
	"photo_phashes":               {"photo_uid"},
	"photo_places":                {"photo_uid"},
	"photo_task_participants":     {"task_uid"},
	"photo_task_photos":           {"photo_uid", "task_uid"},
	"review_skips":                {"photo_uid", "subject_uid"},
	"subject_family_children":     {"child_uid", "family_uid"},
	"tag_notices":                 {"photo_uid"},
	"upload_link_albums":          {"album_uid", "link_uid"},
	"upload_link_labels":          {"label_uid", "link_uid"},
	"upload_link_photos":          {"link_uid", "photo_uid"},
	"user_favorites":              {"photo_uid"},
	"user_ratings":                {"photo_uid"},
}

// ChildTables derives the keyed child tables from the schema: given each
// table's foreign keys (ForeignKeys) and the tables with a project column
// (ProjectColumnTables), it returns every table without a project column that
// has a foreign key into a root table or into another such child — mapped to
// those foreign-key columns, sorted. A key into an account table (users) or
// into projects makes no child.
func ChildTables(fks map[string]map[string]string, projectCols map[string]string) map[string][]string {
	children := make(map[string][]string)
	parent := func(table string) bool {
		if _, ok := RootTables[table]; ok {
			return true
		}
		_, ok := children[table]
		return ok
	}
	for changed := true; changed; {
		changed = false
		for table, cols := range fks {
			if _, ok := projectCols[table]; ok {
				continue
			}
			if keys := parentKeys(cols, parent); len(keys) > 0 && !slices.Equal(keys, children[table]) {
				children[table], changed = keys, true
			}
		}
	}
	return children
}

// parentKeys returns, sorted, the foreign-key columns of cols whose
// referenced table is a parent.
func parentKeys(cols map[string]string, parent func(table string) bool) []string {
	var keys []string
	for col, ref := range cols {
		if parent(ref) {
			keys = append(keys, col)
		}
	}
	slices.Sort(keys)
	return keys
}

// ExemptTables are the tables that carry a project column but are not catalogue
// roots, each with the reason the guard leaves its statements alone. They are
// listed (rather than silently ignored) so the migration cross-check still
// accounts for every project column in the schema.
var ExemptTables = map[string]string{
	"project_members": "account table: a user's memberships are read across projects by design " +
		"(sign-in, the project switcher)",
	"storage_rekey_runs": "maintainer-only run state of the storage move, addressed by the run's own uid",
	"storage_rekey_derived_failures": "maintainer-only bookkeeping of the storage move, keyed by the " +
		"photo's uid; internal/rekey reads it per project",
}

// projectColumns are the column names that tie a row to a project.
var projectColumns = []string{"project_id", "project_uid"}
