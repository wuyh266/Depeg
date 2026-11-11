// Read-only acceptance command for the DG Python handoff.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"depeg.local/platform/internal/adapter/pipeline/python"
)

func main() {
	path := flag.String("input", "", "Python prediction JSON file (required)")
	flag.Parse()
	if *path == "" {
		fmt.Fprintln(os.Stderr, "-input is required")
		os.Exit(2)
	}
	stream, err := os.Open(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer stream.Close()
	predictions, err := python.DecodeResult(stream)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := json.NewEncoder(os.Stdout).Encode(predictions); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
