package main

import (
	"fmt"
	"os"
)

// Automated collection and distributed scheduling are deferred in this phase.
func main() {
	fmt.Fprintln(os.Stderr, "worker is deferred; use cmd/import for explicit historical CSV imports")
	os.Exit(1)
}
