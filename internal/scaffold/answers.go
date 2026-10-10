// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

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
