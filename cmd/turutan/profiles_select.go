// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

func splitProfilesList(ids []string) []string {
	var out []string
	for _, id := range ids {
		for _, part := range strings.Split(id, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			out = append(out, trimmed)
		}
	}
	return out
}

func flagProfiles(cmd *cobra.Command) ([]string, bool) {
	if cmd == nil {
		return nil, false
	}
	lookup := func(name string) (string, bool) {
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
			if val, err := cmd.Flags().GetStringSlice(name); err == nil {
				return strings.Join(val, ","), true
			}
			return "", true
		}
		if f := cmd.InheritedFlags().Lookup(name); f != nil && f.Changed {
			if val, err := cmd.InheritedFlags().GetStringSlice(name); err == nil {
				return strings.Join(val, ","), true
			}
			return "", true
		}
		if root := cmd.Root(); root != nil {
			if f := root.PersistentFlags().Lookup(name); f != nil && f.Changed {
				if val, err := root.PersistentFlags().GetStringSlice(name); err == nil {
					return strings.Join(val, ","), true
				}
				return "", true
			}
		}
		return "", false
	}
	if raw, ok := lookup("profiles"); ok {
		return splitProfilesList([]string{raw}), true
	}
	return nil, false
}

func resolveProfiles(cmd *cobra.Command, v *viper.Viper, projectDir string) []string {
	return resolveProfilesWithViper(v, cmd, projectDir)
}

func resolveProfilesWithViper(v *viper.Viper, cmd *cobra.Command, projectDir string) []string {
	if cmd != nil {
		if val, changed := flagProfiles(cmd); changed {
			return splitProfilesList(val)
		}
	}
	if envVal, ok := os.LookupEnv(config.EnvProfilesOverride); ok && strings.TrimSpace(envVal) != "" {
		return splitProfilesList([]string{envVal})
	}
	if projectDir != "" {
		if state, err := config.LoadState(os.DirFS(projectDir)); err == nil && len(state.Profiles) > 0 {
			return splitProfilesList(append([]string{}, state.Profiles...))
		}
	}
	if v != nil && v.IsSet(config.ProfilesConfigKey) {
		if val := v.GetStringSlice(config.ProfilesConfigKey); len(splitProfilesList(val)) > 0 {
			return splitProfilesList(val)
		}
		if raw := strings.TrimSpace(v.GetString(config.ProfilesConfigKey)); raw != "" {
			return splitProfilesList([]string{raw})
		}
	}
	return append([]string{}, config.DefaultProfiles...)
}

func resolveProfilesListPrecedence(flagProfiles []string, envVal string, projectProfiles, userConfig []string) []string {
	if len(splitProfilesList(flagProfiles)) > 0 {
		return splitProfilesList(flagProfiles)
	}
	if strings.TrimSpace(envVal) != "" {
		return splitProfilesList([]string{envVal})
	}
	if len(splitProfilesList(projectProfiles)) > 0 {
		return splitProfilesList(projectProfiles)
	}
	if len(splitProfilesList(userConfig)) > 0 {
		return splitProfilesList(userConfig)
	}
	return append([]string{}, config.DefaultProfiles...)
}
