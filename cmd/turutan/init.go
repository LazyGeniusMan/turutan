// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

type initFlags struct {
	dataFile   string
	defaults   bool
	conflict   string
	skip       string
	force      bool
	allowHooks bool
	template   string
}

func newInitCmd() *cobra.Command {
	flags := &initFlags{}
	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Scaffold a new project from a registry template",
		Long: `Scaffold a new project from a registry template.

The template is registry/template[@version] (default turutan/default at
latest). The version pins the release; omitting it follows the index
latest. The deprecated free-form source chain (--ref, --subpath,
positional sources) is gone: registry resolution replaces it. The stored
state records the registry ref, bundle digest and fetch URI for later
check/diff/update runs.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			return runInit(cmd, dir, flags)
		},
	}
	cmd.Flags().StringVar(&flags.dataFile, "data-file", "", "load template data from a YAML/JSON file (see go doc text/template for data/dot terms)")
	cmd.Flags().BoolVar(&flags.defaults, "defaults", false,
		"seed project_name from target basename; data-file overlay wins")
	cmd.Flags().StringVar(&flags.conflict, "conflict", "",
		"conflict record mode for future updates (inline|rej; stored in .config/turutan/state.yml)")
	cmd.Flags().StringVar(&flags.skip, "skip", "", "comma-separated template paths kept as-is")
	cmd.Flags().BoolVar(&flags.force, "force", false, "init into a non-empty directory")
	cmd.Flags().BoolVar(&flags.allowHooks, "allow-hooks", false, "consent to template-declared hooks")
	cmd.Flags().StringVar(&flags.template, "template", "", "registry template (registry/template[@version]; default turutan/default@latest)")
	return cmd
}

func runInit(cmd *cobra.Command, dir string, flags *initFlags) error {
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
	ref := resolveInitTemplate(cmd, flags.template)
	registries := loadInitRegistries()
	return scaffold.Init(cmd.Context(), dir, ref, registries, scaffold.Options{
		NonInteractive: appCfg.nonInteractive,
		Force:          flags.force,
		Conflict:       conflict,
		DataFile:       flags.dataFile,
		Defaults:       flags.defaults,
		AllowHooks:     flags.allowHooks,
		Skip:           skip,
		Engine:         version,
		Verbose:        appCfg.verbose,
		Stdout:         cmd.OutOrStdout(),
		Stderr:         cmd.ErrOrStderr(),
		Stdin:          os.Stdin,
	})
}

func resolveInitTemplate(cmd *cobra.Command, flag string) scaffold.RegistryRef {
	_ = cmd
	v := viper.GetViper()
	envVal, _ := os.LookupEnv(config.EnvTemplateOverride)
	return scaffold.ResolveTemplateFlag(flag, envVal, v.GetString(config.TemplateConfigKey))
}

func loadInitRegistries() map[string]string {
	path := config.ResolveConfigPath(appCfg.cfgFile)
	if path == "" {
		return map[string]string{}
	}
	return scaffold.LoadProjectRegistries(path)
}
