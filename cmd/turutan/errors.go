// SPDX-License-Identifier: Apache-2.0

package main

import "errors"

// Command exit codes (spec §5.2): 0 means success (diff: no drift,
// check-update: up-to-date); 1 means runtime error; 2 means drift found
// (diff) or update available (check-update), scriptable like diff(1).
const (
	exitOK               = 0
	exitRuntimeError     = 1
	exitDriftOrAvailable = 2
)

// exitError carries a scriptable non-error outcome: the human line is
// already on the command streams, so Execute must not re-print it and main
// exits with code instead of 1.
type exitError struct {
	code int
	msg  string
}

// Error implements error.
func (e *exitError) Error() string { return e.msg }

// exitCode maps a command error to its process exit code: scriptable
// outcomes keep their code, every other failure is a runtime error.
func exitCode(err error) int {
	if err == nil {
		return exitOK
	}
	if ee, ok := errors.AsType[*exitError](err); ok {
		return ee.code
	}
	return exitRuntimeError
}
