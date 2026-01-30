package util

import (
	"fmt"
	"os"
)

// Standard exit codes aligned with spectre tools family
const (
	// ExitOK indicates successful execution
	ExitOK = 0

	// ExitPolicyFail indicates policy violations or threshold breaches
	// (reserved for future use)
	ExitPolicyFail = 1

	// ExitInvalidInput indicates validation errors or invalid parameters
	ExitInvalidInput = 2

	// ExitRuntimeError indicates I/O errors, API failures, or runtime issues
	ExitRuntimeError = 3
)

// Exit terminates the program with the given exit code
func Exit(code int) {
	os.Exit(code)
}

// ExitWithError prints an error message to stderr and exits with the given code
func ExitWithError(code int, format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	Exit(code)
}
