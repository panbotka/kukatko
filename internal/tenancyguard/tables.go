// Package tenancyguard is the static half of tenant isolation: it finds the SQL
// statements in the Go sources that touch a catalogue root table — or read a
// read-scoped or keyed child table such as audit_log or photo_phashes —
// without tying it to the project it belongs to. Its test runs inside `make
// check`, so a future query that forgets its `project_id` predicate fails the
// gate instead of leaking one project's rows into another.
//
// The scan is deliberately textual. It folds every string expression it can
// see — literals, `+` concatenations, package-level constants, `fmt.Sprintf`
// and `strings.Join` — into one statement text, blanks its SQL comments, and
// walks it with a small parser (unscopedRefs): every root table named after
// FROM/JOIN/USING/UPDATE/INTO, with its alias, must be tied to one project by
// its own predicate — a top-level AND term `<alias>.project_id = <value or
// another scoped alias's project_id>` of a WHERE or JOIN … ON (never under OR
// or NOT, never a NULL test), or its project column in an INSERT's own column
// list. A project column in a select list, SET list, ORDER BY or comment
// scopes nothing, and a predicate on one alias scopes no other. A finding is
// anchored at the statement's use site: the line naming a shared constant,
// so one allow-list entry never covers a second use of it. Legitimate
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
// caller resolves inside a project (a photo, a subject, an album, a label — or
// the row's own uid, handed out by a project-scoped listing). A read of one
// (FROM or JOIN, other than a DELETE's own FROM) must constrain it — in a
// predicate position, like a root table — by a project column (the join to its
// parent filtered on the project) or by one of those keys.
// What the guard catches is the whole-table scan: a neighbour search such as
// "the nearest pHash of all" reads every project's rows and names none of them.
//
// The parent key is a heuristic, not a proof: `WHERE photo_uid = ANY($1)` is
// accepted however the caller built $1, and a JOIN to the parent on that key
// counts as keyed (the parent root table is then judged on its own). Writes are
// not judged — a child row is written for a parent the caller already holds.
var KeyedChildTables = map[string][]string{
	"photo_phashes":        {"photo_uid"},
	"photo_files":          {"photo_uid"},
	"photo_edits":          {"photo_uid"},
	"photo_places":         {"photo_uid"},
	"photo_hls_renditions": {"photo_uid"},
	"face_detections":      {"photo_uid"},
	"markers":              {"photo_uid", "subject_uid", "uid"},
	"photo_labels":         {"photo_uid", "label_uid"},
	"album_photos":         {"photo_uid", "album_uid"},
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
