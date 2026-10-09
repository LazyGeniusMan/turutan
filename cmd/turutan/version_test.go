// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestVersionCmd pins `turutan version`: one line with the injected build
// metadata (version/commit/date via -ldflags -X) plus the §8.4 dual
// license notice (CLI Apache-2.0, default template MIT-0).
func TestVersionCmd(t *testing.T) {
	t.Run("prints metadata and license notice", func(t *testing.T) {
		cmd := newVersionCmd()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("version error: %v", err)
		}
		got := stdout.String()
		for _, want := range []string{
			"turutan " + version,
			"commit " + commit,
			"built " + date,
			"Apache-2.0",
			"MIT-0",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("version output missing %q:\n%s", want, got)
			}
		}
	})
}
