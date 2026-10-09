// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"io/fs"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

// resolveTemplateLicense reads the source template license for the state
// record (spec §8.4): TEMPLATE_LICENSE wins (it avoids confusion with the
// root Apache LICENSE), then LICENSE, then the MIT-0 default. Each file
// is mapped to its SPDX identifier by content; an unreadable or
// unrecognized file falls through to the next candidate.
func resolveTemplateLicense(fsys fs.FS) string {
	for _, name := range []string{"TEMPLATE_LICENSE", "LICENSE"} {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			continue
		}
		if id := detectSPDX(string(data)); id != "" {
			return id
		}
	}
	return config.TemplateLicenseMIT0
}

// detectSPDX maps license text to its SPDX identifier, or "" when the
// text matches nothing known. MIT-0 is checked before MIT because MIT-0
// text also carries the MIT permission grant.
func detectSPDX(text string) string {
	upper := strings.ToUpper(text)
	switch {
	case strings.Contains(upper, "MIT NO ATTRIBUTION") || strings.Contains(upper, "MIT-0"):
		return "MIT-0"
	case strings.Contains(upper, "APACHE LICENSE") && strings.Contains(upper, "VERSION 2.0"):
		return "Apache-2.0"
	case strings.Contains(upper, "PERMISSION IS HEREBY GRANTED"):
		return "MIT"
	default:
		return ""
	}
}
