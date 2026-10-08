// SPDX-License-Identifier: Apache-2.0

// Package config resolves turutan configuration file locations and
// provides minimal settings types. Full load/save/validate arrives in M1+.
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// Well-known names; no magic strings at call sites.
const (
	// EnvPrefix namespaces environment variables (TURUTAN_*).
	EnvPrefix = "TURUTAN"
	// EnvConfigOverride points at an explicit config file or dir (highest precedence).
	EnvConfigOverride = "TURUTAN_CONFIG"
	// EnvXDGConfigHome is the XDG base directory override.
	EnvXDGConfigHome = "XDG_CONFIG_HOME"
	// ConfigFileName is the user/project config file name.
	ConfigFileName = "turutan.yml"
	// ToolName is the lowercase tool directory name.
	ToolName = "turutan"
)

// ResolveConfigPath returns the user config file path following the
// precedence in spec §8.3: explicit --config > TURUTAN_CONFIG > XDG >
// OS default > project .config/turutan.yml. It returns "" when no
// candidate exists; callers treat that as no-user-config, not fatal.
func ResolveConfigPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v, ok := os.LookupEnv(EnvConfigOverride); ok && v != "" {
		if isDir(v) {
			return filepath.Join(v, ConfigFileName)
		}
		return v
	}
	for _, p := range candidates() {
		if p != "" && fileExists(p) {
			return p
		}
	}
	return ""
}

// ResolveCacheDir returns the OS-appropriate cache root for fetched
// templates (SHA-keyed subdirs); empty when the home dir is unknown.
func ResolveCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Caches", ToolName)
	case "windows":
		if v, ok := os.LookupEnv("LOCALAPPDATA"); ok && v != "" {
			return filepath.Join(v, ToolName)
		}
		return filepath.Join(home, "AppData", "Local", ToolName)
	default:
		if v, ok := os.LookupEnv("XDG_CACHE_HOME"); ok && filepath.IsAbs(v) {
			return filepath.Join(v, ToolName)
		}
		return filepath.Join(home, ".cache", ToolName)
	}
}

// candidates lists XDG, OS-default and project config paths in order.
func candidates() []string {
	home, _ := os.UserHomeDir()
	paths := make([]string, 0, 4)
	if v, ok := os.LookupEnv(EnvXDGConfigHome); ok && filepath.IsAbs(v) {
		paths = append(paths, filepath.Join(v, ToolName, ConfigFileName))
	}
	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			paths = append(paths,
				filepath.Join(home, "Library", "Application Support", ToolName, ConfigFileName),
				filepath.Join(home, ".config", ToolName, ConfigFileName),
			)
		}
	case "windows":
		if v, ok := os.LookupEnv("APPDATA"); ok && v != "" {
			paths = append(paths, filepath.Join(v, ToolName, ConfigFileName))
		} else if home != "" {
			paths = append(paths, filepath.Join(home, "AppData", "Roaming", ToolName, ConfigFileName))
		}
	default:
		if home != "" {
			paths = append(paths, filepath.Join(home, ".config", ToolName, ConfigFileName))
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, ".config", ConfigFileName))
	}
	return paths
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
