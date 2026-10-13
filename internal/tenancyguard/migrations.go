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
	tables := make(map[string]string)
	for _, name := range names {
		src, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		applyMigration(tables, string(src))
	}
	return tables, nil
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
