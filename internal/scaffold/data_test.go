// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"slices"
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
