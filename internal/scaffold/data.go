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

const maxInputAttempts = 10

// AsMissingKey reports the missing data key for a render failure.
// ~keep
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

// LoadDataFile reads template data from a YAML or JSON file.
// ~keep
func LoadDataFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- reads CLI-provided data file; caller-intended file read
	if err != nil {
		return nil, fmt.Errorf("reading data file %q: %w", path, err)
	}
	data := map[string]any{}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, fmt.Errorf("parsing data file %q: %w", path, err)
		}
		delete(data, "profiles")
		return data, nil
	}
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parsing data file %q: %w", path, err)
	}
	delete(data, "profiles")
	return data, nil
}

func seedData(target string, opts Options) (map[string]any, error) {
	data := map[string]any{}
	if opts.Defaults {
		data["project_name"] = filepath.Base(target)
	}
	if opts.DataFile != "" {
		fileData, err := LoadDataFile(opts.DataFile)
		if err != nil {
			return nil, err
		}
		maps.Copy(data, fileData)
	}
	delete(data, "profiles")
	return data, nil
}

func seedDataWithManifest(
	target string,
	opts Options,
	manifest *config.Manifest,
) (map[string]any, error) {
	data, err := seedData(target, opts)
	if err != nil {
		return nil, err
	}
	normalizeDataWithManifest(data, manifest)
	if manifest != nil && opts.Defaults {
		for key, def := range manifest.Inputs {
			if _, ok := data[key]; ok {
				continue
			}
			if def.Default == nil {
				continue
			}
			data[key] = manifestDefaultCopy(def.Default)
		}
		normalizeDataWithManifest(data, manifest)
	}
	return data, nil
}

func normalizeDataWithManifest(data map[string]any, manifest *config.Manifest) {
	if manifest == nil {
		return
	}
	for key, def := range manifest.Inputs {
		if !isArrayInput(def) {
			continue
		}
		val, ok := data[key]
		if !ok || val == nil {
			continue
		}
		if _, ok := val.([]string); ok {
			continue
		}
		data[key] = normalizeArrayValue(val)
	}
	delete(data, "profiles")
}

func isArrayInput(def config.ManifestInput) bool {
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

func seedMissingDefaults(data map[string]any, manifest *config.Manifest) {
	if manifest == nil {
		return
	}
	for key, def := range manifest.Inputs {
		if _, ok := data[key]; ok {
			continue
		}
		if def.Default == nil {
			continue
		}
		data[key] = manifestDefaultCopy(def.Default)
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

func splitProfilesValue(ids []string) []string {
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

func normalizeProfilesSelection(ids []string) []string {
	expanded := splitProfilesValue(ids)
	if len(expanded) == 0 {
		expanded = append([]string{}, config.DefaultProfiles...)
	}
	return config.NormalizeProfiles(expanded)
}

func inputOrder(manifest *config.Manifest) []string {
	if manifest == nil {
		return nil
	}
	priority := []string{"project_name", "project_description"}
	var ordered []string
	seen := map[string]bool{}
	for _, key := range priority {
		if _, ok := manifest.Inputs[key]; ok {
			ordered = append(ordered, key)
			seen[key] = true
		}
	}
	var rest []string
	for key := range manifest.Inputs {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

func inputForKey(
	stdout io.Writer,
	stdin io.Reader,
	key string,
	def config.ManifestInput,
) (any, error) {
	if def.Type == "array" || (def.Type == "" && isArrayInput(def)) {
		return inputArrayValue(stdout, stdin, key, def)
	}
	switch def.Type {
	case "bool":
		return inputBoolValue(stdout, stdin, key, def)
	default:
		return inputStringValue(stdout, stdin, key, def)
	}
}

func inputLabel(def config.ManifestInput, key string) string {
	if def.Label != "" {
		return def.Label
	}
	return key
}

func printInputOptions(stdout io.Writer, options []string) error {
	if len(options) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(stdout, "Possible values: %s\n", strings.Join(options, ", ")); err != nil {
		return err
	}
	return nil
}

func inputArrayValue(
	stdout io.Writer,
	stdin io.Reader,
	key string,
	def config.ManifestInput,
) (any, error) {
	defaultList := normalizeArrayValue(def.Default)
	if def.Default == nil {
		defaultList = nil
	}
	label := inputLabel(def, key)
	if err := printInputOptions(stdout, def.Options); err != nil {
		return nil, err
	}
	prompt := label
	if !strings.Contains(strings.ToLower(label), "comma-separated") {
		prompt += " (comma-separated)"
	}
	var text string
	switch {
	case def.Default != nil:
		text = fmt.Sprintf("Enter %s [%s]: ", prompt, strings.Join(defaultList, ","))
	default:
		text = fmt.Sprintf("Enter %s: ", prompt)
	}
	if _, err := fmt.Fprint(stdout, text); err != nil {
		return nil, err
	}
	line, err := readInputLine(stdin)
	if err != nil && len(line) == 0 {
		return nil, fmt.Errorf("reading value for %q: %w", key, err)
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

func inputStringValue(
	stdout io.Writer,
	stdin io.Reader,
	key string,
	def config.ManifestInput,
) (any, error) {
	label := inputLabel(def, key)
	if err := printInputOptions(stdout, def.Options); err != nil {
		return nil, err
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
	var text string
	if hasDefault {
		text = fmt.Sprintf("Enter %s [%s]: ", label, defaultStr)
	} else {
		text = fmt.Sprintf("Enter %s: ", label)
	}
	if _, err := fmt.Fprint(stdout, text); err != nil {
		return nil, err
	}
	line, err := readInputLine(stdin)
	if err != nil && len(line) == 0 {
		return nil, fmt.Errorf("reading value for %q: %w", key, err)
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

func inputBoolValue(
	stdout io.Writer,
	stdin io.Reader,
	key string,
	def config.ManifestInput,
) (any, error) {
	label := inputLabel(def, key)
	if err := printInputOptions(stdout, def.Options); err != nil {
		return nil, err
	}
	var defaultStr string
	hasDefault := def.Default != nil
	if hasDefault {
		switch v := def.Default.(type) {
		case bool:
			if v {
				defaultStr = "y"
			} else {
				defaultStr = "n"
			}
		case string:
			if parsed, err := parseBoolInput(strings.TrimSpace(v), key); err == nil {
				if parsed {
					defaultStr = "y"
				} else {
					defaultStr = "n"
				}
			} else {
				defaultStr = v
			}
		default:
			defaultStr = fmt.Sprint(v)
		}
	}
	prompt := label
	if !strings.Contains(strings.ToLower(label), "y/n") {
		prompt += " (y/n)"
	}
	var text string
	if hasDefault {
		text = fmt.Sprintf("Enter %s [%s]: ", prompt, defaultStr)
	} else {
		text = fmt.Sprintf("Enter %s: ", prompt)
	}
	if _, err := fmt.Fprint(stdout, text); err != nil {
		return nil, err
	}
	line, err := readInputLine(stdin)
	if err != nil && len(line) == 0 {
		return nil, fmt.Errorf("reading value for %q: %w", key, err)
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		if !hasDefault {
			return nil, fmt.Errorf("missing value for %q", key)
		}
		return parseBoolInput(strings.TrimSpace(defaultStr), key)
	}
	return parseBoolInput(trimmed, key)
}

func readInputLine(stdin io.Reader) (string, error) {
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

func parseBoolInput(s, key string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "1", "yes", "y":
		return true, nil
	case "false", "0", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("invalid value %q for %q (want y/n)", s, key)
	}
}

func inputValue(stdout io.Writer, stdin io.Reader, key string) (string, error) {
	if _, err := fmt.Fprintf(stdout, "Enter %s: ", key); err != nil {
		return "", err
	}
	line, err := readInputLine(stdin)
	if err != nil && len(line) == 0 {
		return "", fmt.Errorf("reading value for %q: %w", key, err)
	}
	return strings.TrimSpace(line), nil
}

func askConfirm(stdout io.Writer, stdin io.Reader, prompt string) (bool, error) {
	if _, err := fmt.Fprintf(stdout, "%s [y/N]: ", prompt); err != nil {
		return false, err
	}
	line, _ := readInputLine(stdin)
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
		conds = append(conds, template.Conditional{If: c.If, Includes: c.Includes, Excludes: c.Excludes})
	}
	return conds
}

func profileIncludeFunc(manifest *config.Manifest, profiles []string) func(string) (bool, error) {
	if manifest == nil || len(manifest.Profiles) == 0 {
		return nil
	}
	enabled := config.NormalizeProfiles(splitProfilesValue(profiles))
	return func(slash string) (bool, error) {
		return manifest.IsIncluded(slash, enabled)
	}
}

// renderDataView exposes native profiles as read-only dot `.profiles`
// alongside input keys. See `go doc text/template` for data, dot, action,
// pipeline, variable, and function terms.
// ~keep
func renderDataView(data map[string]any, profiles []string) map[string]any {
	view := make(map[string]any, len(data)+1)
	maps.Copy(view, data)
	view["profiles"] = config.NormalizeProfiles(splitProfilesValue(profiles))
	return view
}

// ShouldAskInput reports whether input key must be prompted, evaluating its
// `if` predicate against dataView (built by renderDataView, so `.profiles`
// plus answered inputs are visible) with the same funcs and
// missingkey=error option as file conditionals. Empty `if` means always
// ask. Unknown keys ask. Missing keys in the predicate return the
// input-missing retry error, never a silent skip.
//
// Skipped inputs take the author default with no prompt, even when
// interactive; a skipped input with no default stays absent, so a later
// `{{.key}}` render reference missingkey-errors and re-asks only when the
// predicate holds by then.
// ~keep
func ShouldAskInput(manifest *config.Manifest, key string, dataView map[string]any) (bool, error) {
	if manifest == nil {
		return true, nil
	}
	def, ok := manifest.Inputs[key]
	if !ok {
		return true, nil
	}
	if strings.TrimSpace(def.If) == "" {
		return true, nil
	}
	return template.EvaluatePredicate(def.If, dataView)
}
