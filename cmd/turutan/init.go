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
	ref        string
	subpath    string
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
		Use:   "init [source] [dir]",
		Short: "Scaffold a new project from a template source",
		Args:  cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			source, dir := "", "."
			if len(args) == 1 && flags.template != "" {
				dir = args[0]
			} else {
				if len(args) > 0 {
					source = args[0]
				}
				if len(args) > 1 {
					dir = args[1]
				}
			}
			return runInit(cmd, source, dir, flags)
		},
	}
	cmd.Flags().StringVar(&flags.ref, "ref", "", "override the requested template ref for this run")
	cmd.Flags().StringVar(&flags.subpath, "subpath", "", "override the URI //subpath at init")
	cmd.Flags().StringVar(&flags.dataFile, "data-file", "", "load template data from a YAML/JSON file (see go doc text/template for data/dot terms)")
	cmd.Flags().BoolVar(&flags.defaults, "defaults", false,
		"seed project_name from target basename; data-file overlay wins")
	cmd.Flags().StringVar(&flags.conflict, "conflict", "",
		"conflict record mode for future updates (inline|rej; stored in .config/turutan/state.yml)")
	cmd.Flags().StringVar(&flags.skip, "skip", "", "comma-separated template paths kept as-is")
	cmd.Flags().BoolVar(&flags.force, "force", false, "init into a non-empty directory")
	cmd.Flags().BoolVar(&flags.allowHooks, "allow-hooks", false, "consent to template-declared hooks")
	cmd.Flags().StringVar(&flags.template, "template", "", "default template source (used when no source arg is given)")
	return cmd
}

func runInit(cmd *cobra.Command, source, dir string, flags *initFlags) error {
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
	source = resolveInitSource(source, flags.template, dir)
	return scaffold.Init(cmd.Context(), source, dir, scaffold.Options{
		NonInteractive: appCfg.nonInteractive,
		Force:          flags.force,
		Conflict:       conflict,
		Ref:            flags.ref,
		Subpath:        flags.subpath,
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

func resolveInitSource(arg, templateFlag, targetDir string) string {
	return resolveInitSourceWithViper(viper.GetViper(), arg, templateFlag, targetDir)
}

func resolveInitSourceWithViper(v *viper.Viper, arg, templateFlag, targetDir string) string {
	envVal, _ := os.LookupEnv(config.EnvTemplateOverride)
	var projectTemplate string
	if state, err := config.LoadState(os.DirFS(targetDir)); err == nil {
		projectTemplate = state.Template
	}
	userTemplate := v.GetString(config.TemplateConfigKey)
	return resolveSourcePrecedence(arg, templateFlag, envVal, projectTemplate, userTemplate)
}

func resolveSourcePrecedence(arg, templateFlag, envVal, projectTemplate, userConfigTemplate string) string {
	if arg != "" {
		return arg
	}
	if strings.TrimSpace(templateFlag) != "" {
		return strings.TrimSpace(templateFlag)
	}
	if strings.TrimSpace(envVal) != "" {
		return strings.TrimSpace(envVal)
	}
	if strings.TrimSpace(projectTemplate) != "" {
		return strings.TrimSpace(projectTemplate)
	}
	if strings.TrimSpace(userConfigTemplate) != "" {
		return strings.TrimSpace(userConfigTemplate)
	}
	return ""
}
