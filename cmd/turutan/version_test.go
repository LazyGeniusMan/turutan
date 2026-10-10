// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCmd(t *testing.T) {
	t.Parallel()
	t.Run("prints metadata and license notice", func(t *testing.T) {
		t.Parallel()
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
