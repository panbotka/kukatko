// Command kukatko is the entrypoint for the Kukátko photo & video library. It
// wires up the Cobra command tree (root + serve + version) and delegates all
// real work to packages under internal/.
package main

import (
	"fmt"
	"os"
)

// main executes the root command and maps any error to a non-zero exit code.
func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
