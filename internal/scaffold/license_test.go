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
		name string
		fsys fstest.MapFS
		want string
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveTemplateLicense(tt.fsys); got != tt.want {
				t.Errorf("resolveTemplateLicense() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBootstrapStampsSourceLicense(t *testing.T) {
	t.Parallel()
	t.Run("custom LICENSE stamps its SPDX", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			"go.mod.tmpl": "module {{.project_name}}\n",
			"LICENSE":     licenseApache,
		}
		src := makeTemplate(t, "", files)
		target := filepath.Join(t.TempDir(), "proj")
		if err := Bootstrap(context.Background(), src, target, testOptions(&strings.Builder{})); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
		}
		state, err := turutanconfig.LoadState(os.DirFS(target))
		if err != nil {
			t.Fatalf("LoadState error: %v", err)
		}
		if state.TemplateLicense != "Apache-2.0" {
			t.Errorf("TemplateLicense = %q, want Apache-2.0", state.TemplateLicense)
		}
	})
	t.Run("missing license defaults MIT-0", func(t *testing.T) {
		t.Parallel()
		src := makeTemplate(t, "", map[string]string{"go.mod.tmpl": "module {{.project_name}}\n"})
		target := filepath.Join(t.TempDir(), "proj")
		if err := Bootstrap(context.Background(), src, target, testOptions(&strings.Builder{})); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
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
