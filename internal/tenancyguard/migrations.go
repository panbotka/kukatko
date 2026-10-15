package tenancyguard

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

var (
	// sqlLineComment matches a `--` comment up to the end of its line.
	sqlLineComment = regexp.MustCompile(`--[^\n]*`)
	// alterTable captures the table an ALTER TABLE statement changes.
	alterTable = regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?(\w+)`)
	// addProjectColumn captures a project column added by ALTER TABLE.
	addProjectColumn = regexp.MustCompile(`(?i)\bADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?(project_id|project_uid)\b`)
	// dropProjectColumn captures a project column dropped by ALTER TABLE.
	dropProjectColumn = regexp.MustCompile(`(?i)\bDROP\s+COLUMN\s+(?:IF\s+EXISTS\s+)?(project_id|project_uid)\b`)
	// createTable captures the table a CREATE TABLE statement creates.
	createTable = regexp.MustCompile(`(?is)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)\s*\(`)
	// columnDef captures a project column declared in a CREATE TABLE body: the
	// name at the start of a column definition, followed by its type.
	columnDef = regexp.MustCompile(`(?i)(?:^|[(,])\s*(project_id|project_uid)\s+[a-z]`)
	// dropTable captures the tables a DROP TABLE statement removes.
	dropTable = regexp.MustCompile(`(?is)^\s*DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?([\w\s,]+?)\s*(?:CASCADE|RESTRICT)?\s*$`)
)

// ProjectColumnTables replays the migrations in fsys (every *.sql file directly
// under dir, in name order) and returns the tables that end up with a project
// column, mapped to that column's name. It understands the forms the schema
// uses: `ALTER TABLE … ADD COLUMN project_id …`, a `project_id`/`project_uid`
// column inside `CREATE TABLE`, and the DROP COLUMN / DROP TABLE that would
// take one away again. It returns an error if dir cannot be read.
func ProjectColumnTables(fsys fs.FS, dir string) (map[string]string, error) {
	srcs, err := migrationSources(fsys, dir)
	if err != nil {
		return nil, err
	}
	tables := make(map[string]string)
	for _, src := range srcs {
		applyMigration(tables, src)
	}
	return tables, nil
}

// migrationSources reads every *.sql file directly under dir of fsys, in name
// order. It returns an error if dir or a file cannot be read.
func migrationSources(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	srcs := make([]string, 0, len(names))
	for _, name := range names {
		src, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		srcs = append(srcs, string(src))
	}
	return srcs, nil
}

// applyMigration folds the project-column effects of one migration's SQL into
// tables, statement by statement.
func applyMigration(tables map[string]string, src string) {
	src = sqlLineComment.ReplaceAllString(src, "")
	for stmt := range strings.SplitSeq(src, ";") {
		applyStatement(tables, stmt)
	}
}

// applyStatement folds one SQL statement's effect on project columns into
// tables; statements that create, alter or drop nothing relevant are ignored.
func applyStatement(tables map[string]string, stmt string) {
	if m := createTable.FindStringSubmatchIndex(stmt); m != nil {
		table := strings.ToLower(stmt[m[2]:m[3]])
		if c := columnDef.FindStringSubmatch(stmt[m[1]:]); c != nil {
			tables[table] = strings.ToLower(c[1])
		}
		return
	}
	if m := alterTable.FindStringSubmatch(stmt); m != nil {
		table := strings.ToLower(m[1])
		if c := addProjectColumn.FindStringSubmatch(stmt); c != nil {
			tables[table] = strings.ToLower(c[1])
		}
		if c := dropProjectColumn.FindStringSubmatch(stmt); len(c) == 2 && strings.EqualFold(tables[table], c[1]) {
			delete(tables, table)
		}
		return
	}
	if m := dropTable.FindStringSubmatch(stmt); m != nil {
		for table := range strings.SplitSeq(m[1], ",") {
			delete(tables, strings.ToLower(strings.TrimSpace(table)))
		}
	}
}

// fkRef captures the table a REFERENCES clause points at.
var fkRef = regexp.MustCompile(`(?i)\bREFERENCES\s+(\w+)`)

// fkConstraint captures a table constraint `[CONSTRAINT name] FOREIGN KEY
// (cols) REFERENCES table`.
var fkConstraint = regexp.MustCompile(`(?is)^(?:CONSTRAINT\s+\w+\s+)?FOREIGN\s+KEY\s*\(([^)]*)\)\s*REFERENCES\s+(\w+)`)

// columnWord captures the column a column definition (or ADD COLUMN) declares.
var columnWord = regexp.MustCompile(`(?i)^(?:ADD\s+(?:COLUMN\s+)?(?:IF\s+NOT\s+EXISTS\s+)?)?"?(\w+)"?`)

// dropColumn captures the column a DROP COLUMN action removes.
var dropColumn = regexp.MustCompile(`(?i)^DROP\s+(?:COLUMN\s+)?(?:IF\s+EXISTS\s+)?(\w+)`)

// renameTable and renameColumn capture ALTER TABLE's RENAME actions.
var (
	renameTable  = regexp.MustCompile(`(?i)^RENAME\s+TO\s+(\w+)`)
	renameColumn = regexp.MustCompile(`(?i)^RENAME\s+(?:COLUMN\s+)?(\w+)\s+TO\s+(\w+)`)
)

// ForeignKeys replays the migrations in fsys (every *.sql file directly under
// dir, in name order) and returns, per table, the columns that reference
// another table, mapped to that table. It understands a column's inline
// `REFERENCES t` (in CREATE TABLE or ALTER TABLE … ADD COLUMN), a `[CONSTRAINT
// n] FOREIGN KEY (col) REFERENCES t` table constraint (in CREATE TABLE or
// ALTER TABLE … ADD), and the DROP COLUMN, RENAME, RENAME COLUMN and DROP
// TABLE that move or remove one. It returns an error if dir cannot be read.
func ForeignKeys(fsys fs.FS, dir string) (map[string]map[string]string, error) {
	srcs, err := migrationSources(fsys, dir)
	if err != nil {
		return nil, err
	}
	fks := make(map[string]map[string]string)
	for _, src := range srcs {
		for stmt := range strings.SplitSeq(sqlLineComment.ReplaceAllString(src, ""), ";") {
			applyForeignKeys(fks, stmt)
		}
	}
	return fks, nil
}

// applyForeignKeys folds one statement's effect on foreign keys into fks.
func applyForeignKeys(fks map[string]map[string]string, stmt string) {
	if m := createTable.FindStringSubmatchIndex(stmt); m != nil {
		table := strings.ToLower(stmt[m[2]:m[3]])
		delete(fks, table)
		body := stmt[m[1]:]
		if end := strings.LastIndexByte(body, ')'); end >= 0 {
			body = body[:end]
		}
		for _, item := range splitTopLevel(body) {
			addForeignKey(fks, table, item)
		}
		return
	}
	if m := alterTable.FindStringSubmatchIndex(stmt); m != nil {
		table := strings.ToLower(stmt[m[2]:m[3]])
		for _, action := range splitTopLevel(stmt[m[1]:]) {
			table = alterForeignKeys(fks, table, action)
		}
		return
	}
	if m := dropTable.FindStringSubmatch(stmt); m != nil {
		for table := range strings.SplitSeq(m[1], ",") {
			delete(fks, strings.ToLower(strings.TrimSpace(table)))
		}
	}
}

// alterForeignKeys applies one ALTER TABLE action to table's foreign keys and
// returns the table's name after it (a RENAME TO changes it).
func alterForeignKeys(fks map[string]map[string]string, table, action string) string {
	if m := renameTable.FindStringSubmatch(action); m != nil {
		to := strings.ToLower(m[1])
		if cols, ok := fks[table]; ok {
			fks[to] = cols
			delete(fks, table)
		}
		return to
	}
	switch {
	case renameColumn.MatchString(action):
		m := renameColumn.FindStringSubmatch(action)
		from, to := strings.ToLower(m[1]), strings.ToLower(m[2])
		if ref, ok := fks[table][from]; ok {
			fks[table][to] = ref
			delete(fks[table], from)
		}
	case dropColumn.MatchString(action) && !strings.HasPrefix(strings.ToUpper(action), "DROP CONSTRAINT"):
		delete(fks[table], strings.ToLower(dropColumn.FindStringSubmatch(action)[1]))
	case len(action) > 3 && strings.EqualFold(action[:3], "ADD"):
		addConstraintOrColumn(fks, table, action)
	}
	return table
}

// addConstraintOrColumn applies an ALTER TABLE … ADD action: a FOREIGN KEY
// table constraint, or a column with an inline REFERENCES.
func addConstraintOrColumn(fks map[string]map[string]string, table, action string) {
	rest := strings.TrimSpace(action[3:])
	if fkConstraint.MatchString(rest) {
		addForeignKey(fks, table, rest)
	} else if fkRef.MatchString(rest) {
		addForeignKey(fks, table, action)
	}
}

// addForeignKey records the foreign keys of one CREATE TABLE item or ALTER
// TABLE … ADD action: a FOREIGN KEY table constraint, or a column definition
// with an inline REFERENCES.
func addForeignKey(fks map[string]map[string]string, table, item string) {
	set := func(col, ref string) {
		if fks[table] == nil {
			fks[table] = make(map[string]string)
		}
		fks[table][strings.ToLower(strings.TrimSpace(col))] = strings.ToLower(ref)
	}
	if m := fkConstraint.FindStringSubmatch(item); m != nil {
		for col := range strings.SplitSeq(m[1], ",") {
			set(col, m[2])
		}
		return
	}
	ref := fkRef.FindStringSubmatch(item)
	col := columnWord.FindStringSubmatch(item)
	if ref == nil || col == nil {
		return
	}
	switch strings.ToUpper(col[1]) {
	case "CONSTRAINT", "PRIMARY", "UNIQUE", "CHECK", "EXCLUDE":
		return
	}
	set(col[1], ref[1])
}

// splitTopLevel splits s on the commas outside parentheses and returns the
// trimmed, non-empty parts.
func splitTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	add := func(part string) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	for i := range len(s) {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				add(s[start:i])
				start = i + 1
			}
		}
	}
	add(s[start:])
	return out
}
