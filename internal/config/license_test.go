// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var goDirsWithHeaders = []string{"cmd", "internal"}

func TestLicenseHeaders(t *testing.T) {
	t.Parallel()
	t.Run("SPDX present on engine sources", func(t *testing.T) {
		t.Parallel()
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

func TestTemplateLicense(t *testing.T) {
	t.Parallel()
	t.Run("license file present", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(moduleRoot(t), "templates", "default")
		for _, name := range []string{"TEMPLATE_LICENSE", "LICENSE"} {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
				return
			}
		}
		t.Error("templates/default holds neither TEMPLATE_LICENSE nor LICENSE")
	})
	t.Run("MIT-0 text", func(t *testing.T) {
		t.Parallel()
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
		t.Parallel()
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

func TestRootLicenseApache(t *testing.T) {
	t.Parallel()
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

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", root, err)
	}
	return root
}

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

func hasSPDXHeader(src string) bool {
	lines := strings.SplitN(src, "\n", 6)
	for _, line := range lines {
		if strings.HasPrefix(line, "// SPDX-License-Identifier: Apache-2.0") {
			return true
		}
	}
	return false
}
