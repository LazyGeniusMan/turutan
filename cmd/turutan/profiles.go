// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

func newProfilesCmd() *cobra.Command {
	var ref string
	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "List template profiles and enabled state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProfiles(cmd, ref)
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "override the stored requestedRef for this run")
	return cmd
}

func runProfiles(cmd *cobra.Command, ref string) error {
	info, err := scaffold.ListProfiles(cmd.Context(), ".", scaffold.ProfileOptions{
		Ref:     ref,
		Engine:  version,
		Verbose: appCfg.verbose,
		Stderr:  cmd.ErrOrStderr(),
	})
	if err != nil {
		return err
	}
	stdout := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(stdout,
		"turutan: offered profiles: %s\n",
		strings.Join(info.Offered, ", ")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout,
		"turutan: enabled profiles: %s\n",
		strings.Join(info.Enabled, ", ")); err != nil {
		return err
	}
	return nil
}
