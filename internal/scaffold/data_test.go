// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

func seedTestManifest() *config.Manifest {
	return &config.Manifest{Inputs: map[string]config.ManifestInput{
		"greeting":   {Type: "string", Default: "hello"},
		"new_list":   {Type: "array", Default: []string{"opencode", "pi"}},
		"no_default": {Type: "string"},
	}}
}

func TestSeedMissingDefaults(t *testing.T) {
	tests := []struct {
		name   string
		data   map[string]any
		key    string
		want   any
		wantOk bool
	}{
		{name: "missing string filled", data: map[string]any{}, key: "greeting", want: "hello", wantOk: true},
		{name: "missing list filled", data: map[string]any{}, key: "new_list", want: []string{"opencode", "pi"}, wantOk: true},
		{name: "present value kept", data: map[string]any{"greeting": "hi"}, key: "greeting", want: "hi", wantOk: true},
		{name: "present empty kept", data: map[string]any{"greeting": ""}, key: "greeting", want: "", wantOk: true},
		{name: "no default untouched", data: map[string]any{}, key: "no_default", want: nil, wantOk: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seedMissingDefaults(tt.data, seedTestManifest())
			got, ok := tt.data[tt.key]
			if ok != tt.wantOk {
				t.Fatalf("present = %v, want %v (value %v)", ok, tt.wantOk, got)
			}
			if !tt.wantOk {
				return
			}
			switch want := tt.want.(type) {
			case []string:
				list, isList := got.([]string)
				if !isList || !slices.Equal(list, want) {
					t.Errorf("data[%q] = %v (%T), want %v", tt.key, got, got, want)
				}
			default:
				if got != want {
					t.Errorf("data[%q] = %v, want %v", tt.key, got, want)
				}
			}
		})
	}
	t.Run("nil manifest is a no-op", func(t *testing.T) {
		data := map[string]any{"greeting": "hi"}
		seedMissingDefaults(data, nil)
		if len(data) != 1 || data["greeting"] != "hi" {
			t.Errorf("data = %v, want it unchanged", data)
		}
	})
}

func TestSeedMissingDefaultsCopies(t *testing.T) {
	t.Run("string slice defaults do not alias", func(t *testing.T) {
		manifest := seedTestManifest()
		data := map[string]any{}
		seedMissingDefaults(data, manifest)
		seeded, ok := data["new_list"].([]string)
		if !ok {
			t.Fatalf("new_list = %T, want []string", data["new_list"])
		}
		seeded[0] = "mutated"
		if def, _ := manifest.Inputs["new_list"].Default.([]string); !slices.Equal(def, []string{"opencode", "pi"}) {
			t.Errorf("manifest default = %v, want it unchanged after seeding", def)
		}
	})
	t.Run("any slice defaults do not alias", func(t *testing.T) {
		manifest := &config.Manifest{Inputs: map[string]config.ManifestInput{
			"mixed": {Type: "array", Default: []any{"a", "b"}},
		}}
		data := map[string]any{}
		seedMissingDefaults(data, manifest)
		seeded, ok := data["mixed"].([]any)
		if !ok {
			t.Fatalf("mixed = %T, want []any", data["mixed"])
		}
		seeded[0] = "mutated"
		if def, _ := manifest.Inputs["mixed"].Default.([]any); len(def) != 2 || def[0] != "a" {
			t.Errorf("manifest default = %v, want it unchanged after seeding", def)
		}
	})
}

func TestInputLabelRendering(t *testing.T) {
	t.Parallel()
	t.Run("string uses label", func(t *testing.T) {
		t.Parallel()
		var stdout bytes.Buffer
		got, err := inputForKey(&stdout, strings.NewReader("\n"),
			"project_name", config.ManifestInput{Label: "Project Name", Type: "string", Default: "my-new-project"})
		if err != nil {
			t.Fatalf("inputForKey error = %v", err)
		}
		if got != "my-new-project" {
			t.Errorf("got = %v, want default my-new-project", got)
		}
		if out := stdout.String(); !strings.Contains(out, "Enter Project Name [my-new-project]:") {
			t.Errorf("prompt = %q, want label-driven string prompt", out)
		}
	})
	t.Run("array uses label with suffix", func(t *testing.T) {
		t.Parallel()
		var stdout bytes.Buffer
		got, err := inputForKey(&stdout, strings.NewReader("\n"),
			"ai_agents", config.ManifestInput{Label: "AI Agents", Type: "array", Default: []string{"opencode", "pi"}})
		if err != nil {
			t.Fatalf("inputForKey error = %v", err)
		}
		list, ok := got.([]string)
		if !ok || !slices.Equal(list, []string{"opencode", "pi"}) {
			t.Errorf("got = %v (%T), want [opencode pi]", got, got)
		}
		out := stdout.String()
		if !strings.Contains(out, "Enter AI Agents (comma-separated) [opencode,pi]:") {
			t.Errorf("prompt = %q, want label-driven array prompt", out)
		}
		if strings.Contains(out, "value for") {
			t.Errorf("prompt = %q, want label text only", out)
		}
		if n := strings.Count(out, "(comma-separated)"); n != 1 {
			t.Errorf("prompt = %q, want exactly one suffix, got %d", out, n)
		}
	})
	t.Run("array suffix not duplicated", func(t *testing.T) {
		t.Parallel()
		var stdout bytes.Buffer
		_, err := inputForKey(&stdout, strings.NewReader("a\n"),
			"tags", config.ManifestInput{Label: "Tags (comma-separated)", Type: "array", Default: []string{}})
		if err != nil {
			t.Fatalf("inputForKey error = %v", err)
		}
		if n := strings.Count(stdout.String(), "(comma-separated)"); n != 1 {
			t.Errorf("prompt = %q, want no duplication, got %d", stdout.String(), n)
		}
	})
	t.Run("bool renders y/n with shortcut", func(t *testing.T) {
		t.Parallel()
		for _, tt := range []struct {
			stdin string
			want  bool
		}{
			{stdin: "y\n", want: true},
			{stdin: "n\n", want: false},
			{stdin: "yes\n", want: true},
			{stdin: "no\n", want: false},
			{stdin: "true\n", want: true},
			{stdin: "false\n", want: false},
			{stdin: "1\n", want: true},
			{stdin: "0\n", want: false},
		} {
			var stdout bytes.Buffer
			got, err := inputForKey(&stdout, strings.NewReader(tt.stdin),
				"flag", config.ManifestInput{Label: "Feature", Type: "bool", Default: false})
			if err != nil {
				t.Fatalf("inputForKey(%q) error = %v", tt.stdin, err)
			}
			if got != tt.want {
				t.Errorf("inputForKey(%q) = %v, want %v", tt.stdin, got, tt.want)
			}
			if out := stdout.String(); !strings.Contains(out, "Enter Feature (y/n)") {
				t.Errorf("prompt = %q, want (y/n) suffix", out)
			}
		}
	})
	t.Run("bool default shown as y/n", func(t *testing.T) {
		t.Parallel()
		var stdout bytes.Buffer
		got, err := inputForKey(&stdout, strings.NewReader("\n"),
			"flag", config.ManifestInput{Label: "Feature", Type: "bool", Default: true})
		if err != nil {
			t.Fatalf("inputForKey error = %v", err)
		}
		if got != true {
			t.Errorf("got = %v, want true", got)
		}
		if out := stdout.String(); !strings.Contains(out, "Enter Feature (y/n) [y]:") {
			t.Errorf("prompt = %q, want [y] default", out)
		}
		var stdoutN bytes.Buffer
		gotN, err := inputForKey(&stdoutN, strings.NewReader("\n"),
			"flag", config.ManifestInput{Label: "Feature", Type: "bool", Default: false})
		if err != nil {
			t.Fatalf("inputForKey error = %v", err)
		}
		if gotN != false {
			t.Errorf("got = %v, want false", gotN)
		}
		if out := stdoutN.String(); !strings.Contains(out, "Enter Feature (y/n) [n]:") {
			t.Errorf("prompt = %q, want [n] default", out)
		}
	})
	t.Run("options listed before prompt", func(t *testing.T) {
		t.Parallel()
		var stdout bytes.Buffer
		_, err := inputForKey(&stdout, strings.NewReader("a\n"),
			"choice", config.ManifestInput{Label: "Choice", Type: "string", Options: []string{"a", "b", "c"}, Default: "a"})
		if err != nil {
			t.Fatalf("inputForKey error = %v", err)
		}
		out := stdout.String()
		if !strings.Contains(out, "Possible values: a, b, c") {
			t.Errorf("prompt = %q, want options line", out)
		}
		if strings.Index(out, "Possible values:") > strings.Index(out, "Enter Choice") {
			t.Errorf("prompt = %q, want options before the prompt", out)
		}
		var stdoutArr bytes.Buffer
		_, err = inputForKey(&stdoutArr, strings.NewReader("a,b\n"),
			"tags", config.ManifestInput{Label: "Tags", Type: "array", Options: []string{"a", "b"}, Default: []string{}})
		if err != nil {
			t.Fatalf("inputForKey error = %v", err)
		}
		if !strings.Contains(stdoutArr.String(), "Possible values: a, b") {
			t.Errorf("array prompt = %q, want options line", stdoutArr.String())
		}
	})
	t.Run("empty label falls back to key", func(t *testing.T) {
		t.Parallel()
		var stdout bytes.Buffer
		_, err := inputForKey(&stdout, strings.NewReader("v\n"),
			"mykey", config.ManifestInput{Type: "string", Default: ""})
		if err != nil {
			t.Fatalf("inputForKey error = %v", err)
		}
		if !strings.Contains(stdout.String(), "Enter mykey []") {
			t.Errorf("prompt = %q, want key fallback", stdout.String())
		}
	})
}
