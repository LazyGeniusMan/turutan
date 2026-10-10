// SPDX-License-Identifier: Apache-2.0

package main

import "errors"

const (
	exitOK               = 0
	exitRuntimeError     = 1
	exitDriftOrAvailable = 2
)

type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func exitCode(err error) int {
	if err == nil {
		return exitOK
	}
	if ee, ok := errors.AsType[*exitError](err); ok {
		return ee.code
	}
	return exitRuntimeError
}
