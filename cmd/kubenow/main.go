// Package main provides the kubenow CLI entrypoint.
package main

import (
	"fmt"
	"os"

	"github.com/ppiankov/kubenow/internal/cli"
	"github.com/ppiankov/kubenow/internal/util"
)

// Set via ldflags at build time.
var (
	Version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cli.SetBuildInfo(Version, commit, date)
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		util.Exit(util.ExitRuntimeError)
	}
}
