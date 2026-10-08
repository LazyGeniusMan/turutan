// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newBootstrapCmd returns the bootstrap stub; M1 implements fetch/render.
func newBootstrapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap <source> [dir]",
		Short: "Scaffold a new project from a template source",
		Args:  cobra.RangeArgs(0, 2),
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("bootstrap: %w", errNotImplemented)
		},
	}
}
