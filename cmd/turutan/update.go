// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

type updateFlags struct {
	ref      string
	conflict string
	force    bool
	dataFile string
	defaults bool
	preview  bool
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
	cmd.Flags().StringVar(&flags.ref, "ref", "", "update to a pinned template version instead of the index latest")
	cmd.Flags().StringVar(&flags.conflict, "conflict", "", "conflict record mode (inline|rej)")
	cmd.Flags().BoolVar(&flags.force, "force", false, "update even when the tree differs from the lock")
	cmd.Flags().StringVar(&flags.dataFile, "data-file", "", "overlay stored data from a YAML/JSON file (see go doc text/template for data/dot terms)")
	cmd.Flags().BoolVar(&flags.defaults, "defaults", false, "seed project_name when stored data lack it")
	cmd.Flags().BoolVar(&flags.preview, "preview", false, "print the per-path file plan without writing")
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
	_, err := scaffold.Update(cmd.Context(), ".", scaffold.UpdateOptions{
		Ref:            flags.ref,
		Conflict:       conflict,
		Force:          flags.force,
		DataFile:       flags.dataFile,
		Defaults:       flags.defaults,
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
