// Command kukatko is the entrypoint for the Kukátko photo & video library. It
// wires up the Cobra command tree (root + serve + version) and delegates all
// real work to packages under internal/. Invoked through a symlink named
// kukatkoctl it becomes the remote client instead — see cmd/kukatko/ctl.go.
package main

import (
	"fmt"
	"os"
)

// main executes the root command and maps any error to a non-zero exit code.
func main() {
	if err := newRootCmd(os.Args[0]).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
