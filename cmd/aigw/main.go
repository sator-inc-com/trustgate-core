package main

import (
	"fmt"
	"os"

	"github.com/trustgate/trustgate/internal/cli"
)

// Set by ldflags at build time
var version = "dev"

func main() {
	if err := cli.Execute(version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
