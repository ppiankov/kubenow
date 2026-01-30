package main

import (
	"fmt"
	"os"

	"github.com/ppiankov/kubenow/internal/cli"
	"github.com/ppiankov/kubenow/internal/util"
)

// Version is set at build time via -ldflags
var Version = "0.1.1"

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		util.Exit(util.ExitRuntimeError)
	}
}
