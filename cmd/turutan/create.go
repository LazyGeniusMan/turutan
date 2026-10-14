// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/spf13/cobra"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

type createFlags struct {
	force bool
}

func newCreateCmd() *cobra.Command {
	flags := &createFlags{}
	cmd := &cobra.Command{
		Use:   "create [dir]",
		Short: "Scaffold a new template-authoring project",
		Long: `Scaffold a new template-authoring project: an example .turutan.yml
covering every engine feature plus the live files it references.

Run 'turutan bootstrap --defaults <dir> <out-dir>' afterwards to see the
example template render (add --allow-hooks to run its example hooks).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			return scaffold.Create(dir, scaffold.CreateOptions{
				Force:   flags.force,
				Verbose: appCfg.verbose,
				Stdout:  cmd.OutOrStdout(),
				Stderr:  cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().BoolVar(&flags.force, "force", false, "create into a non-empty directory")
	return cmd
}
