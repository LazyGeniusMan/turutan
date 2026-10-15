// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrInvalidAuthor is matched with errors.Is when author text fails validation.
// ~keep
var ErrInvalidAuthor = errors.New("invalid author")

var emailRe = regexp.MustCompile(`^[^<>()\s@]+@[^<>()\s@]+\.[^<>()\s@]+$`)

// Author identifies a template author or contributor in the canonical form
// "Name <email> (website)", where email and website are optional.
// ~keep
type Author struct {
	Name    string
	Email   string
	Website string
}

// ParseAuthor parses canonical author text into an Author.
// ~keep
func ParseAuthor(raw string) (Author, error) {
	var author Author
	rest := strings.TrimSpace(raw)
	if rest == "" {
		return author, fmt.Errorf("%w: author is empty", ErrInvalidAuthor)
	}
	if idx := strings.LastIndex(rest, "("); idx >= 0 && strings.HasSuffix(rest, ")") {
		author.Website = strings.TrimSpace(rest[idx+1 : len(rest)-1])
		rest = strings.TrimSpace(rest[:idx])
	}
	if idx := strings.LastIndex(rest, "<"); idx >= 0 && strings.HasSuffix(rest, ">") {
		author.Email = strings.TrimSpace(rest[idx+1 : len(rest)-1])
		rest = strings.TrimSpace(rest[:idx])
	}
	author.Name = rest
	if err := author.Validate(); err != nil {
		return Author{}, err
	}
	return author, nil
}

// Validate reports whether the Author carries a usable name and well-formed
// optional email and website values.
// ~keep
func (a Author) Validate() error {
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidAuthor)
	}
	if strings.ContainsAny(a.Name, "<>()") {
		return fmt.Errorf("%w: name %q must not contain <>()", ErrInvalidAuthor, a.Name)
	}
	if a.Email != "" && !emailRe.MatchString(a.Email) {
		return fmt.Errorf("%w: email %q is not a valid address", ErrInvalidAuthor, a.Email)
	}
	if a.Website != "" {
		parsed, err := url.Parse(a.Website)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("%w: website %q must be an absolute http(s) URL", ErrInvalidAuthor, a.Website)
		}
	}
	return nil
}

// String returns the canonical "Name <email> (website)" form, omitting
// absent parts.
// ~keep
func (a Author) String() string {
	var out strings.Builder
	out.WriteString(a.Name)
	if a.Email != "" {
		out.WriteString(" <" + a.Email + ">")
	}
	if a.Website != "" {
		out.WriteString(" (" + a.Website + ")")
	}
	return out.String()
}

// MarshalYAML renders the Author in canonical string form.
// ~keep
func (a Author) MarshalYAML() (any, error) {
	return a.String(), nil
}

// UnmarshalYAML parses canonical author string form.
// ~keep
func (a *Author) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return err
	}
	parsed, err := ParseAuthor(raw)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}
