// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
	"github.com/LazyGeniusMan/turutan/internal/tui"
)

// newDiffCmd returns the diff command: show pending template-to-project
// drift as Context-3 unified diffs. A TTY shows the Bubbletea file-list +
// hunk review UI; piped output, --non-interactive and --no-pager print the
// plain byte-stable diff to stdout. Exit 0 means no drift, 2 means drift,
// 1 means runtime error. Diff never mutates project state.
func newDiffCmd() *cobra.Command {
	var ref string
	var noPager bool
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show pending template-to-project drift",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDiff(cmd, ref, noPager)
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "override the stored requestedRef for this run")
	cmd.Flags().BoolVar(&noPager, "no-pager", false, "print the plain diff even on a TTY")
	return cmd
}

// runDiff computes the drift and presents it: interactive UI on a TTY,
// plain stdout otherwise. Logs go to stderr; the diff itself to stdout.
// The global --no-color flag threads into both presentations (the plain
// rendering is byte-stable text; the review UI stays unstyled).
func runDiff(cmd *cobra.Command, ref string, noPager bool) error {
	stdout := cmd.OutOrStdout()
	diffs, err := scaffold.ComputeDiff(".", scaffold.DiffOptions{
		Ref:     ref,
		NoColor: noColor,
		Verbose: verbose,
		Stderr:  cmd.ErrOrStderr(),
	})
	if err != nil {
		return err
	}
	if len(diffs) == 0 {
		fmt.Fprintln(stdout, "turutan: no drift")
		return nil
	}
	if !noPager && !nonInteractive && isTerminal(stdout) {
		if err := tui.RunDiff(toTUIDiffs(diffs), tui.Options{NoColor: noColor}); err != nil {
			return err
		}
		return &exitError{code: exitDriftOrAvailable, msg: "drift found"}
	}
	if err := scaffold.WriteDiff(stdout, diffs, scaffold.WriteOptions{NoColor: noColor}); err != nil {
		return err
	}
	return &exitError{code: exitDriftOrAvailable, msg: "drift found"}
}

// toTUIDiffs maps scaffold drift onto the interactive display model.
func toTUIDiffs(diffs []scaffold.FileDiff) []tui.FileDiff {
	out := make([]tui.FileDiff, 0, len(diffs))
	for _, diff := range diffs {
		out = append(out, tui.FileDiff{
			Path:      diff.Path,
			Unified:   diff.Unified,
			Added:     diff.Added,
			Removed:   diff.Removed,
			IsNew:     diff.IsNew,
			IsDeleted: diff.IsDeleted,
		})
	}
	return out
}

// isTerminal reports whether w is a character device (a TTY), so piped or
// captured output takes the plain path. Anything non-file is not a TTY.
func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
