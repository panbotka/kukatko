package inventory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/storage"
)

// absentMark is written in place of a digest for a digested table the schema
// does not have, so the diff reports it instead of the collector failing.
const absentMark = "absent"

// Collect writes a snapshot of the database behind pool into dir (created if
// needed): counts, columns, digests, photos and users, and — when store is not
// nil — the storage check. Everything is read inside one READ ONLY REPEATABLE
// READ transaction, so the files describe a single instant even on a live
// instance, and the session runs in UTC so timestamps render identically
// wherever the snapshot is taken. Collect never writes to the database.
func Collect(ctx context.Context, pool *pgxpool.Pool, dir string, store storage.SizedKeyLister) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("inventory: creating %s: %w", dir, err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("inventory: beginning read-only transaction: %w", err)
	}
	// Read-only: there is nothing to keep, so rolling back is the clean end.
	defer func() { _ = tx.Rollback(ctx) }()
	files, err := collectFiles(ctx, tx, store)
	if err != nil {
		return err
	}
	for name, rows := range files {
		if err := writeTSV(filepath.Join(dir, name), rows); err != nil {
			return err
		}
	}
	return nil
}

// collectFiles reads every file of the snapshot inside tx, keyed by file name;
// the storage check runs only when store is not nil.
func collectFiles(ctx context.Context, tx pgx.Tx, store storage.SizedKeyLister) (map[string][][]string, error) {
	if _, err := tx.Exec(ctx, "SET LOCAL TIME ZONE 'UTC'"); err != nil {
		return nil, fmt.Errorf("inventory: pinning the time zone: %w", err)
	}
	schema, err := readSchema(ctx, tx)
	if err != nil {
		return nil, err
	}
	files := map[string][][]string{ColumnsFile: schema.columnRows()}
	if files[CountsFile], err = collectCounts(ctx, tx, schema); err != nil {
		return nil, err
	}
	if files[DigestsFile], err = collectDigests(ctx, tx, schema); err != nil {
		return nil, err
	}
	if err := collectRosters(ctx, tx, files); err != nil {
		return nil, err
	}
	if store != nil {
		if files[StorageMissingFile], err = checkStorage(ctx, tx, store); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// schemaInfo is the shape of the current schema: every base table and, per
// table, its columns with their type name.
type schemaInfo struct {
	tables  []string
	columns map[string]map[string]string
}

// readSchema loads the tables and columns of the connection's current schema.
func readSchema(ctx context.Context, tx pgx.Tx) (schemaInfo, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.table_name, c.column_name, c.udt_name
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = current_schema() AND t.table_type = 'BASE TABLE'`)
	if err != nil {
		return schemaInfo{}, fmt.Errorf("inventory: reading the schema: %w", err)
	}
	info := schemaInfo{columns: map[string]map[string]string{}}
	var table, column, udt string
	_, err = pgx.ForEachRow(rows, []any{&table, &column, &udt}, func() error {
		if info.columns[table] == nil {
			info.columns[table] = map[string]string{}
			info.tables = append(info.tables, table)
		}
		info.columns[table][column] = udt
		return nil
	})
	if err != nil {
		return schemaInfo{}, fmt.Errorf("inventory: reading the schema: %w", err)
	}
	slices.Sort(info.tables)
	return info, nil
}

// columnRows renders the schema as table, column, type rows.
func (s schemaInfo) columnRows() [][]string {
	var out [][]string
	for table, cols := range s.columns {
		for column, udt := range cols {
			out = append(out, []string{table, column, udt})
		}
	}
	return out
}

// collectCounts counts the rows of every table.
func collectCounts(ctx context.Context, tx pgx.Tx, schema schemaInfo) ([][]string, error) {
	out := make([][]string, 0, len(schema.tables))
	for _, table := range schema.tables {
		var n int64
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&n); err != nil {
			return nil, fmt.Errorf("inventory: counting %s: %w", table, err)
		}
		out = append(out, []string{table, strconv.FormatInt(n, 10)})
	}
	return out, nil
}

// collectDigests digests every allow-listed table: table, row count, digest and
// the allow-listed columns the schema lacks (comma-separated, "-" for none).
func collectDigests(ctx context.Context, tx pgx.Tx, schema schemaInfo) ([][]string, error) {
	out := make([][]string, 0, len(digestSpecs))
	for _, spec := range digestSpecs {
		cols, ok := schema.columns[spec.table]
		if !ok {
			out = append(out, []string{spec.table, "0", absentMark, "-"})
			continue
		}
		query, missing := digestQuery(spec, cols)
		var n int64
		var digest string
		if err := tx.QueryRow(ctx, query).Scan(&n, &digest); err != nil {
			return nil, fmt.Errorf("inventory: digesting %s: %w", spec.table, err)
		}
		out = append(out, []string{spec.table, strconv.FormatInt(n, 10), digest, joinOrDash(missing)})
	}
	return out, nil
}

// digestQuery builds the digest statement for spec against the table's actual
// columns, returning it with the allow-listed columns the table lacks. Each row
// is projected to its allow-listed columns (large types as md5 of their text),
// rendered with row_to_json, hashed, and the row hashes are concatenated in key
// order (text keys under COLLATE "C") and hashed once more — the same result as
// hashing the concatenated rows, in bounded memory. An empty table digests to
// the md5 of the empty string.
func digestQuery(spec digestSpec, cols map[string]string) (string, []string) {
	var exprs, missing []string
	for _, col := range spec.columns {
		udt, ok := cols[col]
		ident := pgx.Identifier{col}.Sanitize()
		switch {
		case !ok:
			missing = append(missing, col)
		case hashedTypes[udt]:
			exprs = append(exprs, "md5("+ident+"::text) AS "+ident)
		default:
			exprs = append(exprs, ident)
		}
	}
	order := make([]string, 0, len(spec.orderBy))
	for _, col := range spec.orderBy {
		expr := "t." + pgx.Identifier{col}.Sanitize()
		if textTypes[cols[col]] {
			expr += ` COLLATE "C"`
		}
		order = append(order, expr)
	}
	query := fmt.Sprintf(
		"SELECT count(*), md5(coalesce(string_agg(md5(row_to_json(t)::text), '' ORDER BY %s), '')) "+
			"FROM (SELECT %s FROM %s) t",
		strings.Join(order, ", "), strings.Join(exprs, ", "), pgx.Identifier{spec.table}.Sanitize())
	return query, missing
}

// collectRosters reads the photo and account rosters into files.
func collectRosters(ctx context.Context, tx pgx.Tx, files map[string][][]string) error {
	var err error
	files[PhotosFile], err = queryRows(ctx, tx, `
		SELECT uid, file_hash, file_path, file_size::text, coalesce(archived_at::text, '')
		FROM photos`)
	if err != nil {
		return fmt.Errorf("inventory: listing photos: %w", err)
	}
	files[UsersFile], err = queryRows(ctx, tx, `SELECT uid, username, role, disabled::text FROM users`)
	if err != nil {
		return fmt.Errorf("inventory: listing users: %w", err)
	}
	return nil
}

// queryRows runs a query whose columns are all text and returns its rows.
func queryRows(ctx context.Context, tx pgx.Tx, query string) ([][]string, error) {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, err //nolint:wrapcheck // the caller wraps with what was being listed
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err //nolint:wrapcheck // the caller wraps with what was being listed
		}
		row := make([]string, len(values))
		for i, v := range values {
			row[i] = fmt.Sprint(v)
		}
		out = append(out, row)
	}
	return out, rows.Err() //nolint:wrapcheck // the caller wraps with what was being listed
}

// expectedOriginal is one catalogue original the storage check looks for.
type expectedOriginal struct {
	uid  string
	size int64
	seen int64
}

// notListed marks an original the listing never yielded.
const notListed = -1

// checkStorage lists the store once and reports every photo whose original is
// absent or has a size other than the catalogue's: file_path, uid, the expected
// size and what was found ("absent" or the listed size).
func checkStorage(ctx context.Context, tx pgx.Tx, store storage.SizedKeyLister) ([][]string, error) {
	rows, err := tx.Query(ctx, "SELECT file_path, uid, file_size FROM photos WHERE file_path <> ''")
	if err != nil {
		return nil, fmt.Errorf("inventory: listing originals: %w", err)
	}
	want := map[string]*expectedOriginal{}
	var path, uid string
	var size int64
	if _, err := pgx.ForEachRow(rows, []any{&path, &uid, &size}, func() error {
		want[path] = &expectedOriginal{uid: uid, size: size, seen: notListed}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("inventory: listing originals: %w", err)
	}
	err = store.KeySizes(ctx, func(key string, size int64) error {
		if orig, ok := want[key]; ok {
			orig.seen = size
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("inventory: listing the store: %w", err)
	}
	return missingOriginals(want), nil
}

// missingOriginals renders the originals the listing did not confirm.
func missingOriginals(want map[string]*expectedOriginal) [][]string {
	var out [][]string
	for path, orig := range want {
		if orig.seen == orig.size {
			continue
		}
		found := "absent"
		if orig.seen != notListed {
			found = strconv.FormatInt(orig.seen, 10)
		}
		out = append(out, []string{path, orig.uid, strconv.FormatInt(orig.size, 10), found})
	}
	return out
}

// joinOrDash joins values with commas, or returns "-" for none.
func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}

// writeTSV writes rows as sorted, escaped, tab-separated lines.
func writeTSV(path string, rows [][]string) error {
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		fields := make([]string, len(row))
		for i, f := range row {
			fields[i] = escapeField(f)
		}
		lines = append(lines, strings.Join(fields, "\t"))
	}
	slices.Sort(lines)
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("inventory: writing %s: %w", path, err)
	}
	return nil
}

// fieldEscaper keeps a field on one line and free of the separator.
var fieldEscaper = strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`, "\r", `\r`)

// escapeField escapes backslashes, tabs and line breaks in a TSV field.
func escapeField(s string) string { return fieldEscaper.Replace(s) }
