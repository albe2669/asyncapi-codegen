package generatorv3

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// BufNotInstalledError is returned when a specification contains
// application/cloudevents+protobuf messages but the buf binary is not
// available in PATH.
type BufNotInstalledError struct {
	cause error
}

// Error implements the error interface.
func (e *BufNotInstalledError) Error() string {
	return "buf is required for application/cloudevents+protobuf messages but was not found in PATH " +
		"(install: https://buf.build/docs/installation)"
}

// Unwrap returns the underlying lookup error.
func (e *BufNotInstalledError) Unwrap() error {
	return e.cause
}

// runBuf executes the buf CLI with the given arguments inside dir, streaming
// stdout and stderr to this process, mirroring buf's excellent error output.
//
// ponytail: buf is a CLI, not a stable Go library; if an in-process runner is
// ever needed, only this function changes.
func runBuf(dir string, args ...string) error {
	bufPath, err := exec.LookPath("buf")
	if err != nil {
		return &BufNotInstalledError{cause: err}
	}

	ctx := context.Background()
	cmd := exec.CommandContext(ctx, bufPath, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		// The buf output is already on stderr; wrap only with the command
		// context so callers can react to the failure.
		return fmt.Errorf("buf %s: %w", strings.Join(args, " "), err)
	}

	return nil
}
