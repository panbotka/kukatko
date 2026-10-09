package photoapi

import (
	"image"
	"testing"
)

// TestEditedRenderCost verifies the edited download charges three full-size
// RGBA copies beyond the decoded original: the oriented copy and two from the
// edit.
func TestEditedRenderCost(t *testing.T) {
	t.Parallel()
	if got, want := editedRenderCost(image.Config{Width: 100, Height: 50}), int64(3*100*50*4); got != want {
		t.Errorf("editedRenderCost(100x50) = %d, want %d", got, want)
	}
}
