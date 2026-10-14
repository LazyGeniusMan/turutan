// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	turutanconfig "github.com/LazyGeniusMan/turutan/internal/config"
)

const (
	licenseMIT0 = "MIT No Attribution\n\nCopyright 2026 Example\n\n" +
		"Permission is hereby granted, free of charge, to any person obtaining a copy\n"
	licenseMIT = "MIT License\n\nCopyright (c) 2026 Example\n\n" +
		"Permission is hereby granted, free of charge, to any person obtaining a copy\n"
	licenseApache = "Apache License\nVersion 2.0, January 2004\n" +
		"http://www.apache.org/licenses/\n"
)

func TestResolveTemplateLicense(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		fsys     fstest.MapFS
		manifest *turutanconfig.Manifest
		want     string
	}{
		{name: "empty defaults MIT-0", fsys: fstest.MapFS{}, want: "MIT-0"},
		{
			name: "TEMPLATE_LICENSE MIT-0",
			fsys: fstest.MapFS{"TEMPLATE_LICENSE": {Data: []byte(licenseMIT0)}},
			want: "MIT-0",
		},
		{
			name: "LICENSE fallback MIT-0",
			fsys: fstest.MapFS{"LICENSE": {Data: []byte(licenseMIT0)}},
			want: "MIT-0",
		},
		{
			name: "MIT text detects MIT",
			fsys: fstest.MapFS{"LICENSE": {Data: []byte(licenseMIT)}},
			want: "MIT",
		},
		{
			name: "Apache text detects Apache-2.0",
			fsys: fstest.MapFS{"LICENSE": {Data: []byte(licenseApache)}},
			want: "Apache-2.0",
		},
		{
			name: "TEMPLATE_LICENSE wins over LICENSE",
			fsys: fstest.MapFS{
				"TEMPLATE_LICENSE": {Data: []byte(licenseMIT0)},
				"LICENSE":          {Data: []byte(licenseApache)},
			},
			want: "MIT-0",
		},
		{
			name: "unknown text defaults MIT-0",
			fsys: fstest.MapFS{"LICENSE": {Data: []byte("All rights reserved.\n")}},
			want: "MIT-0",
		},
		{
			name:     "manifest license wins over sniffing",
			fsys:     fstest.MapFS{"LICENSE": {Data: []byte(licenseApache)}},
			manifest: &turutanconfig.Manifest{License: "MIT-0"},
			want:     "MIT-0",
		},
		{
			name:     "manifest license wins without license files",
			fsys:     fstest.MapFS{},
			manifest: &turutanconfig.Manifest{License: "Apache-2.0"},
			want:     "Apache-2.0",
		},
		{
			name:     "empty manifest license falls back to sniffing",
			fsys:     fstest.MapFS{"LICENSE": {Data: []byte(licenseApache)}},
			manifest: &turutanconfig.Manifest{},
			want:     "Apache-2.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveTemplateLicense(tt.fsys, tt.manifest); got != tt.want {
				t.Errorf("resolveTemplateLicense() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlantLicenseFiles(t *testing.T) {
	t.Parallel()
	t.Run("copies verbatim and removes template-root copy", func(t *testing.T) {
		t.Parallel()
		fetched := t.TempDir()
		const body = "MIT No Attribution\n{{must not render}}\n"
		if err := os.WriteFile(filepath.Join(fetched, "LICENSE"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		stage := t.TempDir()
		if err := os.WriteFile(filepath.Join(stage, "LICENSE"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		manifest := &turutanconfig.Manifest{LicenseFiles: []string{"LICENSE"}}
		if err := plantLicenseFiles(fetched, stage, manifest); err != nil {
			t.Fatalf("plantLicenseFiles error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(stage, ".config", "turutan", "LICENSE"))
		if err != nil {
			t.Fatalf("planted license missing: %v", err)
		}
		if string(got) != body {
			t.Errorf("planted license = %q, want verbatim %q", got, body)
		}
		if _, err := os.Stat(filepath.Join(stage, "LICENSE")); !os.IsNotExist(err) {
			t.Error("staged template-root LICENSE still present, want it removed")
		}
	})
	t.Run("missing file names the file", func(t *testing.T) {
		t.Parallel()
		manifest := &turutanconfig.Manifest{LicenseFiles: []string{"LICENSE"}}
		err := plantLicenseFiles(t.TempDir(), t.TempDir(), manifest)
		if err == nil || !strings.Contains(err.Error(), `"LICENSE"`) {
			t.Errorf("error = %v, want it to name the missing file", err)
		}
	})
	t.Run("no license-files is a no-op", func(t *testing.T) {
		t.Parallel()
		if err := plantLicenseFiles(t.TempDir(), t.TempDir(), &turutanconfig.Manifest{}); err != nil {
			t.Errorf("plantLicenseFiles error: %v", err)
		}
	})
}

func TestInitStampsSourceLicense(t *testing.T) {
	t.Parallel()
	t.Run("custom LICENSE stamps its SPDX", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			"go.mod.tmpl": "module {{.project_name}}\n",
			"LICENSE":     licenseApache,
		}
		src := makeTemplate(t, "", files)
		target := filepath.Join(t.TempDir(), "proj")
		if err := Init(context.Background(), src, target, testOptions(&strings.Builder{})); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		state, err := turutanconfig.LoadState(os.DirFS(target))
		if err != nil {
			t.Fatalf("LoadState error: %v", err)
		}
		if state.TemplateLicense != "Apache-2.0" {
			t.Errorf("TemplateLicense = %q, want Apache-2.0", state.TemplateLicense)
		}
	})
	t.Run("manifest license wins over sniffing", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			"go.mod.tmpl": "module {{.project_name}}\n",
			"LICENSE":     licenseMIT,
		}
		src := makeTemplate(t, "license: Apache-2.0\n", files)
		target := filepath.Join(t.TempDir(), "proj")
		if err := Init(context.Background(), src, target, testOptions(&strings.Builder{})); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		state, err := turutanconfig.LoadState(os.DirFS(target))
		if err != nil {
			t.Fatalf("LoadState error: %v", err)
		}
		if state.TemplateLicense != "Apache-2.0" {
			t.Errorf("TemplateLicense = %q, want Apache-2.0", state.TemplateLicense)
		}
	})
	t.Run("license-files plant into config dir", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			"go.mod.tmpl": "module {{.project_name}}\n",
			"LICENSE":     licenseApache,
		}
		src := makeTemplate(t, "license: Apache-2.0\nlicense-files: [LICENSE]\n", files)
		target := filepath.Join(t.TempDir(), "proj")
		if err := Init(context.Background(), src, target, testOptions(&strings.Builder{})); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(target, ".config", "turutan", "LICENSE"))
		if err != nil {
			t.Fatalf("planted license missing: %v", err)
		}
		if string(got) != licenseApache {
			t.Errorf("planted license = %q, want template bytes", got)
		}
		if _, err := os.Stat(filepath.Join(target, "LICENSE")); !os.IsNotExist(err) {
			t.Error("template-root LICENSE published, want only the .config copy")
		}
	})
	t.Run("missing license-file fails naming the file", func(t *testing.T) {
		t.Parallel()
		src := makeTemplate(t, "license-files: [LICENSE]\n", map[string]string{"go.mod.tmpl": "module {{.project_name}}\n"})
		err := Init(context.Background(), src, filepath.Join(t.TempDir(), "proj"), testOptions(&strings.Builder{}))
		if err == nil || !strings.Contains(err.Error(), `"LICENSE"`) {
			t.Errorf("error = %v, want it to name the missing file", err)
		}
	})
	t.Run("missing license defaults MIT-0", func(t *testing.T) {
		t.Parallel()
		src := makeTemplate(t, "", map[string]string{"go.mod.tmpl": "module {{.project_name}}\n"})
		target := filepath.Join(t.TempDir(), "proj")
		if err := Init(context.Background(), src, target, testOptions(&strings.Builder{})); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		state, err := turutanconfig.LoadState(os.DirFS(target))
		if err != nil {
			t.Fatalf("LoadState error: %v", err)
		}
		if state.TemplateLicense != turutanconfig.TemplateLicenseMIT0 {
			t.Errorf("TemplateLicense = %q, want MIT-0", state.TemplateLicense)
		}
	})
}
