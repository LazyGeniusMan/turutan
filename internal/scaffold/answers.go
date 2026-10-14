// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/template"
)

const maxPromptAttempts = 10

func AsMissingKey(err error) (string, bool) {
	if missing, ok := errors.AsType[*template.MissingKeyError](err); ok {
		return missing.Key, true
	}
	if key := missingKey(err); key != "" {
		return key, true
	}
	return "", false
}

func missingKey(err error) string {
	const marker = `no entry for key "`
	msg := err.Error()
	_, after, ok := strings.Cut(msg, marker)
	if !ok {
		return ""
	}
	key, _, ok := strings.Cut(after, `"`)
	if !ok {
		return ""
	}
	return key
}

func LoadAnswersFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- reads CLI-provided answers file; caller-intended file read
	if err != nil {
		return nil, fmt.Errorf("reading answers file %q: %w", path, err)
	}
	answers := map[string]any{}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		if err := json.Unmarshal(data, &answers); err != nil {
			return nil, fmt.Errorf("parsing answers file %q: %w", path, err)
		}
		return answers, nil
	}
	if err := yaml.Unmarshal(data, &answers); err != nil {
		return nil, fmt.Errorf("parsing answers file %q: %w", path, err)
	}
	return answers, nil
}

func seedAnswers(target string, opts Options) (map[string]any, error) {
	answers := map[string]any{}
	if opts.Defaults {
		answers["project_name"] = filepath.Base(target)
	}
	if opts.AnswersFile != "" {
		fileAnswers, err := LoadAnswersFile(opts.AnswersFile)
		if err != nil {
			return nil, err
		}
		maps.Copy(answers, fileAnswers)
	}
	return answers, nil
}

func seedAnswersWithManifest(
	target string,
	opts Options,
	manifest *config.Manifest,
) (map[string]any, error) {
	answers, err := seedAnswers(target, opts)
	if err != nil {
		return nil, err
	}
	normalizeAnswersWithManifest(answers, manifest)
	if manifest != nil && opts.Defaults {
		for key, def := range manifest.Prompts {
			if _, ok := answers[key]; ok {
				continue
			}
			if def.Default == nil {
				continue
			}
			answers[key] = manifestDefaultCopy(def.Default)
		}
		normalizeAnswersWithManifest(answers, manifest)
	}
	return answers, nil
}

func normalizeAnswersWithManifest(answers map[string]any, manifest *config.Manifest) {
	if manifest == nil {
		return
	}
	for key, def := range manifest.Prompts {
		if !isArrayPrompt(def) {
			continue
		}
		val, ok := answers[key]
		if !ok || val == nil {
			continue
		}
		if _, ok := val.([]string); ok {
			continue
		}
		answers[key] = normalizeArrayValue(val)
	}
	if val, ok := answers["enabled_profiles"]; ok && val != nil {
		if list, ok := val.([]string); ok {
			answers["enabled_profiles"] = config.NormalizeProfiles(list)
		}
	}
}

func isArrayPrompt(def config.ManifestPrompt) bool {
	if def.Type == "array" {
		return true
	}
	switch def.Default.(type) {
	case []string, []any:
		return true
	}
	return false
}

func manifestDefaultCopy(def any) any {
	switch v := def.(type) {
	case []string:
		out := make([]string, len(v))
		copy(out, v)
		return out
	case []any:
		out := make([]any, len(v))
		copy(out, v)
		return out
	default:
		return def
	}
}

func seedMissingDefaults(answers map[string]any, manifest *config.Manifest) {
	if manifest == nil {
		return
	}
	for key, def := range manifest.Prompts {
		if _, ok := answers[key]; ok {
			continue
		}
		if def.Default == nil {
			continue
		}
		answers[key] = manifestDefaultCopy(def.Default)
	}
}

func normalizeArrayValue(val any) []string {
	switch v := val.(type) {
	case []string:
		out := make([]string, 0, len(v))
		seen := map[string]bool{}
		for _, el := range v {
			trimmed := strings.TrimSpace(el)
			if trimmed == "" || seen[trimmed] {
				continue
			}
			seen[trimmed] = true
			out = append(out, trimmed)
		}
		return out
	case []any:
		out := []string{}
		seen := map[string]bool{}
		for _, el := range v {
			s := strings.TrimSpace(fmt.Sprint(el))
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
		return out
	case string:
		return splitDedupeList(v)
	default:
		s := strings.TrimSpace(fmt.Sprint(val))
		if s == "" {
			return []string{}
		}
		return splitDedupeList(s)
	}
}

func splitDedupeList(s string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

func promptOrder(manifest *config.Manifest) []string {
	if manifest == nil {
		return nil
	}
	priority := []string{"project_name", "project_description", "enabled_profiles"}
	var ordered []string
	seen := map[string]bool{}
	for _, key := range priority {
		if _, ok := manifest.Prompts[key]; ok {
			ordered = append(ordered, key)
			seen[key] = true
		}
	}
	var rest []string
	for key := range manifest.Prompts {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

func promptForKey(
	stdout io.Writer,
	stdin io.Reader,
	key string,
	def config.ManifestPrompt,
) (any, error) {
	if def.Type == "array" || (def.Type == "" && isArrayPrompt(def)) {
		return promptArrayValue(stdout, stdin, key, def)
	}
	switch def.Type {
	case "bool":
		return promptBoolValue(stdout, stdin, key, def)
	default:
		return promptStringValue(stdout, stdin, key, def)
	}
}

func promptArrayValue(
	stdout io.Writer,
	stdin io.Reader,
	key string,
	def config.ManifestPrompt,
) (any, error) {
	defaultList := normalizeArrayValue(def.Default)
	if def.Default == nil {
		defaultList = nil
	}
	var prompt string
	switch {
	case def.Default != nil:
		prompt = fmt.Sprintf("Enter value for %s (comma-separated) [%s]: ", key, strings.Join(defaultList, ","))
	default:
		prompt = fmt.Sprintf("Enter value for %s (comma-separated): ", key)
	}
	if _, err := fmt.Fprint(stdout, prompt); err != nil {
		return nil, err
	}
	line, err := readPromptLine(stdin)
	if err != nil && len(line) == 0 {
		return nil, fmt.Errorf("reading answer for %q: %w", key, err)
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		if def.Default != nil {
			out := make([]string, len(defaultList))
			copy(out, defaultList)
			return out, nil
		}
		return []string{}, nil
	}
	values := splitDedupeList(trimmed)
	if len(def.Options) > 0 {
		for _, v := range values {
			if !slices.Contains(def.Options, v) {
				return nil, fmt.Errorf("invalid value %q for %q (allowed: %s)", v, key, strings.Join(def.Options, ", "))
			}
		}
	}
	return values, nil
}

func promptStringValue(
	stdout io.Writer,
	stdin io.Reader,
	key string,
	def config.ManifestPrompt,
) (any, error) {
	label := key
	if def.Prompt != "" {
		label = def.Prompt
	}
	var defaultStr string
	hasDefault := def.Default != nil
	if hasDefault {
		switch v := def.Default.(type) {
		case string:
			defaultStr = v
		case []string:
			defaultStr = strings.Join(v, ",")
		case []any:
			parts := make([]string, 0, len(v))
			for _, el := range v {
				parts = append(parts, fmt.Sprint(el))
			}
			defaultStr = strings.Join(parts, ",")
		default:
			defaultStr = fmt.Sprint(v)
		}
	}
	var prompt string
	if hasDefault {
		prompt = fmt.Sprintf("Enter %s [%s]: ", label, defaultStr)
	} else {
		prompt = fmt.Sprintf("Enter %s: ", label)
	}
	if _, err := fmt.Fprint(stdout, prompt); err != nil {
		return nil, err
	}
	line, err := readPromptLine(stdin)
	if err != nil && len(line) == 0 {
		return nil, fmt.Errorf("reading answer for %q: %w", key, err)
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" && hasDefault {
		return defaultStr, nil
	}
	if trimmed != "" && len(def.Options) > 0 {
		if !slices.Contains(def.Options, trimmed) {
			return nil, fmt.Errorf("invalid value %q for %q (allowed: %s)", trimmed, key, strings.Join(def.Options, ", "))
		}
	}
	return trimmed, nil
}

func promptBoolValue(
	stdout io.Writer,
	stdin io.Reader,
	key string,
	def config.ManifestPrompt,
) (any, error) {
	label := key
	if def.Prompt != "" {
		label = def.Prompt
	}
	var defaultStr string
	hasDefault := def.Default != nil
	if hasDefault {
		switch v := def.Default.(type) {
		case bool:
			if v {
				defaultStr = "true"
			} else {
				defaultStr = "false"
			}
		case string:
			defaultStr = v
		default:
			defaultStr = fmt.Sprint(v)
		}
	}
	var prompt string
	if hasDefault {
		prompt = fmt.Sprintf("Enter %s [%s]: ", label, defaultStr)
	} else {
		prompt = fmt.Sprintf("Enter %s (true/false): ", label)
	}
	if _, err := fmt.Fprint(stdout, prompt); err != nil {
		return nil, err
	}
	line, err := readPromptLine(stdin)
	if err != nil && len(line) == 0 {
		return nil, fmt.Errorf("reading answer for %q: %w", key, err)
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		if !hasDefault {
			return nil, fmt.Errorf("missing value for %q", key)
		}
		return parseBoolPrompt(strings.TrimSpace(defaultStr), key)
	}
	return parseBoolPrompt(trimmed, key)
}

func readPromptLine(stdin io.Reader) (string, error) {
	var buf []byte
	one := make([]byte, 1)
	for {
		n, err := stdin.Read(one)
		if n > 0 {
			buf = append(buf, one[0])
			if one[0] == '\n' {
				break
			}
		}
		if err != nil {
			if len(buf) == 0 {
				return "", err
			}
			break
		}
	}
	return string(buf), nil
}

func parseBoolPrompt(s, key string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "1", "yes", "y":
		return true, nil
	case "false", "0", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("invalid value %q for %q (want true/false)", s, key)
	}
}

func promptValue(stdout io.Writer, stdin io.Reader, key string) (string, error) {
	if _, err := fmt.Fprintf(stdout, "Enter value for %s: ", key); err != nil {
		return "", err
	}
	line, err := readPromptLine(stdin)
	if err != nil && len(line) == 0 {
		return "", fmt.Errorf("reading answer for %q: %w", key, err)
	}
	return strings.TrimSpace(line), nil
}

func promptConfirm(stdout io.Writer, stdin io.Reader, question string) (bool, error) {
	if _, err := fmt.Fprintf(stdout, "%s [y/N]: ", question); err != nil {
		return false, err
	}
	line, _ := readPromptLine(stdin)
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func manifestConditionals(manifest *config.Manifest) []template.Conditional {
	if manifest == nil {
		return nil
	}
	conds := make([]template.Conditional, 0, len(manifest.Conditionals))
	for _, c := range manifest.Conditionals {
		conds = append(conds, template.Conditional{If: c.If, Skip: c.Skip})
	}
	return conds
}

func profileIncludeFunc(manifest *config.Manifest, answers map[string]any) func(string) (bool, error) {
	if manifest == nil || len(manifest.Profiles) == 0 {
		return nil
	}
	enabled := config.NormalizeProfiles(config.GetProfiles(answers))
	return func(slash string) (bool, error) {
		return manifest.IsIncluded(slash, enabled)
	}
}
