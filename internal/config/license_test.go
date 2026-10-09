// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// License gates (spec §8.4): the CLI tree is Apache-2.0, the default
// template is MIT-0, and generated output records MIT-0. The dependency
// license audit (go-licenses, deny GPL/AGPL) is a manual release step,
// not a unit test: it needs the module graph, not source text:
//
//	go run github.com/google/go-licenses@latest check ./...
//
// Repo layout assumed: this package lives at internal/config, so the
// module root is two dirs up.

// goDirsWithHeaders lists engine source trees requiring SPDX headers.
var goDirsWithHeaders = []string{"cmd", "internal"}

// TestLicenseHeaders collects every Go file under cmd/ and internal/
// missing the Apache-2.0 SPDX header and fails once with the full list.
// Files led by a //go:build line carry the header right below it.
func TestLicenseHeaders(t *testing.T) {
	t.Run("SPDX present on engine sources", func(t *testing.T) {
		root := moduleRoot(t)
		var missing []string
		for _, dir := range goDirsWithHeaders {
			err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || !strings.HasSuffix(path, ".go") {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if !hasSPDXHeader(string(data)) {
					rel, _ := filepath.Rel(root, path)
					missing = append(missing, rel)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		if len(missing) > 0 {
			t.Errorf("files missing Apache-2.0 SPDX header:\n  %s", strings.Join(missing, "\n  "))
		}
	})
}

// TestTemplateLicense enforces the MIT-0 side of the split: the default
// template carries its own license file and no Apache-licensed snippets.
func TestTemplateLicense(t *testing.T) {
	t.Run("license file present", func(t *testing.T) {
		dir := filepath.Join(moduleRoot(t), "templates", "default")
		for _, name := range []string{"TEMPLATE_LICENSE", "LICENSE"} {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
				return
			}
		}
		t.Error("templates/default holds neither TEMPLATE_LICENSE nor LICENSE")
	})
	t.Run("MIT-0 text", func(t *testing.T) {
		path := templateLicensePath(t)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"MIT", "Permission is hereby granted"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s missing %q", path, want)
			}
		}
	})
	t.Run("no Apache snippets in template", func(t *testing.T) {
		dir := filepath.Join(moduleRoot(t), "templates", "default")
		var offenders []string
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(strings.ToLower(string(data)), "apache license") {
				rel, _ := filepath.Rel(dir, path)
				offenders = append(offenders, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(offenders) > 0 {
			t.Errorf("Apache-licensed snippets under templates/default (fails build per §8.4):\n  %s",
				strings.Join(offenders, "\n  "))
		}
	})
}

// TestRootLicenseApache pins the CLI side of the split: the root LICENSE
// is the Apache-2.0 full text.
func TestRootLicenseApache(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(moduleRoot(t), "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Apache License", "Version 2.0"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("root LICENSE missing %q", want)
		}
	}
}

// moduleRoot returns the repo root: two dirs above this package dir.
func moduleRoot(t *testing.T) string {
	t.Helper()
	// Tests run with the package dir as working directory.
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", root, err)
	}
	return root
}

// templateLicensePath returns the template license file, preferring
// TEMPLATE_LICENSE (it avoids confusion with the root Apache LICENSE).
func templateLicensePath(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(moduleRoot(t), "templates", "default")
	for _, name := range []string{"TEMPLATE_LICENSE", "LICENSE"} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	t.Fatal("templates/default holds neither TEMPLATE_LICENSE nor LICENSE")
	return ""
}

// hasSPDXHeader reports whether src carries the Apache-2.0 SPDX marker
// in its leading comment block (below an optional //go:build line).
func hasSPDXHeader(src string) bool {
	lines := strings.SplitN(src, "\n", 6)
	for _, line := range lines {
		if strings.HasPrefix(line, "// SPDX-License-Identifier: Apache-2.0") {
			return true
		}
	}
	return false
}
