// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "turutan %s (commit %s, built %s)\n", version, commit, date)
	fmt.Fprintln(w, "CLI: Apache-2.0; default template: MIT-0")
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			printVersion(cmd.OutOrStdout())
			return nil
		},
	}
}
