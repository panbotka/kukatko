package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/panbotka/kukatko/internal/version"
)

// ApplicationNamePrefix starts the application_name of every connection a
// Kukátko binary opens (from the release after 0.31.0 on). OtherClients matches
// it to recognise another Kukátko process on the same database even when that
// process connects as a different database user.
const ApplicationNamePrefix = "kukatko/"

// maxApplicationName is PostgreSQL's limit (NAMEDATALEN - 1). The server
// truncates a longer name silently, after which it would no longer equal the
// one this process compares against, so the name is kept within it.
const maxApplicationName = 63

// processInstance is a random token minted once per process. It is what keeps
// two processes running the same version apart in pg_stat_activity.
var processInstance = sync.OnceValue(func() string {
	var buf [4]byte
	// crypto/rand.Read never returns an error (it panics on an unusable source),
	// see its documentation.
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
})

// ApplicationName returns the application_name this process gives every
// database connection: "kukatko/<version> <instance>", e.g.
// "kukatko/0.32.0 1a2b3c4d". It is the same for every pool the process opens,
// so a subcommand that opens two pools never mistakes itself for a second
// instance.
func ApplicationName() string {
	return applicationName(version.Version, processInstance())
}

// applicationName assembles the application_name from a version and an
// instance token. Only ASCII letters, digits and ".+-_" of the version survive
// (the server replaces anything non-ASCII with '?', breaking the equality
// OtherClients relies on), and the version is shortened so the whole name fits
// maxApplicationName.
func applicationName(ver, instance string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune(".+-_", r):
			return r
		default:
			return '-'
		}
	}, ver)
	if clean == "" {
		clean = "unknown"
	}
	room := maxApplicationName - len(ApplicationNamePrefix) - len(" ") - len(instance)
	if len(clean) > room {
		clean = clean[:max(room, 0)]
	}
	return ApplicationNamePrefix + clean + " " + instance
}

// ApplicationName returns the application_name this DB's connections announce.
func (db *DB) ApplicationName() string {
	return db.appName
}

// Client is one session on the database that is not this process's.
type Client struct {
	// PID is the server process id of the session (pg_stat_activity.pid).
	PID int32
	// ApplicationName is what the client announced; empty when it set none,
	// as Kukátko 0.31.0 and older do.
	ApplicationName string
	// User is the database user the session runs as.
	User string
	// Address is the client's IP address; empty for a Unix socket, or when
	// the session belongs to another user this role may not inspect.
	Address string
	// Since is when the session connected; zero when it is not visible.
	Since time.Time
}

// Kukatko reports whether the client announced itself as a Kukátko process.
func (c Client) Kukatko() bool {
	return strings.HasPrefix(c.ApplicationName, ApplicationNamePrefix)
}

// String describes the client for a log line or an operator's terminal, e.g.
// `pid 4242 "kukatko/0.32.0 1a2b3c4d" user kukatko from 172.18.0.5 since
// 2026-10-14T09:00:00Z`. An unnamed client says so, because that is what an old
// Kukátko release looks like.
func (c Client) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "pid %d", c.PID)
	if c.ApplicationName == "" {
		b.WriteString(" (no application_name — Kukátko 0.31.0 or older, or another tool)")
	} else {
		fmt.Fprintf(&b, " %q", c.ApplicationName)
	}
	if c.User != "" {
		fmt.Fprintf(&b, " user %s", c.User)
	}
	if c.Address != "" {
		fmt.Fprintf(&b, " from %s", c.Address)
	}
	if !c.Since.IsZero() {
		fmt.Fprintf(&b, " since %s", c.Since.UTC().Format(time.RFC3339))
	}
	return b.String()
}

// otherClientsSQL lists the client sessions on the current database that are
// not this process's: every session announcing a Kukátko application_name other
// than ours (whatever user it runs as), and every other session of our own
// database user — which is how a release that sets no application_name (0.31.0
// and older) shows up. Sessions of other users that do not announce Kukátko
// (a monitoring exporter, an admin's psql) are none of a migration's business.
const otherClientsSQL = `SELECT pid, coalesce(application_name, ''), coalesce(usename, ''),
	coalesce(host(client_addr), ''), backend_start
FROM pg_stat_activity
WHERE datname = current_database()
	AND backend_type = 'client backend'
	AND pid <> pg_backend_pid()
	AND application_name IS DISTINCT FROM $1
	AND (application_name LIKE 'kukatko/%' OR usename = current_user)
ORDER BY backend_start NULLS LAST, pid`

// OtherClients returns the sessions on this database that belong to someone
// else than this process (see otherClientsSQL for exactly who counts), oldest
// first. It is the check a migrator runs before changing the schema under a
// server that may still be serving it: the advisory lock only serialises two
// migrators, it does not notice an old server that is merely connected. It is a
// snapshot read of pg_stat_activity and returns a wrapped error if that read
// fails.
func (db *DB) OtherClients(ctx context.Context) ([]Client, error) {
	rows, err := db.pool.Query(ctx, otherClientsSQL, db.appName)
	if err != nil {
		return nil, fmt.Errorf("listing other database clients: %w", err)
	}
	defer rows.Close()

	var clients []Client
	for rows.Next() {
		var (
			c     Client
			since *time.Time
		)
		if err := rows.Scan(&c.PID, &c.ApplicationName, &c.User, &c.Address, &since); err != nil {
			return nil, fmt.Errorf("scanning a database client: %w", err)
		}
		if since != nil {
			c.Since = *since
		}
		clients = append(clients, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating database clients: %w", err)
	}
	return clients, nil
}
