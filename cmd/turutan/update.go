// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

// updateFlags stores per-run flag values; fresh per command construction.
type updateFlags struct {
	ref         string
	conflict    string
	force       bool
	answersFile string
	defaults    bool
}

// newUpdateCmd returns the update command: merge template changes into
// the project with the v1 overlay. Run `turutan diff` before
// `turutan update` to review pending drift. It requires a clean tree
// (lock hashes match workdir) unless --force is given.
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
	cmd.Flags().BoolVar(&flags.defaults, "defaults", false, "seed missing answers from defaults non-interactively")
	return cmd
}

// runUpdate maps CLI flags onto scaffold options and runs the flow.
// Human output goes through the command streams; diagnostics to stderr.
func runUpdate(cmd *cobra.Command, flags *updateFlags) error {
	var conflict scaffold.ConflictMode
	if strings.TrimSpace(flags.conflict) != "" {
		conflict = scaffold.ConflictMode(flags.conflict)
		if err := conflict.Validate(); err != nil {
			return err
		}
	}
	_, err := scaffold.Update(".", scaffold.UpdateOptions{
		Ref:         flags.ref,
		Conflict:    conflict,
		Force:       flags.force,
		AnswersFile: flags.answersFile,
		Defaults:    flags.defaults,
		Engine:      version,
		Stdout:      cmd.OutOrStdout(),
		Stderr:      cmd.ErrOrStderr(),
		Stdin:       os.Stdin,
	})
	return err
}
