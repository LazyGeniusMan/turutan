// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

// bootstrapFlag storage; fresh per command construction.
type bootstrapFlags struct {
	ref         string
	subpath     string
	answersFile string
	defaults    bool
	conflict    string
	skip        string
	force       bool
	allowHooks  bool
}

// newBootstrapCmd returns the bootstrap command: scaffold a new project
// from a template source (remote-git, local-git, filesystem, or the
// built-in "default" remote). An omitted source resolves the default.
func newBootstrapCmd() *cobra.Command {
	flags := &bootstrapFlags{}
	cmd := &cobra.Command{
		Use:   "bootstrap [source] [dir]",
		Short: "Scaffold a new project from a template source",
		Args:  cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			source, dir := "", "."
			if len(args) > 0 {
				source = args[0]
			}
			if len(args) > 1 {
				dir = args[1]
			}
			return runBootstrap(cmd, source, dir, flags)
		},
	}
	cmd.Flags().StringVar(&flags.ref, "ref", "", "override the requested template ref for this run")
	cmd.Flags().StringVar(&flags.subpath, "subpath", "", "override the URI //subpath at bootstrap")
	cmd.Flags().StringVar(&flags.answersFile, "answers-file", "", "load template answers from a YAML/JSON file")
	cmd.Flags().BoolVar(&flags.defaults, "defaults", false, "accept all template defaults non-interactively")
	cmd.Flags().StringVar(&flags.conflict, "conflict", "", "conflict record mode (inline|rej; reserved for update, validated only at bootstrap)")
	cmd.Flags().StringVar(&flags.skip, "skip", "", "comma-separated template paths kept as-is")
	cmd.Flags().BoolVar(&flags.force, "force", false, "bootstrap into a non-empty directory")
	cmd.Flags().BoolVar(&flags.allowHooks, "allow-hooks", false, "consent to template-declared hooks")
	return cmd
}

// runBootstrap maps CLI flags onto scaffold options and runs the flow.
// All human output goes through the command streams so tests can capture
// it; diagnostics go to stderr, the summary to stdout.
func runBootstrap(cmd *cobra.Command, source, dir string, flags *bootstrapFlags) error {
	var skip []string
	for entry := range strings.SplitSeq(flags.skip, ",") {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			skip = append(skip, trimmed)
		}
	}
	conflict := scaffold.ConflictMode(flags.conflict)
	if err := conflict.Validate(); err != nil {
		return err
	}
	return scaffold.Bootstrap(source, dir, scaffold.Options{
		NonInteractive: nonInteractive,
		Force:          flags.force,
		Conflict:       conflict,
		Ref:            flags.ref,
		Subpath:        flags.subpath,
		AnswersFile:    flags.answersFile,
		Defaults:       flags.defaults,
		AllowHooks:     flags.allowHooks,
		Skip:           skip,
		Engine:         version,
		Verbose:        verbose,
		Stdout:         cmd.OutOrStdout(),
		Stderr:         cmd.ErrOrStderr(),
		Stdin:          os.Stdin,
	})
}
