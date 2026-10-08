// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxPromptAttempts bounds interactive answer prompting so a template
// referencing endless keys cannot loop forever.
const maxPromptAttempts = 10

// missingKey extracts the template key from a missingkey=error failure
// (exec: map has no entry for key "name"). It returns "" when err is some
// other render failure.
func missingKey(err error) string {
	const marker = `no entry for key "`
	msg := err.Error()
	_, after, ok := strings.Cut(msg, marker)
	if !ok {
		return ""
	}
	rest := after
	before0, _, ok0 := strings.Cut(rest, `"`)
	if !ok0 {
		return ""
	}
	return before0
}

// LoadAnswersFile reads answers from path: JSON for .json, YAML otherwise
// (YAML is a superset of JSON, so .yaml/.yml cover both styles).
func LoadAnswersFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
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

// seedAnswers merges --defaults seeds with the --answers-file overlay.
// File values win over defaults.
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

// promptValue asks for one answer on stdout, reading a line from stdin.
func promptValue(stdout io.Writer, stdin io.Reader, key string) (string, error) {
	if _, err := fmt.Fprintf(stdout, "Enter value for %s: ", key); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", fmt.Errorf("reading answer for %q: %w", key, err)
	}
	return strings.TrimSpace(line), nil
}

// promptConfirm asks a yes/no question, defaulting to no on empty input
// or unreadable stdin.
func promptConfirm(stdout io.Writer, stdin io.Reader, question string) bool {
	fmt.Fprintf(stdout, "%s [y/N]: ", question)
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
