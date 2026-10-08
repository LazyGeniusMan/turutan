// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newDiffCmd returns the diff stub; M2 implements drift display.
func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "Show pending template-to-project drift",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("diff: %w", errNotImplemented)
		},
	}
}
