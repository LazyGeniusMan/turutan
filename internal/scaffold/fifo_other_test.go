// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package scaffold

import "testing"

func makeTestFifo(t *testing.T, _ string) bool {
	t.Helper()
	return false
}
