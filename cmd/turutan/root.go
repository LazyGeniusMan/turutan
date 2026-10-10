// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type appConfig struct {
	cfgFile        string
	nonInteractive bool
	verbose        bool
	noColor        bool
	showVersion    bool
}

var appCfg appConfig

func Execute() error {
	rootCmd := newRootCmd(viper.GetViper())
	if err := rootCmd.Execute(); err != nil {
		if _, ok := errors.AsType[*exitError](err); ok {
			return err
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		return err
	}
	return nil
}

func newRootCmd(v *viper.Viper) *cobra.Command {
	cmd := newRootCommand(v)
	addRootFlags(cmd)
	cmd.AddCommand(
		newBootstrapCmd(),
		newCheckUpdateCmd(),
		newDiffCmd(),
		newUpdateCmd(),
		newVersionCmd(),
	)
	bindViperFlags(cmd, v)
	return cmd
}

func newRootCommand(v *viper.Viper) *cobra.Command {
	return &cobra.Command{
		Use:   "turutan",
		Short: "Manage project-template lifecycle",
		Long: `turutan scaffolds new projects from versioned templates and merges
template updates back into them (bootstrap, check-update, diff, update).`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			_ = v.BindPFlags(cmd.Flags())
			_ = v.BindPFlags(cmd.PersistentFlags())
			_ = v.BindPFlags(cmd.InheritedFlags())
			return initConfigWithViper(v, appCfg.cfgFile)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if appCfg.showVersion {
				printVersion(cmd.OutOrStdout())
				return nil
			}
			return cmd.Help()
		},
	}
}

func addRootFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(
		&appCfg.cfgFile, "config", "", "config file path")
	cmd.PersistentFlags().BoolVar(
		&appCfg.nonInteractive, "non-interactive", false,
		"disable interactive prompts")
	cmd.PersistentFlags().BoolVar(
		&appCfg.verbose, "verbose", false,
		"verbose diagnostics on stderr")
	cmd.PersistentFlags().BoolVar(
		&appCfg.noColor, "no-color", false, "disable colored output")
	cmd.PersistentFlags().BoolVar(
		&appCfg.showVersion, "version", false,
		"print version information")
}

func bindViperFlags(cmd *cobra.Command, v *viper.Viper) {
	_ = v.BindPFlags(cmd.PersistentFlags())
	for _, sub := range cmd.Commands() {
		_ = v.BindPFlags(sub.Flags())
		_ = v.BindPFlags(sub.PersistentFlags())
	}
}

func newViper() *viper.Viper {
	v := viper.New()
	v.SetEnvPrefix(config.EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	return v
}

func initConfigWithViper(v *viper.Viper, cfgPath string) error {
	v.SetEnvPrefix(config.EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	path := config.ResolveConfigPath(cfgPath)
	if appCfg.verbose {
		fmt.Fprintln(os.Stderr, "turutan: using config", path)
	}
	if path == "" {
		return nil
	}
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); ok {
			return nil
		}
		if _, ok := errors.AsType[*viper.ConfigFileNotFoundError](err); ok {
			return nil
		}
		return fmt.Errorf("reading config %q: %w", path, err)
	}
	return nil
}

func initConfig() error {
	return initConfigWithViper(viper.GetViper(), appCfg.cfgFile)
}
