// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"io/fs"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

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
