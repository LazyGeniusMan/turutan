// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newVersionCmd returns the version command printing injected build metadata.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "turutan %s (commit %s, built %s)\n", version, commit, date)
			fmt.Fprintln(cmd.OutOrStdout(), "CLI: Apache-2.0; default template: MIT-0")
			return nil
		},
	}
}
