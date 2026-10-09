// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/spf13/cobra"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

// newCheckUpdateCmd returns the check-update command: re-resolve the stored
// template ref and report whether the template moved. Exit 0 means
// up-to-date, 2 means update available, 1 means runtime error.
func newCheckUpdateCmd() *cobra.Command {
	var ref string
	cmd := &cobra.Command{
		Use:   "check-update",
		Short: "Check whether the template has a newer resolvable ref",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := scaffold.CheckUpdate(".", scaffold.CheckUpdateOptions{
				Ref:     ref,
				Verbose: verbose,
				Stdout:  cmd.OutOrStdout(),
				Stderr:  cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			if result.Available {
				return &exitError{code: exitDriftOrAvailable, msg: "update available"}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "override the stored requestedRef for this run")
	return cmd
}
