// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

func conditionalInputManifest() *config.Manifest {
	return &config.Manifest{Inputs: map[string]config.ManifestInput{
		"project_name":   {Question: "Project Name", Type: "string"},
		"ai_agents":      {Question: "agents", Type: "array", Default: []string{"opencode", "pi"}, If: `{{contains .profiles "ai"}}`},
		"nick":           {Type: "string", Default: "n/a", If: `{{eq .project_name "x"}}`},
		"opt_no_default": {Type: "string", If: `{{contains .profiles "ai"}}`},
	}}
}

func forwardRefManifest() *config.Manifest {
	return &config.Manifest{Inputs: map[string]config.ManifestInput{
		"aaa_dep":  {Type: "string", Default: "skipped-default", If: `{{eq .zzz_base "x"}}`},
		"zzz_base": {Type: "string"},
	}}
}

func TestShouldAskInput(t *testing.T) {
	t.Parallel()
	manifest := conditionalInputManifest()
	tests := []struct {
		name     string
		key      string
		data     map[string]any
		profiles []string
		want     bool
		wantErr  string
	}{
		{name: "no if asks", key: "project_name", data: map[string]any{}, profiles: []string{"core"}, want: true},
		{name: "ai in asks", key: "ai_agents", data: map[string]any{}, profiles: []string{"core", "ai"}, want: true},
		{name: "ai out skips", key: "ai_agents", data: map[string]any{}, profiles: []string{"core", "go"}, want: false},
		{name: "data eq true asks", key: "nick", data: map[string]any{"project_name": "x"}, profiles: []string{"core"}, want: true},
		{name: "data eq false skips", key: "nick", data: map[string]any{"project_name": "y"}, profiles: []string{"core"}, want: false},
		{name: "missing dep retries", key: "nick", data: map[string]any{}, profiles: []string{"core"}, wantErr: `no entry for key "project_name"`},
		{name: "unknown key asks", key: "absent", data: map[string]any{}, profiles: []string{"core"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ShouldAskInput(manifest, tt.key, renderDataView(tt.data, tt.profiles))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ShouldAskInput error = %v, want containing %q", err, tt.wantErr)
				}
				if _, ok := AsMissingKey(err); !ok {
					t.Errorf("error %v is not a missing-key retry error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ShouldAskInput error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ShouldAskInput = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("nil manifest asks", func(t *testing.T) {
		t.Parallel()
		if got, err := ShouldAskInput(nil, "x", map[string]any{}); err != nil || !got {
			t.Errorf("ShouldAskInput(nil) = %v, %v; want true, nil", got, err)
		}
	})
	t.Run("bad predicate errors", func(t *testing.T) {
		t.Parallel()
		bad := &config.Manifest{Inputs: map[string]config.ManifestInput{
			"x": {If: `{{contains .profiles}`},
		}}
		if _, err := ShouldAskInput(bad, "x", renderDataView(map[string]any{}, []string{"core"})); err == nil {
			t.Error("ShouldAskInput with bad predicate succeeded, want error")
		}
	})
	t.Run("non-bool predicate errors", func(t *testing.T) {
		t.Parallel()
		bad := &config.Manifest{Inputs: map[string]config.ManifestInput{
			"x": {If: `{{.project_name}}`},
		}}
		view := renderDataView(map[string]any{"project_name": "demo"}, []string{"core"})
		if _, err := ShouldAskInput(bad, "x", view); err == nil ||
			!strings.Contains(err.Error(), "not a boolean") {
			t.Errorf("ShouldAskInput non-bool error = %v, want not-a-boolean", err)
		}
	})
}

func TestEnsureInputValuesConditional(t *testing.T) {
	t.Parallel()
	t.Run("skipped takes default with no prompt", func(t *testing.T) {
		t.Parallel()
		manifest := conditionalInputManifest()
		data := map[string]any{"project_name": "demo"}
		var stdout bytes.Buffer
		stdin := strings.NewReader("")
		if err := ensureInputValues(data, manifest, []string{"core", "go"}, false, false, &stdout, stdin, "bootstrap"); err != nil {
			t.Fatalf("ensureInputValues error = %v", err)
		}
		if got, ok := data["ai_agents"].([]string); !ok || len(got) != 2 || got[0] != "opencode" || got[1] != "pi" {
			t.Errorf("ai_agents = %v (%T), want [opencode pi]", data["ai_agents"], data["ai_agents"])
		}
		if data["nick"] != "n/a" {
			t.Errorf("nick = %v, want skipped default n/a", data["nick"])
		}
		if _, ok := data["opt_no_default"]; ok {
			t.Errorf("opt_no_default = %v, want absent (skipped, no default)", data["opt_no_default"])
		}
		if stdout.Len() != 0 {
			t.Errorf("prompted %q, want no prompt for skipped inputs", stdout.String())
		}
	})
	t.Run("asked prompts as before", func(t *testing.T) {
		t.Parallel()
		manifest := conditionalInputManifest()
		data := map[string]any{"project_name": "demo"}
		var stdout bytes.Buffer
		stdin := strings.NewReader("\n\n")
		if err := ensureInputValues(data, manifest, []string{"core", "ai"}, false, false, &stdout, stdin, "bootstrap"); err != nil {
			t.Fatalf("ensureInputValues error = %v", err)
		}
		if got, ok := data["ai_agents"].([]string); !ok || len(got) != 2 {
			t.Errorf("ai_agents = %v (%T), want default [opencode pi] on empty line", data["ai_agents"], data["ai_agents"])
		}
		if data["opt_no_default"] != "" {
			t.Errorf("opt_no_default = %v, want prompted empty value", data["opt_no_default"])
		}
		if !strings.Contains(stdout.String(), "agents") {
			t.Errorf("prompt output %q lacks agents question, want prompt when ai present", stdout.String())
		}
	})
	t.Run("multi-pass answers forward reference", func(t *testing.T) {
		t.Parallel()
		data := map[string]any{}
		var stdout bytes.Buffer
		stdin := strings.NewReader("x\nhello\n")
		if err := ensureInputValues(data, forwardRefManifest(), []string{"core"}, false, false, &stdout, stdin, "bootstrap"); err != nil {
			t.Fatalf("ensureInputValues error = %v", err)
		}
		if data["zzz_base"] != "x" || data["aaa_dep"] != "hello" {
			t.Errorf("data = %v, want zzz_base=x aaa_dep=hello", data)
		}
	})
	t.Run("multi-pass skips when dep answers no", func(t *testing.T) {
		t.Parallel()
		data := map[string]any{}
		var stdout bytes.Buffer
		stdin := strings.NewReader("y\n")
		if err := ensureInputValues(data, forwardRefManifest(), []string{"core"}, false, false, &stdout, stdin, "bootstrap"); err != nil {
			t.Fatalf("ensureInputValues error = %v", err)
		}
		if data["zzz_base"] != "y" {
			t.Errorf("zzz_base = %v, want y", data["zzz_base"])
		}
		if data["aaa_dep"] != "skipped-default" {
			t.Errorf("aaa_dep = %v, want skipped default without prompt", data["aaa_dep"])
		}
	})
	t.Run("non-interactive skipped takes default", func(t *testing.T) {
		t.Parallel()
		data := map[string]any{"project_name": "demo"}
		var stdout bytes.Buffer
		if err := ensureInputValues(data, conditionalInputManifest(), []string{"core", "go"}, true, false, &stdout, strings.NewReader(""), "bootstrap"); err != nil {
			t.Fatalf("ensureInputValues error = %v, want skipped default without missing-value error", err)
		}
		if got, ok := data["ai_agents"].([]string); !ok || len(got) != 2 {
			t.Errorf("ai_agents = %v, want default [opencode pi]", data["ai_agents"])
		}
	})
	t.Run("non-interactive asked still errors", func(t *testing.T) {
		t.Parallel()
		data := map[string]any{}
		var stdout bytes.Buffer
		err := ensureInputValues(data, conditionalInputManifest(), []string{"core", "ai"}, true, false, &stdout, strings.NewReader(""), "bootstrap")
		if err == nil || !strings.Contains(err.Error(), "missing value") {
			t.Errorf("ensureInputValues error = %v, want missing value", err)
		}
	})
	t.Run("bad predicate errors", func(t *testing.T) {
		t.Parallel()
		bad := &config.Manifest{Inputs: map[string]config.ManifestInput{
			"x": {If: `{{.project_name}}`},
		}}
		data := map[string]any{"project_name": "demo"}
		var stdout bytes.Buffer
		if err := ensureInputValues(data, bad, []string{"core"}, false, false, &stdout, strings.NewReader(""), "bootstrap"); err == nil {
			t.Error("ensureInputValues with non-bool predicate succeeded, want error")
		}
	})
}

func TestInputMissingKeyConditional(t *testing.T) {
	t.Parallel()
	t.Run("skipped with default fills without prompt", func(t *testing.T) {
		t.Parallel()
		data := map[string]any{"project_name": "demo"}
		var stdout bytes.Buffer
		if err := inputMissingKey(data, conditionalInputManifest(), []string{"core"}, &stdout, strings.NewReader(""), "ai_agents"); err != nil {
			t.Fatalf("inputMissingKey error = %v", err)
		}
		if got, ok := data["ai_agents"].([]string); !ok || len(got) != 2 {
			t.Errorf("ai_agents = %v, want default without prompt", data["ai_agents"])
		}
	})
	t.Run("skipped without default errors", func(t *testing.T) {
		t.Parallel()
		data := map[string]any{"project_name": "demo"}
		var stdout bytes.Buffer
		err := inputMissingKey(data, conditionalInputManifest(), []string{"core"}, &stdout, strings.NewReader(""), "opt_no_default")
		if err == nil || !strings.Contains(err.Error(), "skipped by its `if`") {
			t.Errorf("inputMissingKey error = %v, want skipped-condition error", err)
		}
	})
	t.Run("predicate true prompts", func(t *testing.T) {
		t.Parallel()
		data := map[string]any{"project_name": "x"}
		var stdout bytes.Buffer
		if err := inputMissingKey(data, conditionalInputManifest(), []string{"core"}, &stdout, strings.NewReader("bob\n"), "nick"); err != nil {
			t.Fatalf("inputMissingKey error = %v", err)
		}
		if data["nick"] != "bob" {
			t.Errorf("nick = %v, want prompted bob", data["nick"])
		}
	})
	t.Run("missing dep asked first", func(t *testing.T) {
		t.Parallel()
		data := map[string]any{}
		var stdout bytes.Buffer
		if err := inputMissingKey(data, forwardRefManifest(), []string{"core"}, &stdout, strings.NewReader("x\nval\n"), "aaa_dep"); err != nil {
			t.Fatalf("inputMissingKey error = %v", err)
		}
		if data["zzz_base"] != "x" || data["aaa_dep"] != "val" {
			t.Errorf("data = %v, want dep asked before target", data)
		}
	})
}
