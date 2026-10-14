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

func TestDiffInputsConditionalInputs(t *testing.T) {
	t.Parallel()
	manifest := conditionalInputsManifest()
	t.Run("ai absent excludes agents", func(t *testing.T) {
		t.Parallel()
		data := map[string]any{"project_name": "demo", "profiles": []string{"go"}}
		if got := DiffInputs(data, manifest); len(got) != 0 {
			t.Errorf("DiffInputs = %v, want empty", got)
		}
	})
	t.Run("ai present includes agents only", func(t *testing.T) {
		t.Parallel()
		data := map[string]any{"project_name": "demo", "profiles": []string{"ai"}}
		if got := DiffInputs(data, manifest); !slices.Equal(got, []string{"ai_agents"}) {
			t.Errorf("DiffInputs = %v, want [ai_agents]", got)
		}
	})
	t.Run("data predicate true includes nick", func(t *testing.T) {
		t.Parallel()
		data := map[string]any{"project_name": "x", "profiles": []string{}}
		if got := DiffInputs(data, manifest); !slices.Equal(got, []string{"nick"}) {
			t.Errorf("DiffInputs = %v, want [nick]", got)
		}
	})
	t.Run("undecidable predicate stays included", func(t *testing.T) {
		t.Parallel()
		if got := DiffInputs(nil, manifest); !slices.Contains(got, "project_name") {
			t.Errorf("DiffInputs = %v, want project_name", got)
		} else if !slices.Contains(got, "nick") {
			t.Errorf("DiffInputs = %v, want undecidable nick still reported", got)
		} else if slices.Contains(got, "ai_agents") {
			t.Errorf("DiffInputs = %v, want no skipped ai_agents", got)
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
	if def.Label == "" {
		t.Errorf("ai_agents lost label: %+v", def)
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
	stored := DiffInputs(map[string]any{
		"project_name": "demo", "profiles": []string{"go"}, "ai_agents": []string{"opencode", "pi"},
	}, got)
	if slices.Contains(stored, "ai_agents") {
		t.Errorf("DiffInputs = %v, want stored agents kept without ai", stored)
	}
	fresh := DiffInputs(map[string]any{"project_name": "demo", "profiles": []string{"go"}}, got)
	if slices.Contains(fresh, "ai_agents") {
		t.Errorf("DiffInputs = %v, want skipped agents excluded without ai", fresh)
	}
	asked := DiffInputs(map[string]any{"project_name": "demo", "profiles": []string{"ai"}}, got)
	if !slices.Contains(asked, "ai_agents") {
		t.Errorf("DiffInputs = %v, want agents reported with ai", asked)
	}
}
