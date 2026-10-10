// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

type updateFlags struct {
	ref         string
	conflict    string
	force       bool
	answersFile string
	defaults    bool
	allowHooks  bool
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
	cmd.Flags().StringVar(&flags.answersFile, "answers-file", "", "overlay stored answers from a YAML/JSON file")
	cmd.Flags().BoolVar(&flags.defaults, "defaults", false, "seed project_name when stored answers lack it")
	cmd.Flags().BoolVar(&flags.allowHooks, "allow-hooks", false, "consent to template-declared migrations")
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
		AnswersFile:    flags.answersFile,
		Defaults:       flags.defaults,
		AllowHooks:     flags.allowHooks,
		NonInteractive: appCfg.nonInteractive,
		Engine:         version,
		Verbose:        appCfg.verbose,
		Stdout:         cmd.OutOrStdout(),
		Stderr:         cmd.ErrOrStderr(),
		Stdin:          os.Stdin,
	})
	return err
}
