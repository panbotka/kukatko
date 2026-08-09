package reset

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
)

// TestTableLists_areDisjointAndSorted verifies the two classifications do not
// overlap and stay in a readable order — a table in both lists would be
// truncated and claimed to be preserved at the same time.
func TestTableLists_areDisjointAndSorted(t *testing.T) {
	t.Parallel()

	catalogue := CatalogueTables()
	preserved := PreservedTables()
	for _, name := range catalogue {
		if slices.Contains(preserved, name) {
			t.Errorf("table %q is classified as both wiped and preserved", name)
		}
	}
	if !slices.IsSorted(catalogue) {
		t.Errorf("catalogueTables is not sorted: %v", catalogue)
	}
	if !slices.IsSorted(preserved) {
		t.Errorf("preservedTables is not sorted: %v", preserved)
	}
}

// TestPreservedTables_holdTheEssentials verifies the tables that must survive a
// wipe are all classified as preserved: the accounts and their credentials, the
// audit trail, the announcement and the migration history.
func TestPreservedTables_holdTheEssentials(t *testing.T) {
	t.Parallel()

	preserved := PreservedTables()
	for _, name := range []string{
		"users", "sessions", "api_tokens", "schema_migrations", "announcements", "audit_log",
	} {
		if !slices.Contains(preserved, name) {
			t.Errorf("table %q must be preserved by a reset, but is not in the list", name)
		}
	}
}

// TestCatalogueTables_holdTheLibrary verifies the tables named by the wipe's
// specification are all classified as catalogue tables.
func TestCatalogueTables_holdTheLibrary(t *testing.T) {
	t.Parallel()

	catalogue := CatalogueTables()
	for _, name := range []string{
		"photos", "photo_files", "albums", "album_photos", "labels", "photo_labels",
		"subjects", "markers", "faces", "face_detections", "embeddings", "photo_phashes",
		"photo_places", "photo_edits", "photo_comments", "import_runs", "import_failures", "jobs",
		"user_favorites", "user_ratings", "saved_searches", "face_rejections",
		"label_rejections", "duplicate_dismissals", "photoprism_aliases",
	} {
		if !slices.Contains(catalogue, name) {
			t.Errorf("table %q must be wiped by a reset, but is not in the list", name)
		}
	}
}

// TestCatalogueTables_isACopy verifies the accessors hand out a fresh slice, so a
// caller cannot mutate the list the truncation is built from.
func TestCatalogueTables_isACopy(t *testing.T) {
	t.Parallel()

	first := CatalogueTables()
	first[0] = "mutated"
	if CatalogueTables()[0] == "mutated" {
		t.Error("CatalogueTables returned the package's own slice")
	}
}

// TestClassifySchema verifies the drift guard: the exact classified set passes,
// an extra table or a missing one aborts with ErrSchemaDrift.
func TestClassifySchema(t *testing.T) {
	t.Parallel()

	full := append(CatalogueTables(), PreservedTables()...)
	tests := []struct {
		name    string
		actual  []string
		wantErr bool
		mention string
	}{
		{name: "exactly the classified set", actual: full},
		{
			name:    "an unclassified table",
			actual:  append(slices.Clone(full), "photo_books"),
			wantErr: true,
			mention: "photo_books",
		},
		{
			name:    "a classified table is missing",
			actual:  slices.Delete(slices.Clone(full), 0, 1),
			wantErr: true,
			mention: full[0],
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := classifySchema(tt.actual)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("classifySchema() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, ErrSchemaDrift) {
				t.Fatalf("classifySchema() = %v, want ErrSchemaDrift", err)
			}
			if !strings.Contains(err.Error(), tt.mention) {
				t.Errorf("error %q does not name %q", err, tt.mention)
			}
		})
	}
}

// TestCounts_rowsAndNonEmpty verifies the summary helpers: the total across the
// catalogue, and which tables still hold rows.
func TestCounts_rowsAndNonEmpty(t *testing.T) {
	t.Parallel()

	counts := Counts{
		Catalogue: []TableCount{{Table: "photos", Rows: 3}, {Table: "albums"}, {Table: "jobs", Rows: 4}},
		Preserved: []TableCount{{Table: "users", Rows: 2}},
	}
	if got := counts.Rows(); got != 7 {
		t.Errorf("Rows() = %d, want 7", got)
	}
	left := counts.NonEmpty()
	if len(left) != 2 || left[0].Table != "photos" || left[1].Table != "jobs" {
		t.Errorf("NonEmpty() = %+v, want photos and jobs", left)
	}
	if empty := (Counts{}); empty.Rows() != 0 || empty.NonEmpty() != nil {
		t.Errorf("zero Counts = %+v, want no rows and no leftovers", empty)
	}
}

// TestTargetFromConfig verifies the target is parsed out of the configured DSN,
// carries the configured bucket alongside it, and that a DSN naming no database
// is refused — "whatever the server defaults to" is not a target anyone chose to
// wipe.
func TestTargetFromConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dsn     string
		bucket  string
		want    Target
		wantErr error
	}{
		{
			name: "full dsn",
			dsn:  "postgres://kukatko:secret@db.example:5433/kukatko?sslmode=disable",
			want: Target{Host: "db.example", Port: 5433, Database: "kukatko"},
		},
		{
			name:   "with a bucket",
			dsn:    "postgres://kukatko:secret@db.example:5433/kukatko?sslmode=disable",
			bucket: "kukatko-dev",
			want:   Target{Host: "db.example", Port: 5433, Database: "kukatko", Bucket: "kukatko-dev"},
		},
		{
			name:    "no database",
			dsn:     "postgres://kukatko:secret@db.example:5432/?sslmode=disable",
			wantErr: ErrTargetMismatch,
		},
		{name: "unparseable", dsn: "://nope", wantErr: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := TargetFromConfig(tt.dsn, tt.bucket)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("TargetFromConfig(%q) error = %v, want %v", tt.dsn, err, tt.wantErr)
				}
			case tt.want == (Target{}):
				if err == nil {
					t.Fatalf("TargetFromConfig(%q) = %+v, want an error", tt.dsn, got)
				}
			default:
				if err != nil {
					t.Fatalf("TargetFromConfig(%q) error = %v", tt.dsn, err)
				}
				if got != tt.want {
					t.Errorf("TargetFromConfig(%q) = %+v, want %+v", tt.dsn, got, tt.want)
				}
			}
		})
	}
}

// TestTarget_String verifies the one line an operator reads before confirming:
// the bucket appears when there is one and is not implied when there is not.
func TestTarget_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target Target
		want   string
	}{
		{
			name:   "no bucket",
			target: Target{Host: "localhost", Port: 5432, Database: "kukatko"},
			want:   "localhost:5432/kukatko",
		},
		{
			name:   "with a bucket",
			target: Target{Host: "localhost", Port: 5432, Database: "kukatko", Bucket: "kukatko-dev"},
			want:   "localhost:5432/kukatko + bucket kukatko-dev",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.target.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestService_checkConfirmation verifies both typed confirmations are exact and
// independent: the database's name, and the bucket's when the store has one. The
// interesting cases are the crossed ones — the right database with the wrong
// bucket is precisely a dev deployment pointed at a production bucket — and a
// bucket typed at a store that has none, which means the operator believed they
// were emptying something this run cannot even reach.
func TestService_checkConfirmation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		target  Target
		opts    Options
		wantErr error
	}{
		{
			name:   "both match",
			target: Target{Database: "kukatko", Bucket: "kukatko-dev"},
			opts:   Options{Confirm: "kukatko", ConfirmBucket: "kukatko-dev"},
		},
		{
			name:   "no bucket configured, none typed",
			target: Target{Database: "kukatko"},
			opts:   Options{Confirm: "kukatko"},
		},
		{
			name:    "wrong database",
			target:  Target{Database: "kukatko", Bucket: "kukatko-dev"},
			opts:    Options{Confirm: "kukatko_test", ConfirmBucket: "kukatko-dev"},
			wantErr: ErrConfirmationMismatch,
		},
		{
			name:    "right database, wrong bucket",
			target:  Target{Database: "kukatko", Bucket: "kukatko-dev"},
			opts:    Options{Confirm: "kukatko", ConfirmBucket: "kotrzina-photos"},
			wantErr: ErrBucketConfirmationMismatch,
		},
		{
			name:    "bucket not typed at all",
			target:  Target{Database: "kukatko", Bucket: "kukatko-dev"},
			opts:    Options{Confirm: "kukatko"},
			wantErr: ErrBucketConfirmationMismatch,
		},
		{
			name:    "bucket typed at a store that has none",
			target:  Target{Database: "kukatko"},
			opts:    Options{Confirm: "kukatko", ConfirmBucket: "kotrzina-photos"},
			wantErr: ErrBucketConfirmationMismatch,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := &Service{target: tt.target}
			err := svc.checkConfirmation(tt.opts)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("checkConfirmation(%+v) = %v, want nil", tt.opts, err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("checkConfirmation(%+v) = %v, want %v", tt.opts, err, tt.wantErr)
			}
		})
	}
}

// TestOptions_concurrency verifies the parallel-deletion limit falls back to the
// default only for a non-positive value.
func TestOptions_concurrency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		set  int
		want int
	}{
		{name: "unset", want: defaultConcurrency},
		{name: "negative", set: -3, want: defaultConcurrency},
		{name: "explicit", set: 3, want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := (Options{Concurrency: tt.set}).concurrency(); got != tt.want {
				t.Errorf("concurrency() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestNew_panicsOnMissingCollaborators verifies a wiring bug is a panic at
// construction rather than a surprise while a destructive command is running.
func TestNew_panicsOnMissingCollaborators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "no pool", cfg: Config{Storage: &fakeStore{}, Target: Target{Database: "kukatko"}}},
		{name: "no storage", cfg: Config{Target: Target{Database: "kukatko"}}},
		{name: "no target database", cfg: Config{Storage: &fakeStore{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Error("New did not panic on an incomplete config")
				}
			}()
			New(tt.cfg)
		})
	}
}

// TestAuditEntry verifies the record of the wipe names the operator, the target
// and what was removed, and that it is attributed as a system action (a CLI run
// has no session to attribute it to).
func TestAuditEntry(t *testing.T) {
	t.Parallel()

	svc := &Service{target: Target{Host: "localhost", Port: 5432, Database: "kukatko"}}
	before := Counts{Catalogue: []TableCount{
		{Table: "photos", Rows: 20670}, {Table: "albums"}, {Table: "jobs", Rows: 3},
	}}
	entry := svc.auditEntry(
		Options{Operator: "pi@rpi", OrphanSweep: true},
		before,
		StorageResult{Deleted: 12, Missing: 4, Foreign: 1},
	)

	if entry.Action != audit.ActionLibraryReset {
		t.Errorf("Action = %q, want %q", entry.Action, audit.ActionLibraryReset)
	}
	if entry.ActorUID != "" {
		t.Errorf("ActorUID = %q, want empty (a CLI run is a system action)", entry.ActorUID)
	}
	if entry.TargetType != auditTargetType {
		t.Errorf("TargetType = %q, want %q", entry.TargetType, auditTargetType)
	}
	wantDetails := map[string]any{
		"operator": "pi@rpi", "database": "kukatko", "host": "localhost",
		"orphan_sweep": true, "rows_deleted": int64(20673),
		"objects_deleted": 12, "objects_missing": 4, "objects_foreign": 1,
	}
	for key, want := range wantDetails {
		if got := entry.Details[key]; got != want {
			t.Errorf("Details[%q] = %v, want %v", key, got, want)
		}
	}
	rows, ok := entry.Details["rows_by_table"].(map[string]any)
	if !ok {
		t.Fatalf("Details[rows_by_table] = %T, want map[string]any", entry.Details["rows_by_table"])
	}
	if rows["photos"] != int64(20670) || rows["jobs"] != int64(3) {
		t.Errorf("rows_by_table = %v, want photos and jobs counts", rows)
	}
	if _, empty := rows["albums"]; empty {
		t.Error("rows_by_table lists albums, which held no rows")
	}
}
