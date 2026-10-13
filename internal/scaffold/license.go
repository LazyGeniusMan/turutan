// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
)

func resolveTemplateLicense(fsys fs.FS, manifest *config.Manifest) string {
	if manifest != nil && strings.TrimSpace(manifest.License) != "" {
		return strings.TrimSpace(manifest.License)
	}
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

func plantLicenseFiles(fetchedDir, stage string, manifest *config.Manifest) error {
	if manifest == nil || len(manifest.LicenseFiles) == 0 {
		return nil
	}
	for _, entry := range manifest.LicenseFiles {
		slash := filter.ToSlash(entry)
		src, err := filter.SafeJoin(fetchedDir, filepath.FromSlash(slash))
		if err != nil {
			return fmt.Errorf("license-files: %w", err)
		}
		data, err := os.ReadFile(src) // #nosec G304 -- reads manifest-declared license file from the fetched template tree, not caller-controlled inclusion
		if err != nil {
			return fmt.Errorf("license-files: template file %q is missing: %w", entry, err)
		}
		dest := ".config/turutan/" + slash
		if err := filter.WriteFileWithinRoot(stage, dest, data, 0o644); err != nil {
			return fmt.Errorf("license-files: writing %q: %w", dest, err)
		}
		if err := filter.RemoveWithinRoot(stage, slash); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("license-files: removing staged %q: %w", entry, err)
		}
	}
	return nil
}
