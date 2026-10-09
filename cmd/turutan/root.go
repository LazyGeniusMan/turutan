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

// Build metadata injected via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Global flag storage.
var (
	cfgFile        string
	nonInteractive bool
	verbose        bool
	noColor        bool
)

// errNotImplemented marks M0 stubs; M1+ replaces each command body.
var errNotImplemented = errors.New("not implemented (M1+)")

var rootCmd = &cobra.Command{
	Use:   "turutan",
	Short: "Manage project-template lifecycle",
	Long: `turutan scaffolds new projects from versioned templates and merges
template updates back into them (bootstrap, check-update, diff, update).`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		return initConfig()
	},
}

// Execute runs the root command. Scriptable outcomes (exitError) are
// already reported on the command streams and pass through untouched so
// main can exit with their code; any other error prints here and exits 1.
func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		if _, ok := errors.AsType[*exitError](err); ok {
			return err
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		return err
	}
	return nil
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path")
	rootCmd.PersistentFlags().BoolVar(&nonInteractive, "non-interactive", false, "disable interactive prompts")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "verbose diagnostics on stderr")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	rootCmd.AddCommand(
		newBootstrapCmd(),
		newCheckUpdateCmd(),
		newDiffCmd(),
		newUpdateCmd(),
		newVersionCmd(),
	)
}

// initConfig wires env layering and loads the resolved config file.
// A missing file is not fatal: flags and env remain valid.
func initConfig() error {
	viper.SetEnvPrefix(config.EnvPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	path := config.ResolveConfigPath(cfgFile)
	if verbose {
		fmt.Fprintln(os.Stderr, "turutan: using config", path)
	}
	if path == "" {
		return nil
	}
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
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
