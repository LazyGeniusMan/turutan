// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newCheckUpdateCmd returns the check-update stub; M2 implements re-resolve.
func newCheckUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check-update",
		Short: "Check whether the template has a newer resolvable ref",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("check-update: %w", errNotImplemented)
		},
	}
}
