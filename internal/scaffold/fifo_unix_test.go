// SPDX-License-Identifier: Apache-2.0

//go:build unix

package scaffold

import (
	"syscall"
	"testing"
)

func makeTestFifo(t *testing.T, path string) bool {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Logf("fifo unavailable: %v", err)
		return false
	}
	return true
}
