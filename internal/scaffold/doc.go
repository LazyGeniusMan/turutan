// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
)

// plantDocFiles moves manifest doc-files from their rendered stage location
// into stage/.config/turutan, preserving relative structure. Unlike
// plantLicenseFiles the source is the rendered stage (docs render like any
// other template file), and a missing entry is skipped silently so
// profile-gated docs vanish cleanly when their profile is off. Runs after
// rendering and after plantLicenseFiles, before publish or walk.
func plantDocFiles(stage string, manifest *config.Manifest) error {
	if manifest == nil || len(manifest.DocFiles) == 0 {
		return nil
	}
	for _, entry := range manifest.DocFiles {
		slash := filter.ToSlash(entry)
		src, err := filter.SafeJoin(stage, filepath.FromSlash(slash))
		if err != nil {
			return fmt.Errorf("doc-files: %w", err)
		}
		data, err := os.ReadFile(src) // #nosec G304 -- reads manifest-declared doc file from this run's staging dir, not caller-controlled inclusion
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("doc-files: reading staged %q: %w", entry, err)
		}
		dest := ".config/turutan/" + slash
		if err := filter.WriteFileWithinRoot(stage, dest, data, 0o644); err != nil {
			return fmt.Errorf("doc-files: writing %q: %w", dest, err)
		}
		if err := filter.RemoveWithinRoot(stage, slash); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("doc-files: removing staged %q: %w", entry, err)
		}
	}
	return nil
}
