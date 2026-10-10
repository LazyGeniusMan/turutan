// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil is success", err: nil, want: exitOK},
		{name: "drift is scriptable", err: &exitError{code: exitDriftOrAvailable, msg: "drift"}, want: 2},
		{name: "wrapped drift stays scriptable", err: fmt.Errorf("diff: %w", &exitError{code: 2, msg: "drift"}), want: 2},
		{name: "runtime error is 1", err: errors.New("boom"), want: exitRuntimeError},
		{name: "custom code passes through", err: &exitError{code: 3, msg: "custom"}, want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := exitCode(tt.err); got != tt.want {
				t.Errorf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
