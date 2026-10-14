// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/spf13/cobra"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

func newCheckCmd() *cobra.Command {
	var ref string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check whether the template has a newer resolvable ref",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := scaffold.Check(cmd.Context(), ".", scaffold.CheckOptions{
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
