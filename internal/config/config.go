// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const (
	EnvPrefix           = "TURUTAN"
	EnvConfigOverride   = "TURUTAN_CONFIG"
	EnvTemplateOverride = "TURUTAN_TEMPLATE"
	TemplateConfigKey   = "template"
	EnvXDGConfigHome    = "XDG_CONFIG_HOME"
	ConfigFileName      = "turutan.yml"
	ConfigFileNameJSON  = "turutan.json"
	ToolName            = "turutan"
)

func ResolveConfigPath(explicit string) string {
	if v, ok := os.LookupEnv(EnvConfigOverride); ok && v != "" {
		if isDir(v) {
			yml := filepath.Join(v, ConfigFileName)
			if fileExists(yml) {
				return yml
			}
			jsonPath := filepath.Join(v, ConfigFileNameJSON)
			if fileExists(jsonPath) {
				return jsonPath
			}
			return yml
		}
		return v
	}
	if explicit != "" {
		return explicit
	}
	for _, p := range candidates() {
		if p != "" && fileExists(p) {
			return p
		}
	}
	return ""
}

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

func candidates() []string {
	home, _ := os.UserHomeDir()
	paths := make([]string, 0, 8)
	appendDir := func(dir string) {
		paths = append(paths,
			filepath.Join(dir, ConfigFileName),
			filepath.Join(dir, ConfigFileNameJSON),
		)
	}
	if v, ok := os.LookupEnv(EnvXDGConfigHome); ok && filepath.IsAbs(v) {
		appendDir(filepath.Join(v, ToolName))
	}
	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			appendDir(filepath.Join(home, "Library", "Application Support", ToolName))
			appendDir(filepath.Join(home, ".config", ToolName))
		}
	case "windows":
		if v, ok := os.LookupEnv("APPDATA"); ok && v != "" {
			appendDir(filepath.Join(v, ToolName))
		} else if home != "" {
			appendDir(filepath.Join(home, "AppData", "Roaming", ToolName))
		}
	default:
		if home != "" {
			appendDir(filepath.Join(home, ".config", ToolName))
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		appendDir(filepath.Join(cwd, ".config"))
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
