// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

func newStateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Re-render the project from pinned state inputs",
		Long: `Re-render the project from the pinned template commit recorded in
.config/turutan/state.yml, without contacting the template upstream.

'state input' re-answers every template input (defaulting to the stored
values), writes the answers back to state.yml, then re-renders.
'state sync' re-renders hand-edited state.yml inputs as-is.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newStateInputCmd(), newStateSyncCmd())
	return cmd
}

type stateInputFlags struct {
	dataFile string
	defaults bool
	conflict string
	force    bool
	preview  bool
}

func newStateInputCmd() *cobra.Command {
	flags := &stateInputFlags{}
	cmd := &cobra.Command{
		Use:   "input",
		Short: "Re-answer stored inputs and re-render at the pinned commit",
		Long: `Re-answer every template input at the pinned commit recorded in
.config/turutan/state.yml, then re-render so the tree reflects the answers.

Each prompt defaults to the stored state.yml value (falling back to the
manifest default only when the key is absent) and follows the same
ordering, validation and 'if'-predicate logic as init. The updated answers
are written back to state.yml before the tree is merged with the same
guards as update (clean tree unless --force, conflicts inline or rej).
Never contacts the template upstream.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStateInput(cmd, flags)
		},
	}
	cmd.Flags().StringVar(&flags.dataFile, "data-file", "", "overlay stored data from a YAML/JSON file (see go doc text/template for data/dot terms)")
	cmd.Flags().BoolVar(&flags.defaults, "defaults", false, "seed project_name when stored data lack it")
	cmd.Flags().StringVar(&flags.conflict, "conflict", "", "conflict record mode (inline|rej)")
	cmd.Flags().BoolVar(&flags.force, "force", false, "re-render even when the tree differs from the lock")

	cmd.Flags().BoolVar(&flags.preview, "preview", false, "print the per-path file plan without writing")
	return cmd
}

func runStateInput(cmd *cobra.Command, flags *stateInputFlags) error {
	var conflict scaffold.ConflictMode
	if strings.TrimSpace(flags.conflict) != "" {
		conflict = scaffold.ConflictMode(flags.conflict)
		if err := conflict.Validate(); err != nil {
			return err
		}
	}
	_, err := scaffold.StateInput(cmd.Context(), ".", scaffold.InputOptions{
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

type stateSyncFlags struct {
	conflict string
	force    bool
	preview  bool
}

func newStateSyncCmd() *cobra.Command {
	flags := &stateSyncFlags{}
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Re-render hand-edited state inputs at the pinned commit",
		Long: `Re-render the project from the current .config/turutan/state.yml inputs
at the exact pinned commit, without contacting the template upstream.

The template is fetched at state ResolvedCommit only (remote-git reuses the
default cache when present, else clones and checks out the pinned SHA;
local-git checks out the stored SHA with no HEAD re-resolution; filesystem
opens the stored path directly). The stored ResolvedCommit and RequestedRef
are kept unchanged, no new-input warnings are printed, and the merge uses
the same guards as update (clean tree unless --force, conflicts inline or
rej).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStateSync(cmd, flags)
		},
	}
	cmd.Flags().StringVar(&flags.conflict, "conflict", "", "conflict record mode (inline|rej)")
	cmd.Flags().BoolVar(&flags.force, "force", false, "re-render even when the tree differs from the lock")

	cmd.Flags().BoolVar(&flags.preview, "preview", false, "print the per-path file plan without writing")
	return cmd
}

func runStateSync(cmd *cobra.Command, flags *stateSyncFlags) error {
	var conflict scaffold.ConflictMode
	if strings.TrimSpace(flags.conflict) != "" {
		conflict = scaffold.ConflictMode(flags.conflict)
		if err := conflict.Validate(); err != nil {
			return err
		}
	}
	_, err := scaffold.StateSync(cmd.Context(), ".", scaffold.SyncOptions{
		Conflict:       conflict,
		Force:          flags.force,
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
