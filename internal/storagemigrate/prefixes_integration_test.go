//go:build integration

package storagemigrate_test

import (
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/storagemigrate"
)

// TestStore_StoragePrefixes verifies the catalogue names the storage prefix of
// every project — the bootstrap project's root layout and a created project's
// own tree — which is where the migration looks for each one's genealogy export.
func TestStore_StoragePrefixes(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	created, err := project.NewStore(db.Pool()).Create(t.Context(), audit.Meta{},
		project.CreateParams{Slug: "prefixes", Name: "Prefixes"})
	if err != nil {
		t.Fatalf("creating a project: %v", err)
	}

	prefixes, err := storagemigrate.NewStore(db.Pool()).StoragePrefixes(t.Context())
	if err != nil {
		t.Fatalf("StoragePrefixes: %v", err)
	}
	if want := []string{"", created.StoragePrefix}; !slices.Equal(prefixes, want) {
		t.Errorf("StoragePrefixes() = %q, want %q", prefixes, want)
	}
}
