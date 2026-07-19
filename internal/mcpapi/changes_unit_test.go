package mcpapi

import (
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
)

// TestMetadataChanges_recordsEditedText verifies the MCP metadata diff records
// old→new for the text field it changed and omits the ones it left alone.
func TestMetadataChanges_recordsEditedText(t *testing.T) {
	t.Parallel()

	before := photos.Photo{Title: "old title", Description: "kept", Notes: "kept notes"}
	after := photos.MetadataUpdate{Title: "new title", Description: "kept", Notes: "kept notes"}

	diff := metadataChanges(before, after).Map()
	if len(diff) != 1 {
		t.Fatalf("diff has %d entries, want 1 (title): %v", len(diff), diff)
	}
	change, ok := diff["title"].(audit.Change)
	if !ok {
		t.Fatalf("title change type = %T, want audit.Change", diff["title"])
	}
	if change.Old != "old title" || change.New != "new title" {
		t.Errorf("title change = %+v, want old→new", change)
	}
}

// TestMetadataChanges_noopStampsNothing verifies an MCP edit that changes nothing
// leaves the details map without a "changes" key.
func TestMetadataChanges_noopStampsNothing(t *testing.T) {
	t.Parallel()

	before := photos.Photo{Title: "same", Description: "same", Notes: "same"}
	after := photos.MetadataUpdate{Title: "same", Description: "same", Notes: "same"}

	details := map[string]any{"via": viaMCP}
	metadataChanges(before, after).StampInto(details)
	if _, ok := details[audit.ChangesKey]; ok {
		t.Errorf("no-op edit stamped a changes key: %v", details)
	}
}
