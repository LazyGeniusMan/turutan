// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	EnvPrefix           = "TURUTAN"
	EnvConfigOverride   = "TURUTAN_CONFIG"
	EnvTemplateOverride = "TURUTAN_TEMPLATE"
	TemplateConfigKey   = "template"
	RegistriesConfigKey = "registries"
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

// UserConfig is the per-user turutan configuration file shape: a default
// template plus named registry references (`registries: {name: uri}`).
// Unknown top-level keys are preserved when registries are updated.
// ~keep
type UserConfig struct {
	Template   string            `yaml:"template,omitempty" json:"template,omitempty" mapstructure:"template"`
	Registries map[string]string `yaml:"registries,omitempty" json:"registries,omitempty" mapstructure:"registries"`
}

// DefaultUserConfigPath returns the writable default user config path: the
// first candidate directory joined with the YAML file name, even when no
// config file exists yet.
// ~keep
func DefaultUserConfigPath() string {
	home, _ := os.UserHomeDir()
	if v, ok := os.LookupEnv(EnvXDGConfigHome); ok && filepath.IsAbs(v) {
		return filepath.Join(v, ToolName, ConfigFileName)
	}
	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			return filepath.Join(home, "Library", "Application Support", ToolName, ConfigFileName)
		}
	case "windows":
		if v, ok := os.LookupEnv("APPDATA"); ok && v != "" {
			return filepath.Join(v, ToolName, ConfigFileName)
		}
		if home != "" {
			return filepath.Join(home, "AppData", "Roaming", ToolName, ConfigFileName)
		}
	default:
		if home != "" {
			return filepath.Join(home, ".config", ToolName, ConfigFileName)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Join(cwd, ".config", ConfigFileName)
	}
	return ConfigFileName
}

// LoadUserConfigFile reads path into a UserConfig. A missing or empty file
// yields an empty config; other errors report the offending path.
// ~keep
func LoadUserConfigFile(path string) (*UserConfig, error) {
	doc, err := readConfigMap(path)
	if err != nil {
		return nil, err
	}
	var cfg UserConfig
	if len(doc) == 0 {
		return &cfg, nil
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encoding %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", path, err)
	}
	if cfg.Registries == nil {
		cfg.Registries = map[string]string{}
	}
	return &cfg, nil
}

// LoadRegistries returns the named registry references stored in path. A
// missing, empty or registry-free file yields an empty map.
// ~keep
func LoadRegistries(path string) (map[string]string, error) {
	if strings.TrimSpace(path) == "" {
		return map[string]string{}, nil
	}
	cfg, err := LoadUserConfigFile(path)
	if err != nil {
		return nil, err
	}
	if cfg.Registries == nil {
		return map[string]string{}, nil
	}
	return cfg.Registries, nil
}

// UpsertRegistry stores the name -> uri reference in the user config at
// path, creating parent directories as needed. Every other top-level key
// already present in the file is preserved byte-for-byte in value.
// ~keep
func UpsertRegistry(path, name, uri string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("registry config path is empty")
	}
	doc, err := readConfigMap(path)
	if err != nil {
		return err
	}
	regs, ok := doc[RegistriesConfigKey].(map[string]any)
	if !ok || regs == nil {
		regs = map[string]any{}
		doc[RegistriesConfigKey] = regs
	}
	regs[name] = uri
	return writeConfigMap(path, doc)
}

func readConfigMap(path string) (map[string]any, error) {
	doc := map[string]any{}
	data, err := os.ReadFile(path) // #nosec G304 -- path is the explicit caller-supplied config file
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return doc, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func writeConfigMap(path string, doc map[string]any) error {
	var (
		data []byte
		err  error
	)
	if strings.EqualFold(filepath.Ext(path), ".json") {
		data, err = json.MarshalIndent(doc, "", "  ")
	} else {
		data, err = yaml.Marshal(doc)
	}
	if err != nil {
		return fmt.Errorf("encoding %q: %w", path, err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %q: %w", path, err)
	}
	return nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
