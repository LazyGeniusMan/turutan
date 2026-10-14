// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
)

func conditionalInputsManifest() *Manifest {
	return &Manifest{Inputs: map[string]ManifestInput{
		"project_name": {},
		"ai_agents":    {Default: []string{"opencode", "pi"}, If: `{{contains .profiles "ai"}}`},
		"nick":         {Default: "n/a", If: `{{eq .project_name "x"}}`},
	}}
}

func TestDiffProfilesConditionalInputs(t *testing.T) {
	t.Parallel()
	manifest := conditionalInputsManifest()
	t.Run("ai absent excludes agents", func(t *testing.T) {
		t.Parallel()
		diff := DiffProfiles([]string{"core", "go"}, map[string]any{"project_name": "demo"}, manifest)
		if slices.Contains(diff.NewInputs, "ai_agents") {
			t.Errorf("NewInputs = %v, want no skipped ai_agents", diff.NewInputs)
		}
		if slices.Contains(diff.NewInputs, "nick") {
			t.Errorf("NewInputs = %v, want no skipped nick", diff.NewInputs)
		}
		if len(diff.NewInputs) != 0 {
			t.Errorf("NewInputs = %v, want empty", diff.NewInputs)
		}
	})
	t.Run("ai present includes agents only", func(t *testing.T) {
		t.Parallel()
		diff := DiffProfiles([]string{"core", "ai"}, map[string]any{"project_name": "demo"}, manifest)
		if !slices.Equal(diff.NewInputs, []string{"ai_agents"}) {
			t.Errorf("NewInputs = %v, want [ai_agents]", diff.NewInputs)
		}
	})
	t.Run("data predicate true includes nick", func(t *testing.T) {
		t.Parallel()
		diff := DiffProfiles([]string{"core"}, map[string]any{"project_name": "x"}, manifest)
		if !slices.Equal(diff.NewInputs, []string{"nick"}) {
			t.Errorf("NewInputs = %v, want [nick]", diff.NewInputs)
		}
	})
	t.Run("undecidable predicate stays included", func(t *testing.T) {
		t.Parallel()
		diff := DiffProfiles([]string{"core"}, nil, manifest)
		if !slices.Contains(diff.NewInputs, "project_name") {
			t.Errorf("NewInputs = %v, want project_name", diff.NewInputs)
		}
		if !slices.Contains(diff.NewInputs, "nick") {
			t.Errorf("NewInputs = %v, want undecidable nick still reported", diff.NewInputs)
		}
		if slices.Contains(diff.NewInputs, "ai_agents") {
			t.Errorf("NewInputs = %v, want no skipped ai_agents", diff.NewInputs)
		}
	})
}

func TestDefaultTemplateConditionalInputs(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "templates", "default", ManifestFileName))
	if err != nil {
		t.Skipf("default template manifest not found: %v", err)
	}
	got, err := LoadManifest(fstest.MapFS{ManifestFileName: {Data: data}})
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}
	def, ok := got.Inputs["ai_agents"]
	if !ok {
		t.Fatal("default manifest lacks ai_agents input")
	}
	if def.If != `{{contains .profiles "ai"}}` {
		t.Errorf("ai_agents if = %q, want profiles-ai predicate", def.If)
	}
	if def.Question == "" {
		t.Errorf("ai_agents lost question: %+v", def)
	}
	if def.Type != "array" {
		t.Errorf("ai_agents type = %q, want array", def.Type)
	}
	if def.Default == nil {
		t.Error("ai_agents lost default, want [opencode pi]")
	}
	if len(def.Options) == 0 {
		t.Error("ai_agents lost options")
	}
	diff := DiffProfiles([]string{"core", "go"}, map[string]any{
		"project_name": "demo", "ai_agents": []string{"opencode", "pi"},
	}, got)
	if slices.Contains(diff.NewInputs, "ai_agents") {
		t.Errorf("NewInputs = %v, want stored agents kept without ai", diff.NewInputs)
	}
	fresh := DiffProfiles([]string{"core", "go"}, map[string]any{"project_name": "demo"}, got)
	if slices.Contains(fresh.NewInputs, "ai_agents") {
		t.Errorf("NewInputs = %v, want skipped agents excluded without ai", fresh.NewInputs)
	}
	asked := DiffProfiles([]string{"core", "ai"}, map[string]any{"project_name": "demo"}, got)
	if !slices.Contains(asked.NewInputs, "ai_agents") {
		t.Errorf("NewInputs = %v, want agents reported with ai", asked.NewInputs)
	}
}
