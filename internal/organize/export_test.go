//go:build integration

package organize

// ListAlbumsSQL exposes the album-index statement to the package's integration
// tests, which run EXPLAIN over the real statement to assert a property of its
// query plan (that its cost stays proportional to the memberships rather than to
// the size of the library). Testing a copy of the SQL would assert nothing about
// the statement the store actually issues.
const ListAlbumsSQL = listAlbumsSQL
