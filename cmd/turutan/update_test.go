// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestUpdateCmd(t *testing.T) {
	t.Run("help recommends diff first", func(t *testing.T) {
		cmd := newUpdateCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--help"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("update --help error: %v", err)
		}
		if got := out.String(); !strings.Contains(got, "turutan diff") {
			t.Errorf("help = %q, want it to recommend turutan diff", got)
		}
	})
	t.Run("invalid conflict mode fails", func(t *testing.T) {
		cmd := newUpdateCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--conflict", "merge"})
		if err := cmd.Execute(); err == nil {
			t.Error("update with bad --conflict succeeded, want error")
		}
	})
	t.Run("flags register", func(t *testing.T) {
		cmd := newUpdateCmd()
		for _, name := range []string{"ref", "conflict", "force", "answers-file", "defaults", "allow-hooks"} {
			if cmd.Flags().Lookup(name) == nil {
				t.Errorf("flag --%s missing", name)
			}
		}
	})
}
