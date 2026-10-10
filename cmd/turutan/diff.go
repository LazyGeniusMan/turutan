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

func runDiff(cmd *cobra.Command, ref string, noPager bool) error {
	stdout := cmd.OutOrStdout()
	diffs, err := scaffold.ComputeDiff(cmd.Context(), ".", scaffold.DiffOptions{
		Ref:     ref,
		Engine:  version,
		NoColor: appCfg.noColor,
		Verbose: appCfg.verbose,
		Stderr:  cmd.ErrOrStderr(),
	})
	if err != nil {
		return err
	}
	if len(diffs) == 0 {
		fmt.Fprintln(stdout, "turutan: no drift")
		return nil
	}
	if !noPager && !appCfg.nonInteractive && isTerminal(stdout) {
		if err := tui.RunDiff(
			toTUIDiffs(diffs), tui.Options{NoColor: appCfg.noColor}); err != nil {
			return err
		}
		return &exitError{code: exitDriftOrAvailable, msg: "drift found"}
	}
	if err := scaffold.WriteDiff(
		stdout, diffs, scaffold.WriteOptions{NoColor: appCfg.noColor}); err != nil {
		return err
	}
	return &exitError{code: exitDriftOrAvailable, msg: "drift found"}
}

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
