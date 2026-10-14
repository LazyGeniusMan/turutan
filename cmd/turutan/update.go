// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

type updateFlags struct {
	ref        string
	conflict   string
	force      bool
	dataFile   string
	defaults   bool
	allowHooks bool
	enable     []string
	disable    []string
	preview    bool
}

func newUpdateCmd() *cobra.Command {
	flags := &updateFlags{}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Merge template changes into the project",
		Long: `Merge template changes into the project (v1 overlay).

Run 'turutan diff' before 'turutan update' to review pending drift.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdate(cmd, flags)
		},
	}
	cmd.Flags().StringVar(&flags.ref, "ref", "", "override the stored requestedRef for this run")
	cmd.Flags().StringVar(&flags.conflict, "conflict", "", "conflict record mode (inline|rej)")
	cmd.Flags().BoolVar(&flags.force, "force", false, "update even when the tree differs from the lock")
	cmd.Flags().StringVar(&flags.dataFile, "data-file", "", "overlay stored data from a YAML/JSON file (see go doc text/template for data/dot terms)")
	cmd.Flags().BoolVar(&flags.defaults, "defaults", false, "seed project_name when stored data lack it")
	cmd.Flags().BoolVar(&flags.allowHooks, "allow-hooks", false, "consent to template-declared migrations")
	cmd.Flags().StringSliceVar(&flags.enable, "enable", nil, "enable template profiles (repeatable or comma-separated)")
	cmd.Flags().StringSliceVar(&flags.disable, "disable", nil, "disable template profiles (repeatable or comma-separated)")
	cmd.Flags().BoolVar(&flags.preview, "preview", false, "print the per-profile file plan without writing")
	return cmd
}

func runUpdate(cmd *cobra.Command, flags *updateFlags) error {
	var conflict scaffold.ConflictMode
	if strings.TrimSpace(flags.conflict) != "" {
		conflict = scaffold.ConflictMode(flags.conflict)
		if err := conflict.Validate(); err != nil {
			return err
		}
	}
	profiles := resolveProfiles(cmd, viper.GetViper(), ".")
	_, err := scaffold.Update(cmd.Context(), ".", scaffold.UpdateOptions{
		Ref:            flags.ref,
		Conflict:       conflict,
		Force:          flags.force,
		DataFile:       flags.dataFile,
		Profiles:       profiles,
		Defaults:       flags.defaults,
		AllowHooks:     flags.allowHooks,
		Enable:         flags.enable,
		Disable:        flags.disable,
		Preview:        flags.preview,
		NonInteractive: appCfg.nonInteractive,
		Engine:         version,
		Verbose:        appCfg.verbose,
		Stdout:         cmd.OutOrStdout(),
		Stderr:         cmd.ErrOrStderr(),
		Stdin:          os.Stdin,
	})
	return err
}
