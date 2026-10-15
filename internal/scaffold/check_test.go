// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func checkOptions(stdout *strings.Builder, regs map[string]string) CheckOptions {
	return CheckOptions{Stdout: stdout, Registries: regs}
}

func TestCheckRegistry(t *testing.T) {
	t.Run("up to date on clean init", func(t *testing.T) {
		projectDir, regs := initRegistryProject(t, "", map[string]string{"hello.txt": "v1\n"})
		var stdout strings.Builder
		result, err := Check(context.Background(), projectDir, checkOptions(&stdout, regs))
		if err != nil {
			t.Fatalf("Check error: %v", err)
		}
		if result.Available {
			t.Error("Available = true, want false on clean init")
		}
		if result.Old != testRegistryVersion || result.New != testRegistryVersion {
			t.Errorf("Old/New = %q/%q, want both %q", result.Old, result.New, testRegistryVersion)
		}
		if !strings.Contains(stdout.String(), "up to date") {
			t.Errorf("stdout = %q, want up-to-date line", stdout.String())
		}
	})
	t.Run("new release reports available", func(t *testing.T) {
		projectDir, regs := initRegistryProject(t, "", map[string]string{"hello.txt": "v1\n"})
		fresh := updateTestTemplateFile(t, regs, testRegistryTemplate, "hello.txt", "v2\n")
		var stdout strings.Builder
		result, err := Check(context.Background(), projectDir, checkOptions(&stdout, regs))
		if err != nil {
			t.Fatalf("Check error: %v", err)
		}
		if !result.Available {
			t.Fatal("Available = false, want true after template release")
		}
		if result.Old != testRegistryVersion {
			t.Errorf("Old = %q, want %q", result.Old, testRegistryVersion)
		}
		if result.New != fresh {
			t.Errorf("New = %q, want %q", result.New, fresh)
		}
		if !strings.Contains(stdout.String(), "update available") {
			t.Errorf("stdout = %q, want update-available line", stdout.String())
		}
	})
	t.Run("ref pins the stored version", func(t *testing.T) {
		projectDir, regs := initRegistryProject(t, "", map[string]string{"hello.txt": "v1\n"})
		updateTestTemplateFile(t, regs, testRegistryTemplate, "hello.txt", "v2\n")
		var stdout strings.Builder
		opts := checkOptions(&stdout, regs)
		opts.Ref = testRegistryVersion
		result, err := Check(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Check error: %v", err)
		}
		if result.Available {
			t.Error("Available = true, want false when --ref pins the stored version")
		}
	})
	t.Run("unknown ref errors", func(t *testing.T) {
		projectDir, regs := initRegistryProject(t, "", map[string]string{"hello.txt": "v1\n"})
		var stdout strings.Builder
		opts := checkOptions(&stdout, regs)
		opts.Ref = "9.9.9"
		if _, err := Check(context.Background(), projectDir, opts); err == nil {
			t.Error("Check with unknown ref succeeded, want error")
		}
	})
	t.Run("project-only edit stays up to date", func(t *testing.T) {
		projectDir, regs := initRegistryProject(t, "", map[string]string{"hello.txt": "v1\n"})
		if err := os.WriteFile(filepath.Join(projectDir, "hello.txt"), []byte("local edit\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout strings.Builder
		result, err := Check(context.Background(), projectDir, checkOptions(&stdout, regs))
		if err != nil {
			t.Fatalf("Check error: %v", err)
		}
		if result.Available {
			t.Error("Available = true, want false: only the project changed, not the template")
		}
	})
	t.Run("preserved local edit stays up to date", func(t *testing.T) {
		projectDir, regs := initRegistryProject(t,
			"preserves:\n  - \"keep.txt\"\n", map[string]string{"keep.txt": "template keep\n"})
		if err := os.WriteFile(filepath.Join(projectDir, "keep.txt"), []byte("local keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout strings.Builder
		result, err := Check(context.Background(), projectDir, checkOptions(&stdout, regs))
		if err != nil {
			t.Fatalf("Check error: %v", err)
		}
		if result.Available {
			t.Error("Available = true, want false: preserved paths carry local content")
		}
	})
	t.Run("missing state errors", func(t *testing.T) {
		var stdout strings.Builder
		if _, err := Check(context.Background(), t.TempDir(), checkOptions(&stdout, nil)); err == nil {
			t.Error("Check without state succeeded, want error")
		}
	})
}

func TestCheckRegistryMinEngine(t *testing.T) {
	t.Run("unsatisfiable floor stays best-effort", func(t *testing.T) {
		projectDir, regs := initRegistryProject(t,
			"min-engine: \">=0.1.0\"\n", map[string]string{"hello.txt": "v1\n"})
		updateTestTemplateFile(t, regs, testRegistryTemplate, ".turutan.yml", "min-engine: \">=99.0.0\"\n")
		var stdout strings.Builder
		opts := checkOptions(&stdout, regs)
		opts.Engine = "0.1.0"
		result, err := Check(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Check error: %v", err)
		}
		if !result.Available {
			t.Error("Available = false, want true: the probe reports versions, the update gates the floor")
		}
	})
}

func TestCheckRegistryInputs(t *testing.T) {
	t.Run("new input reports available with inputs line", func(t *testing.T) {
		const manifest = "inputs:\n" +
			"  project_name:\n    label: \"Project Name\"\n    type: string\n"
		projectDir, regs := initRegistryProject(t, manifest, map[string]string{"hello.txt": "v1\n"})
		updateTestTemplateFile(t, regs, testRegistryTemplate, ".turutan.yml",
			manifest+"  service_name:\n    label: \"Service name\"\n    type: string\n")
		var stdout strings.Builder
		result, err := Check(context.Background(), projectDir, checkOptions(&stdout, regs))
		if err != nil {
			t.Fatalf("Check error: %v", err)
		}
		if !result.Available {
			t.Fatal("Available = false, want true after template release")
		}
		if !strings.Contains(stdout.String(), "update available") {
			t.Errorf("stdout = %q, want the update-available line", stdout.String())
		}
		if !strings.Contains(stdout.String(), "turutan: template adds new inputs: service_name") {
			t.Errorf("stdout = %q, want the new-inputs line", stdout.String())
		}
	})
}
