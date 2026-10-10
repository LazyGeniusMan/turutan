// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/spf13/cobra"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

func newCheckUpdateCmd() *cobra.Command {
	var ref string
	cmd := &cobra.Command{
		Use:   "check-update",
		Short: "Check whether the template has a newer resolvable ref",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := scaffold.CheckUpdate(cmd.Context(), ".", scaffold.CheckUpdateOptions{
				Ref:     ref,
				Engine:  version,
				Verbose: appCfg.verbose,
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
