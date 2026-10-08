// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newUpdateCmd returns the update stub; M3 implements the merge flow.
func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Merge template changes into the project",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("update: %w", errNotImplemented)
		},
	}
}
