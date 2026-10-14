// SPDX-License-Identifier: Apache-2.0

// Package condition evaluates text/template boolean predicates shared by
// file conditionals, conditional inputs, and profile diffs. It lives apart
// from internal/template so internal/config reuses the same funcs without
// an import cycle.
// ~keep
package condition

import (
	"fmt"
	"slices"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

// MissingKeyError reports the data key absent when a predicate executed.
// Callers map it onto the input-missing retry path instead of skipping.
// ~keep
type MissingKeyError struct {
	Key string
	Err error
}

func (e *MissingKeyError) Error() string {
	return e.Err.Error()
}

func (e *MissingKeyError) Unwrap() error { return e.Err }

// ParseMissingKey extracts the absent data key from a missingkey=error
// failure, or "" when the error is about something else.
// ~keep
func ParseMissingKey(err error) string {
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

// Contains matches the template `contains` override: sprig's substring order
// plus either-order slice membership, so `.profiles` works however it is
// stored. See `go doc text/template` for pipeline and function terms.
// ~keep
func Contains(a, b any) bool {
	if needle, ok := a.(string); ok {
		if haystack, ok := b.(string); ok {
			return strings.Contains(haystack, needle)
		}
		return sliceContains(b, needle)
	}
	if needle, ok := b.(string); ok {
		return sliceContains(a, needle)
	}
	return false
}

func sliceContains(haystack any, needle string) bool {
	switch v := haystack.(type) {
	case nil:
		return false
	case string:
		return strings.Contains(v, needle)
	case []string:
		return slices.Contains(v, needle)
	case []any:
		for _, el := range v {
			if s, ok := el.(string); ok {
				if s == needle {
					return true
				}
				continue
			}
			if fmt.Sprint(el) == needle {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// TemplateFuncMap is the shared template function policy: sprig minus env
// access plus the slice-aware contains override.
// ~keep
func TemplateFuncMap() map[string]any {
	funcs := sprig.TxtFuncMap()
	delete(funcs, "env")
	delete(funcs, "expandenv")
	funcs["contains"] = Contains
	return funcs
}

// ParseConditionalBool reports the boolean meaning of a rendered predicate.
// Only explicit true/false spellings (plus 1/0, yes/no, empty=false) pass;
// anything else is an authoring error.
// ~keep
func ParseConditionalBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "", "no":
		return false, nil
	default:
		return false, fmt.Errorf("conditional result %q is not a boolean (want true/false)", s)
	}
}

// EvaluatePredicate evaluates one `if` predicate against view with the same
// funcs and missingkey=error option as file conditionals. Empty predicates
// report true so undecided callers ask rather than silently skip. Missing
// keys return *MissingKeyError for the retry path, never a silent skip.
// ~keep
func EvaluatePredicate(ifExpr string, view map[string]any) (bool, error) {
	if strings.TrimSpace(ifExpr) == "" {
		return true, nil
	}
	tmpl, err := template.New("predicate").
		Option("missingkey=error").
		Funcs(TemplateFuncMap()).
		Parse(ifExpr)
	if err != nil {
		return false, fmt.Errorf("evaluating predicate: parsing: %w", err)
	}
	var rendered strings.Builder
	if err := tmpl.Execute(&rendered, view); err != nil {
		wrapped := fmt.Errorf("evaluating predicate: %w", err)
		if key := ParseMissingKey(err); key != "" {
			return false, &MissingKeyError{Key: key, Err: wrapped}
		}
		return false, wrapped
	}
	enabled, err := ParseConditionalBool(strings.TrimSpace(rendered.String()))
	if err != nil {
		return false, fmt.Errorf("evaluating predicate: %w", err)
	}
	return enabled, nil
}
