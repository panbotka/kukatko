package imgconvert

import (
	"fmt"
	"os"
	"sync"
)

// createTempJPEG creates a new empty temporary file matching pattern under
// os.TempDir() and immediately closes it so an external process can open and
// write to it. It returns the absolute path plus a once-only cleanup function;
// if creation succeeds but Close fails the partial file is removed first.
func createTempJPEG(pattern string) (string, func(), error) {
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, fmt.Errorf("imgconvert: create temp jpeg: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := onceRemove(tmpPath)
	if closeErr := tmp.Close(); closeErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("imgconvert: close temp jpeg: %w", closeErr)
	}
	return tmpPath, cleanup, nil
}

// onceRemove returns a cleanup function that os.Removes path on its first
// invocation and is a no-op on every subsequent call, satisfying the
// "safe to call multiple times" cleanup contract on EnsureDecodable.
func onceRemove(path string) func() {
	var once sync.Once
	return func() {
		once.Do(func() { _ = os.Remove(path) })
	}
}
